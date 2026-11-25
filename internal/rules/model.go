// Package rules runs "condition → action" automations for the home.
//
// A rule watches the device readings CoolHome already polls and, when its
// conditions hold, sends commands through the same validated path the UI uses.
// Nothing here talks to the cloud directly: the engine is a pure function over a
// snapshot, and the runner executes its decisions.
//
// Two properties shape the design.
//
// First, CoolHome polls instead of subscribing, so the engine sees the home once
// per interval and can only compare one sample with the previous one. A rule
// therefore fires on the transition into "true", not on every tick where it is
// true — otherwise a rule that turns the heater on when it is cold would send
// that command every ten seconds for as long as the room stays cold.
//
// Second, a rule can be switched off — one rule, or a whole group of them. That
// is the answer to "the automation fights me": rather than tracking who changed
// a lamp and rewriting the rule around it, the person turns the rule off, the
// way one turns off a light switch.
package rules

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultMode is what a rule uses when it declares no mode: fire once on the
// transition into true.
const DefaultMode = "edge"

// LevelMode keeps firing while the condition holds, bounded by the cooldown. It
// is what a rule needs when the device may drift away from the wanted state
// without the condition changing — a lamp switched off by hand.
const LevelMode = "level"

// ActionKind names what an action does. Only two things exist, because both go
// through the same validated write: a data point of a device, and a scene, which
// is itself a device with a single trigger button in this app.
const (
	ActionCommand = "command"
	ActionScene   = "scene"
)

// Bounds. A rule set is hand-written in a small editor, and these keep a bad or
// hostile payload from turning into a runaway automation.
const (
	MaxRules   = 100
	MaxGroups  = 20
	MaxWhen    = 8
	MaxActions = 6
	MaxNameLen = 60

	// sceneTriggerCode is the single function every scene device exposes; see
	// internal/scene.
	sceneTriggerCode = "trigger"
)

// Group switches a set of rules together. A rule with an empty Group belongs to
// no group and is governed only by its own Enabled flag.
type Group struct {
	Name string `json:"name"`
	// Enabled is the group's own switch. A disabled group stops all its rules
	// without touching their individual switches, so turning it back on restores
	// exactly the set that was running before.
	Enabled bool `json:"enabled"`
}

// Condition compares one reading with a value.
type Condition struct {
	Device string `json:"device"`
	Code   string `json:"code"`
	// Op is one of <, <=, >, >=, ==, !=.
	Op string `json:"op"`
	// Value is compared with the reading. Numbers compare numerically; strings
	// compare as text, which is what an enum such as battery_state needs.
	Value any `json:"value"`
}

// Schedule is a window of local time. It is a condition on the clock rather than
// a separate trigger: "turn the heater on at 7:30" is the rising edge of a
// one-minute window, and "when it is cold and it is night" is a window that
// gates the readings. One mechanism, two uses.
type Schedule struct {
	// Days lists weekdays ("mon".."sun"). Empty means every day.
	Days []string `json:"days,omitempty"`
	// From is "HH:MM" local time; To is "HH:MM" and may be earlier than From,
	// which means the window runs past midnight.
	From string `json:"from"`
	To   string `json:"to,omitempty"`
}

// Action does something when a rule fires.
type Action struct {
	// Kind is ActionCommand or ActionScene.
	Kind string `json:"kind"`
	// Device and Code name the data point for a command action.
	Device string `json:"device,omitempty"`
	Code   string `json:"code,omitempty"`
	Value  any    `json:"value,omitempty"`
	// Scene is the scene's device id for a scene action.
	Scene string `json:"scene,omitempty"`
}

// Rule is one automation.
type Rule struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group,omitempty"`
	// Enabled is the rule's own switch, and the most important field in the
	// file: it is how a person stops an automation that is in the way.
	Enabled bool `json:"enabled"`
	// When lists conditions that must all hold.
	When []Condition `json:"when,omitempty"`
	// At gates the rule to a time window. Alone, it is the whole trigger.
	At *Schedule `json:"at,omitempty"`
	// For requires the trigger to hold this long before the rule fires;
	// "0" or empty means immediately. It also debounces a reading that flaps.
	For string `json:"for,omitempty"`
	// Mode is DefaultMode (edge) or LevelMode.
	Mode string `json:"mode,omitempty"`
	// Cooldown is the shortest gap between two firings.
	Cooldown string   `json:"cooldown,omitempty"`
	Actions  []Action `json:"actions"`
}

// Set is the whole rule file.
type Set struct {
	Groups []Group `json:"groups,omitempty"`
	Rules  []Rule  `json:"rules"`
}

