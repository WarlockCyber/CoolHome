package rules

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// at builds a time in a fixed zone so a test never depends on the machine's.
var msk = time.FixedZone("MSK", 3*3600)

func t2(hhmm string) time.Time {
	var h, m int
	if _, err := time.Parse("15:04", hhmm); err != nil {
		panic(err)
	}
	h = int(hhmm[0]-'0')*10 + int(hhmm[1]-'0')
	m = int(hhmm[3]-'0')*10 + int(hhmm[4]-'0')
	return time.Date(2026, 11, 25, h, m, 0, 0, msk)
}

// TestReadingsOfIndexesStatus confirms the adapter from the home snapshot: only
// reported points appear, and a device with none contributes an empty map — which
// is what makes its conditions unknown rather than false.
func TestReadingsOfIndexesStatus(t *testing.T) {
	views := []home.DeviceView{
		{Device: tuya.Device{ID: "a"}, Spec: nil},
		{Device: tuya.Device{ID: "b"}},
	}
	views[0].Status = nil
	r := ReadingsOf(views)
	if _, ok := r["a"]; !ok {
		t.Fatal("a device with no status must still have an entry")
	}
	if len(r["a"]) != 0 {
		t.Errorf("a device with no status has %d points", len(r["a"]))
	}
}

func readings(pairs map[string]map[string]any) Readings {
	out := make(Readings, len(pairs))
	for id, values := range pairs {
		out[id] = values
	}
	return out
}

// one is a rule that reacts to a single reading, for the table tests.
func one(id string, op string, value any, actions ...Action) Set {
	if len(actions) == 0 {
		actions = []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}}
	}
	return Set{Rules: []Rule{{
		ID:      id,
		Name:    id,
		Enabled: true,
		When:    []Condition{{Device: "sensor", Code: "temp", Op: op, Value: value}},
		Actions: actions,
	}}}
}

// fire runs one tick and reports whether the rule fired.
func fire(t *testing.T, set Set, r Readings, state State, now time.Time) (bool, State) {
	t.Helper()
	decisions, next, errs := Evaluate(set, r, state, now, msk)
	if len(errs) > 0 {
		t.Fatalf("evaluation reported errors: %v", errs)
	}
	fired := false
	for _, d := range decisions {
		if len(d.Actions) == 0 {
			t.Fatalf("decision for %s carries no actions", d.RuleID)
		}
		fired = true
	}
	return fired, next
}

var (
	cold   = readings(map[string]map[string]any{"sensor": {"temp": float64(150)}})
	warm   = readings(map[string]map[string]any{"sensor": {"temp": float64(250)}})
	absent = readings(map[string]map[string]any{"sensor": {}})
)

// The first evaluation records state and fires nothing: after a restart every
// rule whose condition holds would otherwise fire at once.
func TestFirstTickPrimesAndDoesNotFire(t *testing.T) {
	fired, state := fire(t, one("r", "<", 180), cold, NewState(), t2("10:00"))
	if fired {
		t.Fatal("the first tick fired; it must only record state")
	}
	if !state.Primed() {
		t.Fatal("state is not primed after the first evaluation")
	}
}

func TestEdgeFiresOncePerRun(t *testing.T) {
	// prime on warm: condition false
	fired, state := fire(t, one("r", "<", 180), warm, NewState(), t2("10:00"))
	if fired {
		t.Fatal("fired while the condition was false")
	}
	// becomes cold: rising edge
	fired, state = fire(t, one("r", "<", 180), cold, state, t2("10:01"))
	if !fired {
		t.Fatal("did not fire on the rising edge")
	}
	// stays cold: no repeat
	for i := 2; i < 6; i++ {
		fired, state = fire(t, one("r", "<", 180), cold, state, t2("10:0"+string(rune('0'+i))))
		if fired {
			t.Fatalf("edge rule fired again on tick %d while the condition still held", i)
		}
	}
	// warms up, then cold again: a new run, a new fire
	fired, state = fire(t, one("r", "<", 180), warm, state, t2("10:10"))
	if fired {
		t.Fatal("fired on the falling edge")
	}
	fired, _ = fire(t, one("r", "<", 180), cold, state, t2("10:11"))
	if !fired {
		t.Fatal("did not fire on the second rising edge")
	}
}

