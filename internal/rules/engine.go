package rules

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/home"
)

// Readings is what the engine sees of the home: the current value of every
// reported data point, by device and code. It is built once per evaluation from
// a snapshot, so a condition lookup is a map read.
type Readings map[string]map[string]any

// ReadingsOf indexes a snapshot. Only reported points are included: a device
// that never sent a value has nothing to compare, and that absence is what the
// engine treats as "unknown" rather than "false".
func ReadingsOf(devices []home.DeviceView) Readings {
	out := make(Readings, len(devices))
	for _, d := range devices {
		values := make(map[string]any, len(d.Status))
		for _, s := range d.Status {
			values[s.Code] = s.Value
		}
		out[d.ID] = values
	}
	return out
}

// Truth is the result of testing a rule's trigger. Unknown is not False: a
// sensor that is offline or has not reported yet must not look like "the
// condition ended", because that would fire the rule's falling edge and, in
// level mode, start behaving as if the house were fine.
type Truth int

const (
	Unknown Truth = iota
	False
	True
)

func (t Truth) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	default:
		return "unknown"
	}
}

// State is the memory the engine needs between ticks. It is deliberately small
// and plain: what was true last time, since when, and when the rule last fired.
type State struct {
	// truth is the last known trigger state per rule.
	truth map[string]Truth
	// since is when the trigger first became true in the current run.
	since map[string]time.Time
	// fired is when the rule last acted.
	fired map[string]time.Time
	// primed is false until the first evaluation has run. The first one records
	// the state and fires nothing: after a restart every rule whose condition
	// happens to hold would otherwise fire at once.
	primed bool
}

// NewState returns an empty state, ready for the first evaluation.
func NewState() State {
	return State{
		truth: map[string]Truth{},
		since: map[string]time.Time{},
		fired: map[string]time.Time{},
	}
}

// Primed reports whether at least one evaluation has run.
func (s State) Primed() bool { return s.primed }

// LastFired reports when a rule last acted, for the journal and for tests.
func (s State) LastFired(id string) (time.Time, bool) {
	t, ok := s.fired[id]
	return t, ok
}

// Decision is one rule firing in one tick.
type Decision struct {
	RuleID   string
	RuleName string
	Group    string
	// Trigger is the trigger state that produced the decision, for the journal:
	// "condition became true" reads differently from "still true, level mode".
	Trigger Truth
	Actions []Action
}