// ErrInvalid means a submitted set is not storable as given.
var ErrInvalid = fmt.Errorf("rules: invalid rule set")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// validOps is the comparison vocabulary. It is closed on purpose: an unknown
// operator must fail when the rule is saved, not silently evaluate to false for
// weeks.
var validOps = map[string]bool{"<": true, "<=": true, ">": true, ">=": true, "==": true, "!=": true}

var validDays = map[string]bool{
	"mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true, "sun": true,
}

// ParseDuration reads a rule's duration field. It accepts Go durations ("90s",
// "10m") and a bare number of seconds, and treats an empty string as zero.
func ParseDuration(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			return 0, fmt.Errorf("%q is negative", raw)
		}
		return d, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 {
			return 0, fmt.Errorf("%q is negative", raw)
		}
		return time.Duration(n) * time.Second, nil
	}
	return 0, fmt.Errorf("%q is not a duration (want e.g. 45s, 10m)", raw)
}

// parseClock reads "HH:MM" (or "HH:MM:SS") into minutes and seconds of the day.
func parseClock(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("%q is not a time of day (want HH:MM)", raw)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("%q has an invalid hour", raw)
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("%q has an invalid minute", raw)
	}
	seconds := 0
	if len(parts) == 3 {
		seconds, err = strconv.Atoi(parts[2])
		if err != nil || seconds < 0 || seconds > 59 {
			return 0, fmt.Errorf("%q has an invalid second", raw)
		}
	}
	return hour*3600 + minute*60 + seconds, nil
}