func TestLevelFiresWhileTrueUntilCooldown(t *testing.T) {
	set := one("r", "<", 180)
	set.Rules[0].Mode = LevelMode
	set.Rules[0].Cooldown = "10m"

	fired, state := fire(t, set, cold, NewState(), t2("10:00"))
	if fired {
		t.Fatal("fired on the priming tick")
	}
	fired, state = fire(t, set, cold, state, t2("10:01"))
	if !fired {
		t.Fatal("level rule did not fire while the condition held")
	}
	// inside the cooldown
	fired, state = fire(t, set, cold, state, t2("10:05"))
	if fired {
		t.Fatal("fired inside the cooldown")
	}
	// after the cooldown
	fired, _ = fire(t, set, cold, state, t2("10:12"))
	if !fired {
		t.Fatal("did not fire after the cooldown expired")
	}
}

// A reading that has not arrived is unknown, not false: a sensor dropping off
// the network must not fire the falling edge of a rule, and must not restart the
// hold timer of one that is waiting.
func TestMissingValueIsNotFalse(t *testing.T) {
	set := one("r", "<", 180)
	// Prime on a state where the condition is false: a rule that is already true
	// at startup waits for the next real rise instead of acting.
	fired, state := fire(t, set, warm, NewState(), t2("10:00"))
	if fired {
		t.Fatal("priming tick fired")
	}
	fired, state = fire(t, set, cold, state, t2("10:01"))
	if !fired {
		t.Fatal("did not fire on the rise")
	}

	// The sensor stops reporting. Edge mode must not fire again, and the state
	// must stay "true" rather than flipping to false and back.
	fired, state = fire(t, set, absent, state, t2("10:02"))
	if fired {
		t.Fatal("fired while the reading was missing")
	}
	fired, _ = fire(t, set, cold, state, t2("10:03"))
	if fired {
		t.Fatal("fired again after the gap — the run was restarted by a missing reading")
	}
}

func TestForRequiresTheConditionToHold(t *testing.T) {
	set := one("r", "<", 180)
	set.Rules[0].For = "5m"

	// Prime with the condition false: the run has to start with a real rise.
	fired, state := fire(t, set, warm, NewState(), t2("10:00"))
	if fired {
		t.Fatal("priming tick fired")
	}
	fired, state = fire(t, set, cold, state, t2("10:01"))
	if fired {
		t.Fatal("fired on the tick the condition became true")
	}
	for _, at := range []string{"10:02", "10:05"} {
		fired, state = fire(t, set, cold, state, t2(at))
		if fired {
			t.Fatalf("fired at %s, before the 5m hold elapsed", at)
		}
	}
	fired, _ = fire(t, set, cold, state, t2("10:06"))
	if !fired {
		t.Fatal("did not fire once the hold elapsed")
	}
}

// A reading that flaps must not accumulate hold time: each drop restarts it.
func TestForRestartsOnFlap(t *testing.T) {
	set := one("r", "<", 180)
	set.Rules[0].For = "5m"

	fired, state := fire(t, set, warm, NewState(), t2("10:00"))
	if fired {
		t.Fatal("priming tick fired")
	}
	fired, state = fire(t, set, cold, state, t2("10:01"))
	if fired {
		t.Fatal("fired on the rise")
	}
	fired, state = fire(t, set, cold, state, t2("10:03"))
	if fired {
		t.Fatal("fired early")
	}
	// warms up briefly
	fired, state = fire(t, set, warm, state, t2("10:04"))
	if fired {
		t.Fatal("fired on the falling edge")
	}
	// cold again: the timer restarts at 10:05
	fired, state = fire(t, set, cold, state, t2("10:05"))
	if fired {
		t.Fatal("fired immediately after the flap")
	}
	fired, state = fire(t, set, cold, state, t2("10:09"))
	if fired {
		t.Fatalf("fired before the restarted hold elapsed")
	}
	fired, _ = fire(t, set, cold, state, t2("10:10"))
	if !fired {
		t.Fatal("did not fire after the restarted hold — the flap was not counted")
	}
}

