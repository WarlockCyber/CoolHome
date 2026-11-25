package rules

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// fakeExec records the commands it is asked to send and can be told to fail for
// one device, which is what the quota refusal looks like from here.
type fakeExec struct {
	mu    sync.Mutex
	sent  []string
	fail  map[string]error
	after func()
}

func (f *fakeExec) Command(_ context.Context, id string, cmds []tuya.Command) (home.DeviceView, error) {
	f.mu.Lock()
	fail := f.fail[id]
	f.mu.Unlock()
	if fail != nil {
		return home.DeviceView{}, fail
	}
	f.mu.Lock()
	for _, c := range cmds {
		f.sent = append(f.sent, id+":"+c.Code)
	}
	after := f.after
	f.mu.Unlock()
	if after != nil {
		after()
	}
	return home.DeviceView{Device: tuya.Device{ID: id}}, nil
}

func (f *fakeExec) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// fakeHome is a mutable snapshot the test drives.
type fakeHome struct {
	mu   sync.Mutex
	snap home.Snapshot
}

func (h *fakeHome) set(id, code string, value any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.snap.Devices {
		if h.snap.Devices[i].ID == id {
			found := false
			for j := range h.snap.Devices[i].Status {
				if h.snap.Devices[i].Status[j].Code == code {
					h.snap.Devices[i].Status[j].Value = value
					found = true
				}
			}
			if !found {
				h.snap.Devices[i].Status = append(h.snap.Devices[i].Status,
					tuya.DeviceStatus{Code: code, Value: value})
			}
			return
		}
	}
	// Status lives inside the embedded Device on DeviceView.
	view := home.DeviceView{Device: tuya.Device{ID: id, Online: true}}
	view.Device.Status = []tuya.DeviceStatus{{Code: code, Value: value}}
	h.snap.Devices = append(h.snap.Devices, view)
}

func (h *fakeHome) snapshot() home.Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snap
}

