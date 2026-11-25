package rules

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// Executor is the slice of the home service the runner drives. Using the service
// rather than the cloud client means every command a rule sends is validated
// against the device's thing model exactly like one from the UI, and lands in the
// same cache, so the next tick sees it.
type Executor interface {
	Command(ctx context.Context, id string, cmds []tuya.Command) (home.DeviceView, error)
}

// DefaultBudget is how many actions one tick may send. A rule that fires and a
// rule that answers it can otherwise ping-pong faster than a person can read the
// journal; the budget makes a cycle wear itself out instead of running forever.
const DefaultBudget = 6

// JournalLength is how many recent firings are kept for the page.
const JournalLength = 50

// Entry is one recorded firing, whether it succeeded or not.
type Entry struct {
	Time   time.Time `json:"time"`
	RuleID string    `json:"rule"`
	Name   string    `json:"name"`
	// Group is the rule's group, for the journal's "which set of automations
	// is doing this" column.
	Group string `json:"group,omitempty"`
	// Actions is one record per action. It is structured rather than a sentence
	// because only the page knows what a device id is called: the journal keeps
	// the ids it acted on and lets the page name them.
	Actions []JournalAction `json:"actions"`
	// Skipped is set when nothing was sent because the device was already in the
	// wanted state. The rule still counts as fired: the condition did happen.
	Skipped bool `json:"skipped,omitempty"`
	// Preview marks an entry from a dry run: it says what would happen, not what
	// did. Without the mark the page shows it under "последние срабатывания",
	// which claims the house just did something it did not.
	Preview bool   `json:"preview,omitempty"`
	Error   string `json:"error,omitempty"`
}

// describeRuleErrors turns per-rule evaluation failures into one line for the
// page: "rule r1: operator \">\" does not apply" is what tells a person their
// rule is broken rather than waiting for a condition that never comes true.
func describeRuleErrors(errs map[string]string) string {
	ids := make([]string, 0, len(errs))
	for id := range errs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id+": "+errs[id])
	}
	return "не удалось вычислить правило " + strings.Join(parts, "; ")
}

// JournalAction is what one action did.
type JournalAction struct {
	Kind   string `json:"kind"`
	Device string `json:"device,omitempty"`
	Code   string `json:"code,omitempty"`
	Value  any    `json:"value,omitempty"`
	Scene  string `json:"scene,omitempty"`
	// Note explains a non-obvious outcome: the device was already in the wanted
	// state, or the cloud refused.
	Note string `json:"note,omitempty"`
}

// Runner evaluates the rules on a timer and carries out their decisions.
type Runner struct {
	store    *Store
	exec     Executor
	snapshot func() home.Snapshot
	log      *slog.Logger
	now      func() time.Time
	loc      *time.Location
	interval time.Duration
	budget   int

	// stepMu serialises evaluations. Step is normally called from Run's own
	// goroutine, but a second caller — a test, a future "run now" button — would
	// otherwise evaluate the same state twice and fire the same rule twice.
	stepMu sync.Mutex

	mu      sync.Mutex
	state   State
	journal []Entry
	lastRun time.Time
	lastErr string
}

// Options configure a runner. Only Store, Executor and Snapshot are required.
type Options struct {
	Store    *Store
	Executor Executor
	Snapshot func() home.Snapshot
	Logger   *slog.Logger
	// Interval is how often the rules are evaluated; it is the home's poll
	// interval, because a rule cannot see a change its input has not seen.
	Interval time.Duration
	// Location is the zone a schedule is read in. Nil means time.Local.
	Location *time.Location
	// Budget overrides DefaultBudget.
	Budget int
	// Now is injectable for tests.
	Now func() time.Time
}