func TestDisabledRuleNeverFires(t *testing.T) {
	set := one("r", "<", 180)
	set.Rules[0].Enabled = false

	fired, state := fire(t, set, cold, NewState(), t2("10:00"))
	if fired {
		t.Fatal("a disabled rule fired")
	}
	for i := 1; i < 5; i++ {
		fired, state = fire(t, set, cold, state, t2("10:0"+string(rune('0'+i))))
		if fired {
			t.Fatal("a disabled rule fired on a later tick")
		}
	}
}

// A group switch stops its rules without touching their individual switches, and
// turning it back on restores the same set with a fresh run.
func TestGroupSwitchGatesItsRules(t *testing.T) {
	set := Set{
		Groups: []Group{{Name: "Отопление", Enabled: false}},
		Rules: []Rule{{
			ID: "r", Name: "r", Group: "Отопление", Enabled: true,
			When:    []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
			Actions: []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}},
		}},
	}
	fired, state := fire(t, set, cold, NewState(), t2("10:00"))
	if fired {
		t.Fatal("a rule in a disabled group fired")
	}
	fired, state = fire(t, set, cold, state, t2("10:01"))
	if fired {
		t.Fatal("a rule in a disabled group fired on a later tick")
	}

	// Turn the group on. The rule must fire on the next tick — its run starts
	// then, not retroactively from the disabled period.
	set.Groups[0].Enabled = true
	fired, _ = fire(t, set, cold, state, t2("10:02"))
	if !fired {
		t.Fatal("the rule did not fire after its group was enabled")
	}
}

func TestUnknownGroupDoesNotDisableTheRule(t *testing.T) {
	// Losing a group must not silently stop an automation: the rule is treated
	// as ungrouped instead.
	set := Set{Rules: []Rule{{
		ID: "r", Name: "r", Group: "удалённая", Enabled: true,
		When:    []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
		Actions: []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}},
	}}}
	if !set.GroupEnabled("удалённая") {
		t.Fatal("a missing group disabled its rule")
	}
}

// ---- schedule ----

func scheduleRule(from, to string, days ...string) Set {
	return Set{Rules: []Rule{{
		ID: "r", Name: "r", Enabled: true,
		At:      &Schedule{From: from, To: to, Days: days},
		Actions: []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}},
	}}}
}

func TestScheduleFiresAtTheStartOfItsWindow(t *testing.T) {
	set := scheduleRule("07:30", "")

	fired, state := fire(t, set, readings(nil), NewState(), t2("07:29"))
	if fired {
		t.Fatal("fired before the window")
	}
	fired, state = fire(t, set, readings(nil), state, t2("07:30"))
	if !fired {
		t.Fatal("did not fire when the window opened")
	}
	fired, state = fire(t, set, readings(nil), state, t2("07:31"))
	if fired {
		t.Fatal("fired again inside the window; the rule must fire once at its start")
	}
	// Next day, same time: a new window, a new fire.
	fired, _ = fire(t, set, readings(nil), state, time.Date(2026, 11, 26, 7, 30, 0, 0, msk))
	if !fired {
		t.Fatal("did not fire on the next day")
	}
}

func TestScheduleWindowCompare(t *testing.T) {
	s := &Schedule{From: "10:00", To: "12:00"}
	cases := map[string]bool{
		"09:59": false,
		"10:00": true,
		"11:59": true,
		"12:00": false,
		"13:00": false,
	}
	for clock, want := range cases {
		got, err := s.Within(t2(clock))
		if err != nil {
			t.Fatalf("%s: %v", clock, err)
		}
		if got != want {
			t.Errorf("%s: within = %v, want %v", clock, got, want)
		}
	}
}