// Evaluate advances the engine by one tick and reports what should happen.
//
// It is a pure function: same set, same readings, same state and same clock give
// the same decisions. Everything that makes automation dangerous — timing,
// cooldown, edge detection, missing values — is decided here and can be tested
// without a network, a clock or a device.
//
// loc is the location the schedule is read in; nil means time.Local.
func Evaluate(set Set, readings Readings, prev State, now time.Time, loc *time.Location) ([]Decision, State) {
	if loc == nil {
		loc = time.Local
	}
	next := NewState()
	next.primed = true
	local := now.In(loc)

	var decisions []Decision
	for _, rule := range set.Rules {
		if !rule.Enabled || !set.GroupEnabled(rule.Group) {
			// A switched-off rule keeps no memory: turning it back on starts a
			// fresh run rather than resuming a half-finished edge.
			continue
		}

		truth, err := rule.truth(readings, local)
		if err != nil {
			// A malformed schedule or comparison cannot be acted on. It is
			// reported by validation at save time; if one slips through, the rule
			// is skipped for this tick rather than fired on a guess.
			continue
		}

		previous := prev.truth[rule.ID]
		since := prev.since[rule.ID]

		switch truth {
		case True:
			if previous != True || since.IsZero() {
				since = now
			}
			next.truth[rule.ID] = True
			next.since[rule.ID] = since
		case False:
			next.truth[rule.ID] = False
		case Unknown:
			// Keep what was known. A gap in the readings is not a change of
			// state: it neither starts the timer nor ends it.
			if previous == True {
				next.truth[rule.ID] = True
				next.since[rule.ID] = since
			} else {
				next.truth[rule.ID] = Unknown
			}
			if last, ok := prev.fired[rule.ID]; ok {
				next.fired[rule.ID] = last
			}
			continue
		}
		if last, ok := prev.fired[rule.ID]; ok {
			next.fired[rule.ID] = last
		}

		if truth != True {
			continue
		}

		hold, _ := ParseDuration(rule.For)
		if hold > 0 && now.Sub(since) < hold {
			// Not held long enough yet. This is also the debounce: a reading that
			// flaps around the threshold never accumulates the hold time.
			continue
		}

		if !prev.primed {
			// First tick: record, do not act. After a restart every rule whose
			// condition happens to hold would otherwise fire at once.
			continue
		}

		mode := rule.Mode
		if mode == "" {
			mode = DefaultMode
		}
		if mode == DefaultMode {
			// Edge means once per true run. "Run" is measured from `since`, not
			// from the tick where the condition first held: with `for` set, the
			// rise is recorded immediately but the fire happens later, and a
			// comparison against the previous tick alone would lose it.
			if last, fired := prev.fired[rule.ID]; fired && !last.Before(since) {
				continue
			}
		}

		cool, _ := ParseDuration(rule.Cooldown)
		if last, ok := prev.fired[rule.ID]; ok && cool > 0 && now.Sub(last) < cool {
			continue
		}

		next.fired[rule.ID] = now
		decisions = append(decisions, Decision{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Group:    rule.Group,
			Trigger:  truth,
			Actions:  rule.Actions,
		})
	}
	return decisions, next
}

// truth evaluates a rule's trigger: all conditions hold, and the schedule (when
// present) is inside its window. Either part missing is "no constraint"; a
// reading that is missing makes the whole thing unknown.
func (r Rule) truth(readings Readings, now time.Time) (Truth, error) {
	result := True
	for _, c := range r.When {
		value, ok := readings[c.Device][c.Code]
		if !ok || value == nil {
			return Unknown, nil
		}
		ok, err := compare(value, c.Op, c.Value)
		if err != nil {
			return Unknown, err
		}
		if !ok {
			// A definite false beats an unknown from another condition: the
			// trigger is not true, and that is a fact.
			result = False
		}
	}
	if result == False {
		return False, nil
	}
	if r.At != nil {
		within, err := r.At.Within(now)
		if err != nil {
			return Unknown, err
		}
		if !within {
			return False, nil
		}
	}
	return True, nil
}

// compare applies one operator. Numbers compare numerically, so a value written
// as 180 and a reading of 180.0 agree, and a value written as "18.5" still works
// against an integer reading. Strings compare exactly, which is what an enum
// such as battery_state needs.
func compare(reading any, op string, want any) (bool, error) {
	if a, ok := asNumber(reading); ok {
		if b, ok := asNumber(want); ok {
			switch op {
			case "<":
				return a < b, nil
			case "<=":
				return a <= b, nil
			case ">":
				return a > b, nil
			case ">=":
				return a >= b, nil
			case "==":
				return a == b, nil
			case "!=":
				return a != b, nil
			}
		}
		// A numeric reading against a non-numeric value compares as text: the
		// rule is wrong, but treating ">=" on an enum as false is better than
		// failing the whole evaluation.
	}
	if b, ok := asBool(reading); ok {
		if w, ok := asBool(want); ok {
			switch op {
			case "==":
				return b == w, nil
			case "!=":
				return b != w, nil
			}
			return false, fmt.Errorf("operator %q does not apply to a boolean", op)
		}
	}
	left := asText(reading)
	right := asText(want)
	switch op {
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	}
	return false, fmt.Errorf("operator %q does not apply to %q and %q", op, left, right)
}

func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := strconv.ParseFloat(n.String(), 64)
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil || math.IsNaN(f) {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func asBool(v any) (bool, bool) {
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func asText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}