// NewRunner builds a runner.
func NewRunner(opts Options) (*Runner, error) {
	if opts.Store == nil {
		return nil, errors.New("rules: runner needs a store")
	}
	if opts.Executor == nil {
		return nil, errors.New("rules: runner needs an executor")
	}
	if opts.Snapshot == nil {
		return nil, errors.New("rules: runner needs a snapshot source")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Interval <= 0 {
		opts.Interval = 10 * time.Second
	}
	if opts.Budget <= 0 {
		opts.Budget = DefaultBudget
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Runner{
		store:    opts.Store,
		exec:     opts.Executor,
		snapshot: opts.Snapshot,
		log:      opts.Logger,
		now:      opts.Now,
		loc:      opts.Location,
		interval: opts.Interval,
		budget:   opts.Budget,
		state:    NewState(),
	}, nil
}

// Run evaluates the rules until ctx is cancelled. The first evaluation only
// records the state: a restart must not fire every rule whose condition happens
// to hold at that moment.
func (r *Runner) Run(ctx context.Context) {
	// Prime immediately so the first real tick has a baseline, then tick.
	r.Step(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Step(ctx)
		}
	}
}

// DryStep reports what would fire right now without sending anything and without
// consuming the edge. It is what the "проверить" button asks: waiting for the
// next tick to learn whether a rule works is a bad way to write one, and a person
// inspecting a threshold must not switch the heating on by inspecting it.
func (r *Runner) DryStep(ctx context.Context) []Decision {
	set, err := r.store.Get()
	if err != nil {
		r.log.Warn("rules: dry run cannot read rule set", "err", err)
		return nil
	}
	snap := r.snapshot()
	r.mu.Lock()
	state := r.state
	r.mu.Unlock()
	decisions, _, _ := Evaluate(set, ReadingsOf(snap.Devices), state, r.now(), r.loc)
	return decisions
}

// Step evaluates once and executes what fired. It is exported so a test and the
// "Проверить сейчас" button can drive one tick without waiting.
func (r *Runner) Step(ctx context.Context) {
	r.stepMu.Lock()
	defer r.stepMu.Unlock()
	set, err := r.store.Get()
	if err != nil {
		r.recordError(err)
		r.log.Warn("rules: cannot read rule set", "err", err)
		return
	}
	snap := r.snapshot()
	readings := ReadingsOf(snap.Devices)
	now := r.now()

	r.mu.Lock()
	state := r.state
	r.mu.Unlock()

	decisions, next, ruleErrs := Evaluate(set, readings, state, now, r.loc)

	r.mu.Lock()
	r.state = next
	r.lastRun = now
	r.lastErr = ""
	if len(ruleErrs) > 0 {
		r.lastErr = describeRuleErrors(ruleErrs)
	}
	r.mu.Unlock()

	if !state.Primed() {
		// The ticking tick that primes must not act, which Evaluate already
		// enforces; the log line is for the person reading the journal after a
		// restart and wondering why nothing happened.
		r.log.Info("rules: baseline recorded, no actions on the first tick",
			"rules", len(set.Rules))
		return
	}

	sent := 0
	for _, d := range decisions {
		entry, writes, starved := r.apply(ctx, d, snap, now, r.budget-sent)
		sent += writes
		r.addEntry(entry)
		if starved {
			// Nothing was sent because the budget was spent. The engine already
			// recorded the firing — it cannot know the cost — so the mark is
			// taken back: an edge rule must get its turn on the next tick rather
			// than lose the trigger for good.
			r.mu.Lock()
			r.state.ForgetFiring(d.RuleID)
			r.mu.Unlock()
		}
	}
}

// apply carries out one decision's actions in order. An error on one action is
// recorded and does not stop the rest: a rule that turns on a heater and opens a
// blind should still open the blind when the heater is outside the quota.
// apply carries out a decision's actions within what is left of the tick's
// budget. It reports how many writes actually went to the cloud — not how many
// actions were considered: a command that was skipped as "already so" nor one
// the cloud refused costs nothing, and counting them let a single no-op rule with
// many actions exhaust the budget for every rule after it.
func (r *Runner) apply(ctx context.Context, d Decision, snap home.Snapshot, now time.Time, budget int) (Entry, int, bool) {
	entry := Entry{Time: now, RuleID: d.RuleID, Name: d.RuleName, Group: d.Group}
	current := ReadingsOf(snap.Devices)
	writes := 0
	starved := false

	for _, a := range d.Actions {
		needsWrite := a.Kind == ActionScene
		if a.Kind == ActionCommand {
			// Idempotency: a lamp already on needs no command. Without this a
			// level-mode rule would re-send the same value every tick and fill
			// the cloud's request budget with writes that change nothing.
			existing, known := current[a.Device][a.Code]
			needsWrite = !(known && sameValue(existing, a.Value))
		}
		if needsWrite && writes >= budget {
			// Out of budget for this tick. The rule keeps its turn for the next
			// one; saying so is better than silently dropping the action.
			rec := journalFor(a)
			rec.Note = "пропущено: бюджет такта израсходован"
			entry.Actions = append(entry.Actions, rec)
			entry.Error = rec.Note
			starved = true
			continue
		}

		switch a.Kind {
		case ActionScene:
			rec := JournalAction{Kind: ActionScene, Scene: a.Scene}
			if err := r.send(ctx, a.Scene, tuya.Command{Code: sceneTriggerCode, Value: true}); err != nil {
				rec.Note = err.Error()
				entry.Error = err.Error()
			} else {
				writes++
			}
			entry.Actions = append(entry.Actions, rec)
		case ActionCommand:
			rec := JournalAction{Kind: ActionCommand, Device: a.Device, Code: a.Code, Value: a.Value}
			if !needsWrite {
				entry.Skipped = true
				rec.Note = "уже так"
				entry.Actions = append(entry.Actions, rec)
				continue
			}
			if err := r.send(ctx, a.Device, tuya.Command{Code: a.Code, Value: a.Value}); err != nil {
				rec.Note = err.Error()
				entry.Error = err.Error()
			} else {
				writes++
			}
			entry.Actions = append(entry.Actions, rec)
		}
	}
	return entry, writes, starved
}

// journalFor describes an action that was not attempted.
func journalFor(a Action) JournalAction {
	if a.Kind == ActionScene {
		return JournalAction{Kind: ActionScene, Scene: a.Scene}
	}
	return JournalAction{Kind: ActionCommand, Device: a.Device, Code: a.Code, Value: a.Value}
}

func (r *Runner) send(ctx context.Context, deviceID string, cmd tuya.Command) error {
	// A per-action timeout keeps one unreachable device from stalling the whole
	// tick and delaying the rest of the house.
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := r.exec.Command(c, deviceID, []tuya.Command{cmd}); err != nil {
		return err
	}
	return nil
}

// sameValue compares a reading with a wanted value the way a person would: 1 and
// 1.0 agree, true and "true" agree.
func sameValue(a, b any) bool {
	if ok, _ := compare(a, "==", b); ok {
		return true
	}
	return false
}

// Store is the rule store this runner reads. The HTTP layer edits the same
// object, so a change made in the page is seen by the next tick without a second
// store and a second lock on one file.
func (r *Runner) Store() *Store { return r.store }

// Journal returns a copy of the recent firings, newest last.
func (r *Runner) Journal() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Entry(nil), r.journal...)
}

// Status reports what the page shows next to the rule list.
func (r *Runner) Status() (primed bool, lastRun time.Time, lastErr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.Primed(), r.lastRun, r.lastErr
}

func (r *Runner) addEntry(e Entry) {
	r.mu.Lock()
	r.journal = append(r.journal, e)
	if len(r.journal) > JournalLength {
		r.journal = r.journal[len(r.journal)-JournalLength:]
	}
	r.mu.Unlock()

	level := slog.LevelInfo
	if e.Error != "" {
		level = slog.LevelWarn
	}
	r.log.Log(context.Background(), level, "rule fired",
		"rule", e.RuleID, "name", e.Name, "actions", e.Actions, "skipped", e.Skipped, "err", e.Error)
}

func (r *Runner) recordError(err error) {
	r.mu.Lock()
	r.lastErr = err.Error()
	r.mu.Unlock()
}