func TestScheduleAcrossMidnight(t *testing.T) {
	s := &Schedule{From: "22:00", To: "06:00"}
	cases := map[string]bool{
		"21:59": false,
		"22:00": true,
		"23:59": true,
		"00:00": true,
		"05:59": true,
		"06:00": false,
		"12:00": false,
	}
	for clock, want := range cases {
		got, err := s.Within(t2(clock))
		if err != nil {
			t.Fatalf("%s: %v", clock, err)
		}
		if got != want {
			t.Errorf("%s: within = %v, want %v", clock, got, want)
		}
	}
}

func TestScheduleDaysGateTheWindow(t *testing.T) {
	// 2026-11-25 is a Wednesday.
	set := scheduleRule("07:30", "", "sat", "sun")
	fired, _ := fire(t, set, readings(nil), NewState(), t2("07:30"))
	if fired {
		t.Fatal("fired on a weekday for a weekend-only rule")
	}
}

// A schedule with conditions gates them: "if cold AND at night" must not fire
// in the afternoon even though the reading is cold.
func TestScheduleGatesConditions(t *testing.T) {
	set := Set{Rules: []Rule{{
		ID: "r", Name: "r", Enabled: true,
		When:    []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
		At:      &Schedule{From: "22:00", To: "06:00"},
		Actions: []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}},
	}}}
	fired, state := fire(t, set, cold, NewState(), t2("14:00"))
	if fired {
		t.Fatal("priming tick fired")
	}
	fired, state = fire(t, set, cold, state, t2("14:01"))
	if fired {
		t.Fatal("fired in the afternoon: the schedule did not gate the condition")
	}
	fired, state = fire(t, set, cold, state, t2("22:00"))
	if !fired {
		t.Fatal("did not fire when the window opened with a cold reading")
	}
}

// ---- comparisons ----

func TestCompareNumericAndText(t *testing.T) {
	cases := []struct {
		reading any
		op      string
		want    any
		ok      bool
	}{
		{float64(150), "<", 180, true},
		{float64(150), "<", "180", true}, // value written as a string
		{float64(180), "<=", 180, true},
		{float64(181), "<=", 180, false},
		{float64(150), ">", 180, false},
		{"middle", "==", "middle", true}, // enum comparison
		{"middle", "!=", "low", true},
		{"low", "==", "middle", false},
		{true, "==", true, true},
		{true, "!=", false, true},
	}
	for _, c := range cases {
		got, err := compare(c.reading, c.op, c.want)
		if err != nil {
			t.Errorf("%v %s %v: %v", c.reading, c.op, c.want, err)
			continue
		}
		if got != c.ok {
			t.Errorf("%v %s %v = %v, want %v", c.reading, c.op, c.want, got, c.ok)
		}
	}
}

func TestCompareOrderingOnTextIsAnError(t *testing.T) {
	// ">" on an enum is a mistake in the rule; it must not silently be false.
	if _, err := compare("middle", ">", "low"); err == nil {
		t.Fatal("ordering comparison on text did not fail")
	}
}

func TestAllConditionsMustHold(t *testing.T) {
	set := Set{Rules: []Rule{{
		ID: "r", Name: "r", Enabled: true,
		When: []Condition{
			{Device: "sensor", Code: "temp", Op: "<", Value: 180},
			{Device: "sensor", Code: "battery", Op: "!=", Value: "low"},
		},
		Actions: []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}},
	}}}
	ok := readings(map[string]map[string]any{"sensor": {"temp": float64(150), "battery": "high"}})
	lowBat := readings(map[string]map[string]any{"sensor": {"temp": float64(150), "battery": "low"}})

	_, state := fire(t, set, ok, NewState(), t2("10:00"))
	fired, _ := fire(t, set, lowBat, state, t2("10:01"))
	if fired {
		t.Fatal("fired although the second condition did not hold")
	}
	// And with the battery back to normal, from a primed state, it fires.
	_, state = fire(t, set, lowBat, NewState(), t2("10:00"))
	fired, _ = fire(t, set, ok, state, t2("10:01"))
	if !fired {
		t.Fatal("did not fire when both conditions held")
	}
}