// Within reports whether t falls inside the schedule's window on its local day.
// The window is half-open: [From, To). An empty To makes the window one minute
// long, which is how "at 7:30" is expressed — a rising edge, not a permanent
// true.
func (s *Schedule) Within(t time.Time) (bool, error) {
	if s == nil {
		return true, nil
	}
	from, err := parseClock(s.From)
	if err != nil {
		return false, err
	}
	to := from + 60
	if strings.TrimSpace(s.To) != "" {
		to, err = parseClock(s.To)
		if err != nil {
			return false, err
		}
	}
	if len(s.Days) > 0 {
		day := strings.ToLower(t.Format("Mon"))[:3]
		found := false
		for _, d := range s.Days {
			if strings.ToLower(strings.TrimSpace(d)) == day {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	sec := t.Hour()*3600 + t.Minute()*60 + t.Second()
	if from == to {
		// A zero-length window would never be true; treat it as the whole day so
		// a rule does not silently stop working after a typo.
		return true, nil
	}
	if from < to {
		return sec >= from && sec < to, nil
	}
	// Past midnight: 22:00–06:00 is true from 22:00 to 23:59 and 00:00 to 06:00.
	return sec >= from || sec < to, nil
}

// Normalize validates a set and returns its canonical form: trimmed names,
// lower-cased ids of groups unchanged, defaults filled in, duplicates rejected.
func Normalize(set Set) (Set, error) {
	if len(set.Rules) > MaxRules {
		return Set{}, invalid("%d rules, max %d", len(set.Rules), MaxRules)
	}
	if len(set.Groups) > MaxGroups {
		return Set{}, invalid("%d groups, max %d", len(set.Groups), MaxGroups)
	}

	groups := make([]Group, 0, len(set.Groups))
	groupNames := map[string]bool{}
	for i, g := range set.Groups {
		g.Name = strings.TrimSpace(g.Name)
		if g.Name == "" {
			return Set{}, invalid("group %d has no name", i)
		}
		if utf8.RuneCountInString(g.Name) > MaxNameLen {
			return Set{}, invalid("group name is longer than %d characters", MaxNameLen)
		}
		if groupNames[g.Name] {
			return Set{}, invalid("group %q appears twice", g.Name)
		}
		groupNames[g.Name] = true
		groups = append(groups, g)
	}

	rules := make([]Rule, 0, len(set.Rules))
	ids := map[string]bool{}
	for i, r := range set.Rules {
		r.ID = strings.TrimSpace(r.ID)
		r.Name = strings.TrimSpace(r.Name)
		r.Group = strings.TrimSpace(r.Group)
		if r.ID == "" {
			return Set{}, invalid("rule %d has no id", i)
		}
		if len(r.ID) > 64 {
			return Set{}, invalid("rule id %q is longer than 64 characters", r.ID)
		}
		if ids[r.ID] {
			return Set{}, invalid("rule %q appears twice", r.ID)
		}
		ids[r.ID] = true
		if r.Name == "" {
			r.Name = r.ID
		}
		if utf8.RuneCountInString(r.Name) > MaxNameLen {
			return Set{}, invalid("rule %q name is longer than %d characters", r.ID, MaxNameLen)
		}
		if r.Group != "" && !groupNames[r.Group] {
			return Set{}, invalid("rule %q names unknown group %q", r.ID, r.Group)
		}

		if len(r.When) > MaxWhen {
			return Set{}, invalid("rule %q has %d conditions, max %d", r.ID, len(r.When), MaxWhen)
		}
		for j := range r.When {
			c := &r.When[j]
			// Trimmed in place, not only compared: an id with a stray space
			// copied from the device list would look right in the editor and
			// never match a device at run time.
			c.Device = strings.TrimSpace(c.Device)
			c.Code = strings.TrimSpace(c.Code)
			c.Op = strings.TrimSpace(c.Op)
			if c.Device == "" {
				return Set{}, invalid("rule %q condition %d has no device", r.ID, j)
			}
			if c.Code == "" {
				return Set{}, invalid("rule %q condition %d has no code", r.ID, j)
			}
			if !validOps[c.Op] {
				return Set{}, invalid("rule %q condition %d has operator %q (want one of <, <=, >, >=, ==, !=)", r.ID, j, c.Op)
			}
			if c.Value == nil {
				return Set{}, invalid("rule %q condition %d has no value", r.ID, j)
			}
		}
		if r.At != nil {
			if _, err := parseClock(r.At.From); err != nil {
				return Set{}, invalid("rule %q: %v", r.ID, err)
			}
			if strings.TrimSpace(r.At.To) != "" {
				if _, err := parseClock(r.At.To); err != nil {
					return Set{}, invalid("rule %q: %v", r.ID, err)
				}
			}
			for _, d := range r.At.Days {
				if !validDays[strings.ToLower(strings.TrimSpace(d))] {
					return Set{}, invalid("rule %q has day %q (want mon..sun)", r.ID, d)
				}
			}
		}
		if len(r.When) == 0 && r.At == nil {
			return Set{}, invalid("rule %q has neither conditions nor a schedule, so it would never fire", r.ID)
		}
		if len(r.Actions) == 0 {
			return Set{}, invalid("rule %q has no actions", r.ID)
		}
		if len(r.Actions) > MaxActions {
			return Set{}, invalid("rule %q has %d actions, max %d", r.ID, len(r.Actions), MaxActions)
		}
		for j := range r.Actions {
			a := &r.Actions[j]
			a.Kind = strings.TrimSpace(a.Kind)
			a.Device = strings.TrimSpace(a.Device)
			a.Code = strings.TrimSpace(a.Code)
			a.Scene = strings.TrimSpace(a.Scene)
			switch a.Kind {
			case ActionCommand:
				if a.Device == "" || a.Code == "" {
					return Set{}, invalid("rule %q action %d needs device and code", r.ID, j)
				}
				if a.Value == nil {
					return Set{}, invalid("rule %q action %d has no value", r.ID, j)
				}
			case ActionScene:
				if a.Scene == "" {
					return Set{}, invalid("rule %q action %d needs a scene", r.ID, j)
				}
			default:
				return Set{}, invalid("rule %q action %d has kind %q (want command or scene)", r.ID, j, a.Kind)
			}
		}

		switch strings.ToLower(strings.TrimSpace(r.Mode)) {
		case "":
			r.Mode = DefaultMode
		case DefaultMode:
			r.Mode = DefaultMode
		case LevelMode:
			r.Mode = LevelMode
		default:
			return Set{}, invalid("rule %q mode is %q (want edge or level)", r.ID, r.Mode)
		}

		if _, err := ParseDuration(r.For); err != nil {
			return Set{}, invalid("rule %q: for: %v", r.ID, err)
		}
		if _, err := ParseDuration(r.Cooldown); err != nil {
			return Set{}, invalid("rule %q: cooldown: %v", r.ID, err)
		}
		rules = append(rules, r)
	}
	return Set{Groups: groups, Rules: rules}, nil
}

// GroupOf returns the rule's group state: the name and whether it is enabled.
// A rule with no group is governed by its own switch alone.
func (s Set) GroupEnabled(name string) bool {
	if name == "" {
		return true
	}
	for _, g := range s.Groups {
		if g.Name == name {
			return g.Enabled
		}
	}
	// A rule pointing at a group that is gone is treated as ungrouped rather
	// than dead: losing the group must not silently disable automations.
	return true
}

// Find returns a rule by id.
func (s Set) Find(id string) (Rule, bool) {
	for _, r := range s.Rules {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}