// clock is a movable now.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestRunner(t *testing.T, set Set) (*Runner, *fakeExec, *fakeHome, *clock) {
	t.Helper()
	store := New("")
	if _, err := store.Save(set); err != nil {
		t.Fatalf("save: %v", err)
	}
	exec := &fakeExec{fail: map[string]error{}}
	hm := &fakeHome{}
	cl := &clock{t: time.Date(2026, 11, 25, 10, 0, 0, 0, msk)}
	r, err := NewRunner(Options{
		Store: store, Executor: exec, Snapshot: hm.snapshot,
		Interval: time.Minute, Location: msk, Now: cl.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, exec, hm, cl
}

func TestRunnerDoesNotActOnTheFirstTick(t *testing.T) {
	set := one("r", "<", 180)
	r, exec, hm, _ := newTestRunner(t, set)
	hm.set("sensor", "temp", float64(150))

	r.Step(context.Background())
	if exec.sentCount() != 0 {
		t.Fatalf("the first tick sent %d commands; it must only record state", exec.sentCount())
	}
	if primed, _, _ := r.Status(); !primed {
		t.Error("the runner is not primed after its first tick")
	}
}

func TestRunnerFiresOnChange(t *testing.T) {
	set := one("r", "<", 180)
	r, exec, hm, cl := newTestRunner(t, set)
	hm.set("sensor", "temp", float64(250))
	r.Step(context.Background()) // prime: warm

	cl.advance(time.Minute)
	hm.set("sensor", "temp", float64(150))
	r.Step(context.Background())

	if got := exec.sentCount(); got != 1 {
		t.Fatalf("sent %d commands, want 1", got)
	}
	journal := r.Journal()
	if len(journal) != 1 || journal[0].RuleID != "r" || journal[0].Error != "" {
		t.Fatalf("journal = %+v", journal)
	}
}

// Level mode keeps the device in the wanted state; a command that would change
// nothing is skipped, so a lamp that is already on does not get a write every
// tick.
func TestRunnerSkipsCommandsThatChangeNothing(t *testing.T) {
	set := one("r", "<", 180)
	set.Rules[0].Mode = LevelMode
	r, exec, hm, cl := newTestRunner(t, set)

	hm.set("sensor", "temp", float64(150))
	hm.set("plug", "switch_1", true) // already in the wanted state
	r.Step(context.Background())

	cl.advance(time.Minute)
	r.Step(context.Background())

	if exec.sentCount() != 0 {
		t.Fatalf("sent %d commands for a device already in the wanted state", exec.sentCount())
	}
	journal := r.Journal()
	if len(journal) != 1 || !journal[0].Skipped {
		t.Fatalf("journal = %+v, want one skipped firing", journal)
	}
}

func TestRunnerRecordsAFailedActionAndContinues(t *testing.T) {
	set := Set{Rules: []Rule{{
		ID: "r", Name: "r", Enabled: true,
		When: []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
		Actions: []Action{
			{Kind: ActionCommand, Device: "blocked", Code: "switch_1", Value: true},
			{Kind: ActionCommand, Device: "fine", Code: "switch_1", Value: true},
		},
	}}}
	r, exec, hm, cl := newTestRunner(t, set)
	exec.fail["blocked"] = errors.New("tuya api error 60001001: Your controllable device pool quota is insufficient")

	hm.set("sensor", "temp", float64(250))
	r.Step(context.Background())
	cl.advance(time.Minute)
	hm.set("sensor", "temp", float64(150))
	r.Step(context.Background())

	journal := r.Journal()
	if len(journal) != 1 {
		t.Fatalf("journal = %+v", journal)
	}
	if !strings.Contains(journal[0].Error, "60001001") {
		t.Errorf("the refusal is not in the journal: %+v", journal[0])
	}
	// The second action still ran.
	if exec.sentCount() != 1 || !strings.Contains(exec.sent[0], "fine") {
		t.Errorf("the second action did not run: %v", exec.sent)
	}
}

// The budget bounds one tick: a pile of rules firing together must not turn into
// a burst of cloud writes.
func TestRunnerRespectsTheBudget(t *testing.T) {
	var rules []Rule
	for i := 0; i < 10; i++ {
		rules = append(rules, Rule{
			ID: "r" + string(rune('a'+i)), Name: "r", Enabled: true,
			When:    []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
			Actions: []Action{{Kind: ActionCommand, Device: "plug", Code: "switch_1", Value: true}},
		})
	}
	store := New("")
	if _, err := store.Save(Set{Rules: rules}); err != nil {
		t.Fatal(err)
	}
	exec := &fakeExec{fail: map[string]error{}}
	hm := &fakeHome{}
	cl := &clock{t: time.Date(2026, 11, 25, 10, 0, 0, 0, msk)}
	r, err := NewRunner(Options{
		Store: store, Executor: exec, Snapshot: hm.snapshot,
		Interval: time.Minute, Location: msk, Now: cl.now, Budget: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	hm.set("sensor", "temp", float64(250))
	r.Step(context.Background())
	cl.advance(time.Minute)
	hm.set("sensor", "temp", float64(150))
	r.Step(context.Background())

	if got := exec.sentCount(); got != 3 {
		t.Fatalf("sent %d commands with a budget of 3", got)
	}
	// The rest are in the journal as skipped, not silently dropped.
	var skipped int
	for _, e := range r.Journal() {
		if strings.Contains(e.Error, "бюджет") {
			skipped++
		}
	}
	if skipped != 7 {
		t.Errorf("journal explains %d skipped rules, want 7", skipped)
	}
}

// A scene is a device with one button in this app, so a scene action goes through
// the same validated command path.
func TestRunnerTriggersScenes(t *testing.T) {
	set := Set{Rules: []Rule{{
		ID: "r", Name: "r", Enabled: true,
		When:    []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
		Actions: []Action{{Kind: ActionScene, Scene: "scene-1"}},
	}}}
	r, exec, hm, cl := newTestRunner(t, set)
	hm.set("sensor", "temp", float64(250))
	r.Step(context.Background())
	cl.advance(time.Minute)
	hm.set("sensor", "temp", float64(150))
	r.Step(context.Background())

	if exec.sentCount() != 1 || exec.sent[0] != "scene-1:"+sceneTriggerCode {
		t.Fatalf("scene was not triggered: %v", exec.sent)
	}
}

// Turning a rule off in the store stops it on the next tick, without a restart.
func TestToggleTakesEffectOnTheNextTick(t *testing.T) {
	set := one("r", "<", 180)
	store := New("")
	if _, err := store.Save(set); err != nil {
		t.Fatal(err)
	}
	exec := &fakeExec{fail: map[string]error{}}
	hm := &fakeHome{}
	cl := &clock{t: time.Date(2026, 11, 25, 10, 0, 0, 0, msk)}
	r, err := NewRunner(Options{
		Store: store, Executor: exec, Snapshot: hm.snapshot,
		Interval: time.Minute, Location: msk, Now: cl.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	hm.set("sensor", "temp", float64(250))
	r.Step(context.Background())

	// The person switches the rule off — the answer to "the automation fights me".
	if _, err := store.Update(func(s *Set) error {
		s.Rules[0].Enabled = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	cl.advance(time.Minute)
	hm.set("sensor", "temp", float64(150))
	r.Step(context.Background())
	if exec.sentCount() != 0 {
		t.Fatalf("a disabled rule fired: %v", exec.sent)
	}
}

func TestNewRunnerRequiresItsParts(t *testing.T) {
	cases := map[string]Options{
		"no store":    {Executor: &fakeExec{}, Snapshot: func() home.Snapshot { return home.Snapshot{} }},
		"no executor": {Store: New(""), Snapshot: func() home.Snapshot { return home.Snapshot{} }},
		"no snapshot": {Store: New(""), Executor: &fakeExec{}},
	}
	for name, opts := range cases {
		if _, err := NewRunner(opts); err == nil {
			t.Errorf("%s: accepted an incomplete runner", name)
		}
	}
}

// A rule whose target is already in the wanted state sends nothing. Counting its
// "уже так" actions against the budget let one no-op level rule with many actions
// starve every rule after it, indefinitely.
func TestNoOpRuleDoesNotEatTheBudget(t *testing.T) {
	set := Set{Rules: []Rule{
		{
			ID: "noop", Name: "noop", Enabled: true, Mode: LevelMode,
			When:    []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
			Actions: []Action{{Kind: ActionCommand, Device: "already", Code: "switch_1", Value: true}},
		},
		{
			ID: "real", Name: "real", Enabled: true,
			When:    []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
			Actions: []Action{{Kind: ActionCommand, Device: "fresh", Code: "switch_1", Value: true}},
		},
	}}
	store := New("")
	if _, err := store.Save(set); err != nil {
		t.Fatal(err)
	}
	exec := &fakeExec{fail: map[string]error{}}
	hm := &fakeHome{}
	cl := &clock{t: t2("10:00")}
	r, err := NewRunner(Options{
		Store: store, Executor: exec, Snapshot: hm.snapshot,
		Interval: time.Minute, Location: msk, Now: cl.now, Budget: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Prime with the condition false, so the later step is a real rise.
	hm.set("sensor", "temp", float64(250))
	hm.set("already", "switch_1", true)
	r.Step(context.Background())

	cl.advance(time.Minute)
	hm.set("sensor", "temp", float64(150))
	r.Step(context.Background())

	if exec.sentCount() != 1 || !strings.Contains(exec.sent[0], "fresh") {
		t.Fatalf("sent %v, want the real rule's command: a no-op must cost nothing", exec.sent)
	}
}

// A rule dropped by the budget must keep its turn: the engine marks a firing
// before the cost is known, and an edge rule that lost that mark would never fire
// for that trigger at all.
func TestBudgetStarvedRuleKeepsItsTurn(t *testing.T) {
	set := Set{Rules: []Rule{
		{
			ID: "first", Name: "first", Enabled: true,
			When:    []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
			Actions: []Action{{Kind: ActionCommand, Device: "a", Code: "switch_1", Value: true}},
		},
		{
			ID: "second", Name: "second", Enabled: true,
			When:    []Condition{{Device: "sensor", Code: "temp", Op: "<", Value: 180}},
			Actions: []Action{{Kind: ActionCommand, Device: "b", Code: "switch_1", Value: true}},
		},
	}}
	store := New("")
	if _, err := store.Save(set); err != nil {
		t.Fatal(err)
	}
	exec := &fakeExec{fail: map[string]error{}}
	hm := &fakeHome{}
	cl := &clock{t: t2("10:00")}
	r, err := NewRunner(Options{
		Store: store, Executor: exec, Snapshot: hm.snapshot,
		Interval: time.Minute, Location: msk, Now: cl.now, Budget: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	hm.set("sensor", "temp", float64(250))
	r.Step(context.Background())

	cl.advance(time.Minute)
	hm.set("sensor", "temp", float64(150))
	r.Step(context.Background())
	if exec.sentCount() != 1 {
		t.Fatalf("sent %d, want 1 (budget)", exec.sentCount())
	}

	// The second rule was starved. On the next tick its command must go.
	cl.advance(time.Minute)
	r.Step(context.Background())
	if exec.sentCount() != 2 || !strings.Contains(exec.sent[1], "b") {
		t.Fatalf("sent %v; the starved rule lost its turn", exec.sent)
	}
}