// ---- validation ----

func TestNormalizeFillsDefaultsAndTrims(t *testing.T) {
	set, err := Normalize(Set{Rules: []Rule{{
		ID: "  r1  ", Name: "  Холодно  ",
		When:    []Condition{{Device: " d ", Code: " c ", Op: "<", Value: 1}},
		Actions: []Action{{Kind: " command ", Device: " p ", Code: " s ", Value: true}},
	}}})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	r := set.Rules[0]
	if r.ID != "r1" || r.Name != "Холодно" {
		t.Errorf("id/name = %q/%q", r.ID, r.Name)
	}
	if r.Mode != DefaultMode {
		t.Errorf("mode = %q, want %q", r.Mode, DefaultMode)
	}
	if r.When[0].Device != "d" || r.Actions[0].Kind != ActionCommand {
		t.Errorf("nested values were not trimmed: %+v", r)
	}
}

func TestNormalizeRejectsBadRules(t *testing.T) {
	good := Rule{
		ID: "r", Name: "r",
		When:    []Condition{{Device: "d", Code: "c", Op: "<", Value: 1}},
		Actions: []Action{{Kind: ActionCommand, Device: "d", Code: "c", Value: true}},
	}
	mutate := func(f func(*Rule)) Set {
		r := good
		r.When = append([]Condition(nil), good.When...)
		r.Actions = append([]Action(nil), good.Actions...)
		f(&r)
		return Set{Rules: []Rule{r}}
	}
	cases := map[string]Set{
		"no id":           mutate(func(r *Rule) { r.ID = "" }),
		"no actions":      mutate(func(r *Rule) { r.Actions = nil }),
		"bad operator":    mutate(func(r *Rule) { r.When[0].Op = "=<" }),
		"no value":        mutate(func(r *Rule) { r.When[0].Value = nil }),
		"no trigger":      mutate(func(r *Rule) { r.When = nil }),
		"bad mode":        mutate(func(r *Rule) { r.Mode = "sometimes" }),
		"bad cooldown":    mutate(func(r *Rule) { r.Cooldown = "потом" }),
		"bad time":        mutate(func(r *Rule) { r.At = &Schedule{From: "25:00"} }),
		"bad day":         mutate(func(r *Rule) { r.At = &Schedule{From: "10:00", Days: []string{"пн"}} }),
		"unknown group":   mutate(func(r *Rule) { r.Group = "нет такой" }),
		"bad action kind": mutate(func(r *Rule) { r.Actions[0].Kind = "email" }),
		"scene without id": mutate(func(r *Rule) {
			r.Actions = []Action{{Kind: ActionScene}}
		}),
		"duplicate id": {Rules: []Rule{good, good}},
	}
	for name, set := range cases {
		if _, err := Normalize(set); err == nil {
			t.Errorf("%s: accepted a rule it must reject", name)
		}
	}
}

func TestNormalizeRejectsTooManyRules(t *testing.T) {
	set := Set{}
	for i := 0; i <= MaxRules; i++ {
		set.Rules = append(set.Rules, Rule{
			ID:      "r" + strings.Repeat("x", i),
			When:    []Condition{{Device: "d", Code: "c", Op: "<", Value: 1}},
			Actions: []Action{{Kind: ActionCommand, Device: "d", Code: "c", Value: true}},
		})
	}
	if _, err := Normalize(set); err == nil {
		t.Fatal("accepted more rules than the limit")
	}
}

func TestParseDurationForms(t *testing.T) {
	cases := map[string]time.Duration{
		"":      0,
		"30s":   30 * time.Second,
		"10m":   10 * time.Minute,
		"90":    90 * time.Second,
		"1h30m": 90 * time.Minute,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"-5m", "abc", "10 m"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// A hold that spans a gap in the ticks still counts: `for` is wall-clock, not a
// count of evaluations. A server that was busy for two minutes must not make a
// five-minute hold take seven.
func TestForCountsWallClockNotTicks(t *testing.T) {
	set := one("r", "<", 180)
	set.Rules[0].For = "5m"
	fired, state := fire(t, set, warm, NewState(), t2("10:00"))
	if fired {
		t.Fatal("priming tick fired")
	}
	fired, state = fire(t, set, cold, state, t2("10:01"))
	if fired {
		t.Fatal("fired on the rise")
	}
	// No evaluations between 10:01 and 10:06 — a stalled loop, a restart of the
	// poller, a long refresh. The hold is wall-clock, so it still counts.
	fired, _ = fire(t, set, cold, state, t2("10:06"))
	if !fired {
		t.Fatal("the hold did not count across the gap in ticks")
	}
}

// A window that passes while nothing is evaluated is not replayed later: a
// missed schedule does not fire at 9:00 because 7:30 was skipped. Firing late is
// worse than not firing — "turn the light on at 7:30" at nine in the morning is
// a surprise.
func TestMissedWindowDoesNotFireLate(t *testing.T) {
	set := scheduleRule("07:30", "")
	fired, state := fire(t, set, readings(nil), NewState(), t2("07:00"))
	if fired {
		t.Fatal("priming tick fired")
	}
	fired, _ = fire(t, set, readings(nil), state, t2("09:00"))
	if fired {
		t.Fatal("the rule fired at 09:00 for a window that closed at 07:31")
	}
}

// Europe/Berlin moves the clock forward at 02:00 on the last Sunday of March:
// 02:30 does not exist that day. A schedule must answer sanely rather than
// panic or invert, and an overnight window must still work on the autumn day
// when 02:30 happens twice.
func TestScheduleSurvivesTheClockChange(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no zone database on this machine")
	}
	// Spring forward: a window that would contain the missing hour.
	s := &Schedule{From: "01:00", To: "04:00"}
	for _, clock := range [][2]int{{1, 30}, {3, 30}, {4, 30}} {
		when := time.Date(2026, 3, 29, clock[0], clock[1], 0, 0, loc)
		if _, err := s.Within(when); err != nil {
			t.Fatalf("%02d:%02d on the spring jump: %v", clock[0], clock[1], err)
		}
	}
	// Autumn: 02:30 occurs twice; both occurrences must be inside an overnight
	// window, and neither may error.
	night := &Schedule{From: "22:00", To: "06:00"}
	when := time.Date(2026, 10, 25, 2, 30, 0, 0, loc)
	inside, err := night.Within(when)
	if err != nil {
		t.Fatalf("overnight window on the autumn jump: %v", err)
	}
	if !inside {
		t.Error("02:30 on the autumn jump is not inside 22:00–06:00")
	}
}

// A window that crosses midnight belongs to the day it opened on. With
// days=["fri"], Friday 22:00–Saturday 06:00 is one window: checking "now's"
// weekday made the night stop dead at midnight, and a level-mode rule stopped
// maintaining the device for the second half of the night.
func TestOvernightWindowBelongsToTheDayItOpened(t *testing.T) {
	s := &Schedule{From: "22:00", To: "06:00", Days: []string{"fri"}}
	// 2026-11-27 is a Friday, 2026-11-28 a Saturday.
	cases := []struct {
		when string
		want bool
	}{
		{"2026-11-27 23:00", true},  // Friday night
		{"2026-11-28 02:00", true},  // Saturday early morning, Friday's window
		{"2026-11-28 23:00", false}, // Saturday night
		{"2026-11-27 02:00", false}, // Friday early morning, Thursday's window
	}
	for _, c := range cases {
		when, err := time.ParseInLocation("2006-01-02 15:04", c.when, msk)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Within(when)
		if err != nil {
			t.Fatalf("%s: %v", c.when, err)
		}
		if got != c.want {
			t.Errorf("%s: within = %v, want %v", c.when, got, c.want)
		}
	}
}

// The plain window keeps its meaning: a day list with a same-day window is
// checked against that day alone.
func TestSameDayWindowChecksItsOwnDay(t *testing.T) {
	s := &Schedule{From: "09:00", To: "17:00", Days: []string{"sat"}}
	// 2026-11-25 is a Wednesday, 2026-11-28 a Saturday.
	wed, _ := time.ParseInLocation("2006-01-02 15:04", "2026-11-25 10:00", msk)
	sat, _ := time.ParseInLocation("2006-01-02 15:04", "2026-11-28 10:00", msk)
	if inside, _ := s.Within(wed); inside {
		t.Error("a Saturday-only window matched on Wednesday")
	}
	if inside, _ := s.Within(sat); !inside {
		t.Error("a Saturday-only window did not match on Saturday")
	}
}

// A missing reading in the first condition must not hide a definite false in the
// second. The old code returned "unknown" at the first gap, so the trigger stayed
// true from its previous run, kept its `since`, and the next real rise was
// swallowed: the rule fired once and never again for that run.
func TestMissingReadingDoesNotHideALaterFalse(t *testing.T) {
	set := Set{Rules: []Rule{{
		ID: "r", Name: "r", Enabled: true,
		// The battery condition is deliberately first.
		When: []Condition{
			{Device: "sensor", Code: "battery", Op: "!=", Value: "low"},
			{Device: "sensor", Code: "temp", Op: "<", Value: 180},
		},
		Actions: []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}},
	}}}

	both := readings(map[string]map[string]any{"sensor": {"temp": float64(150), "battery": "high"}})
	// Temperature reported, battery gone.
	batteryGone := readings(map[string]map[string]any{"sensor": {"temp": float64(250)}})

	// Prime with the temperature above the threshold: the rule is false, so the
	// later rise is a real one.
	warmReading := readings(map[string]map[string]any{"sensor": {"temp": float64(250), "battery": "high"}})
	fired, state := fire(t, set, warmReading, NewState(), t2("10:00"))
	if fired {
		t.Fatal("priming tick fired")
	}
	fired, state = fire(t, set, both, state, t2("10:01"))
	if !fired {
		t.Fatal("did not fire when both conditions held")
	}

	// It got warm while the battery sensor was offline: unknown, so the run is
	// not ended.
	fired, state = fire(t, set, batteryGone, state, t2("10:02"))
	if fired {
		t.Fatal("fired while a reading was missing")
	}
	// Now it is cold again and the battery is back. This is a new run and must
	// fire: the false temperature reading while the battery was missing is a fact
	// that ended the previous run.
	fired, _ = fire(t, set, both, state, t2("10:03"))
	if !fired {
		t.Fatal("the new rise was swallowed — a missing reading hid the false one")
	}
}

// A closed window ends the run even when a reading is missing: morning is a fact
// whether or not the thermometer can be reached.
func TestClosedWindowEndsTheRunDespiteAMissingReading(t *testing.T) {
	set := Set{Rules: []Rule{{
		ID: "r", Name: "r", Enabled: true,
		When:    []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
		At:      &Schedule{From: "22:00", To: "06:00"},
		Actions: []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}},
	}}}
	cold := readings(map[string]map[string]any{"sensor": {"temp": float64(150)}})
	gone := readings(map[string]map[string]any{"sensor": {}})

	// Prime before the window opens, then step inside it: the entry is the rise.
	_, state := fire(t, set, cold, NewState(), t2("21:00"))
	fired, state := fire(t, set, cold, state, t2("22:01"))
	if !fired {
		t.Fatal("did not fire inside the window")
	}
	// Morning, sensor offline. The window closed, so the run ended.
	fired, state = fire(t, set, gone, state, t2("07:00"))
	if fired {
		t.Fatal("fired outside the window")
	}
	// Next night, back inside — a new run, and it fires. The date has to move:
	// the same clock time on the same day is the same instant.
	nextNight := time.Date(2026, 11, 26, 23, 0, 0, 0, msk)
	fired, _ = fire(t, set, cold, state, nextNight)
	if !fired {
		t.Fatal("the next night's rise was swallowed")
	}
}

// A rule whose comparison cannot be carried out is reported, not treated as a
// condition that is simply false. Silence here is the worst outcome: the rule
// looks enabled, the journal stays empty, and nothing ever happens.
func TestUnevaluableRuleIsReported(t *testing.T) {
	set := Set{Rules: []Rule{{
		ID: "bad", Name: "bad", Enabled: true,
		When:    []Condition{{Device: "sensor", Code: "state", Op: ">", Value: "low"}},
		Actions: []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}},
	}}}
	r := readings(map[string]map[string]any{"sensor": {"state": "middle"}})
	decisions, _, errs := Evaluate(set, r, NewState(), t2("10:00"), msk)
	if len(decisions) != 0 {
		t.Errorf("an unevaluable rule produced %d decisions", len(decisions))
	}
	if errs["bad"] == "" {
		t.Fatal("the failure was not reported")
	}
	if !strings.Contains(errs["bad"], ">") {
		t.Errorf("the message does not name the operator: %q", errs["bad"])
	}
}

// The same, seen from the runner: the page's status line carries the reason.
func TestRunnerSurfacesAnUnevaluableRule(t *testing.T) {
	set := Set{Rules: []Rule{{
		ID: "bad", Name: "bad", Enabled: true,
		When:    []Condition{{Device: "sensor", Code: "state", Op: ">", Value: "low"}},
		Actions: []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}},
	}}}
	r, _, hm, _ := newTestRunner(t, set)
	hm.set("sensor", "state", "middle")
	r.Step(context.Background())

	_, _, lastErr := r.Status()
	if lastErr == "" {
		t.Fatal("the runner reported nothing for a rule it could not evaluate")
	}
	if !strings.Contains(lastErr, "bad") {
		t.Errorf("the message does not name the rule: %q", lastErr)
	}
}

// A rule that is already true when the server starts must not act: not
// immediately, and not `for` later either. "The hold elapsed while the server was
// down" is true but unknowable, and acting on it would turn the heater on at
// every restart.
func TestRuleAlreadyTrueAtStartupWaitsForTheNextRise(t *testing.T) {
	set := one("r", "<", 180)
	set.Rules[0].For = "5m"
	set.Rules[0].Cooldown = ""

	// Prime while already cold.
	fired, state := fire(t, set, cold, NewState(), t2("10:00"))
	if fired {
		t.Fatal("priming tick fired")
	}
	// Long after the hold would have elapsed, still no action.
	for _, at := range []string{"10:06", "10:30", "12:00"} {
		fired, state = fire(t, set, cold, state, t2(at))
		if fired {
			t.Fatalf("acted at %s on a condition that was already true at startup", at)
		}
	}
	// A real rise after the condition went false does act.
	fired, state = fire(t, set, warm, state, t2("13:00"))
	if fired {
		t.Fatal("fired on the falling edge")
	}
	fired, state = fire(t, set, cold, state, t2("13:01"))
	if fired {
		t.Fatal("fired before the hold elapsed on the new run")
	}
	fired, _ = fire(t, set, cold, state, t2("13:07"))
	if !fired {
		t.Fatal("did not fire after the hold on a genuine new run")
	}
}

// Level mode is the exception on purpose: its job is to keep the device in the
// wanted state, so after a restart it applies on the next tick.
func TestLevelRuleAppliesAfterStartup(t *testing.T) {
	set := one("r", "<", 180)
	set.Rules[0].Mode = LevelMode

	fired, state := fire(t, set, cold, NewState(), t2("10:00"))
	if fired {
		t.Fatal("priming tick acted")
	}
	fired, _ = fire(t, set, cold, state, t2("10:01"))
	if !fired {
		t.Fatal("a level rule did not apply after startup")
	}
}
