package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/rules"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// rulesServer wires a runner over the demo home so the API can be exercised end
// to end: saving a rule, switching it, and asking what would fire.
func rulesServer(t *testing.T, path string) (*httptest.Server, *rules.Runner) {
	t.Helper()
	store := rules.New(path)
	homeStub := &staticHome{}
	runner, err := rules.NewRunner(rules.Options{
		Store:    store,
		Executor: homeStub,
		Snapshot: homeStub.snapshot,
		Interval: time.Minute,
		Location: time.UTC,
		Now:      func() time.Time { return time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, Options{Demo: true, Rules: runner, RulesLocation: time.UTC})
	return srv, runner
}

// staticHome is an executor and snapshot in one: no devices, no cloud.
type staticHome struct{}

func (s *staticHome) Command(context.Context, string, []tuya.Command) (home.DeviceView, error) {
	return home.DeviceView{}, nil
}

func (s *staticHome) snapshot() home.Snapshot { return home.Snapshot{} }

const oneRule = `{"rules":[{"id":"r1","name":"Холодно","enabled":true,` +
	`"when":[{"device":"d1","code":"temp_current","op":"<","value":180}],` +
	`"actions":[{"kind":"command","device":"p1","code":"switch_1","value":true}]}]}`

func TestRulesOffIsExplainedNot404(t *testing.T) {
	srv := newTestServer(t, Options{Demo: true})
	resp, body := getJSON(t, srv.URL+"/api/rules")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 so the page can explain", resp.StatusCode)
	}
	var out rulesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Running {
		t.Error("running = true with no runner")
	}
	if len(out.Rules) != 0 || out.Rules == nil {
		t.Errorf("rules = %v, want an empty list", out.Rules)
	}
}

func TestRulesSaveAndServe(t *testing.T) {
	srv, _ := rulesServer(t, filepath.Join(t.TempDir(), "rules.json"))

	resp, body := putJSON(t, srv.URL+"/api/rules", oneRule)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out rulesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rules) != 1 || out.Rules[0].ID != "r1" {
		t.Fatalf("rules = %+v", out.Rules)
	}
	if out.Rules[0].Mode != rules.DefaultMode {
		t.Errorf("mode = %q, want the default filled in", out.Rules[0].Mode)
	}
	if !out.Running || !out.Editable {
		t.Errorf("running=%v editable=%v, want both true", out.Running, out.Editable)
	}

	// Served back, which is what a reload does.
	_, body = getJSON(t, srv.URL+"/api/rules")
	if !strings.Contains(string(body), `"r1"`) {
		t.Errorf("saved rule not served: %s", body)
	}
}

func TestRulesRejectABadSet(t *testing.T) {
	srv, _ := rulesServer(t, filepath.Join(t.TempDir(), "rules.json"))
	cases := map[string]string{
		"no actions": `{"rules":[{"id":"r","when":[{"device":"d","code":"c","op":"<","value":1}],"actions":[]}]}`,
		"bad op":     `{"rules":[{"id":"r","when":[{"device":"d","code":"c","op":"~","value":1}],"actions":[{"kind":"command","device":"d","code":"c","value":1}]}]}`,
		"bad json":   `{`,
		"no trigger": `{"rules":[{"id":"r","actions":[{"kind":"command","device":"d","code":"c","value":1}]}]}`,
	}
	for name, body := range cases {
		resp, raw := putJSON(t, srv.URL+"/api/rules", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body = %s", name, resp.StatusCode, raw)
		}
	}
}

// Switching one rule is the hurried action: the automation is in the way and must
// stop now.
func TestRuleSwitch(t *testing.T) {
	srv, runner := rulesServer(t, filepath.Join(t.TempDir(), "rules.json"))
	if resp, body := putJSON(t, srv.URL+"/api/rules", oneRule); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}

	resp, body := postJSON(t, srv.URL+"/api/rules/r1/enabled", `{"enabled":false}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out rulesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Rules[0].Enabled {
		t.Error("the rule is still enabled")
	}
	// The store the runner reads is the same one the API wrote.
	set, err := runner.Store().Get()
	if err != nil {
		t.Fatal(err)
	}
	if set.Rules[0].Enabled {
		t.Error("the runner's store still has the rule enabled")
	}
}

func TestRuleSwitchNeedsTheField(t *testing.T) {
	srv, _ := rulesServer(t, filepath.Join(t.TempDir(), "rules.json"))
	if resp, _ := putJSON(t, srv.URL+"/api/rules", oneRule); resp.StatusCode != http.StatusOK {
		t.Fatal("setup failed")
	}
	resp, raw := postJSON(t, srv.URL+"/api/rules/r1/enabled", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "enabled") {
		t.Errorf("body = %s, want the missing field named", raw)
	}
}

func TestRuleSwitchUnknownIdIs404(t *testing.T) {
	srv, _ := rulesServer(t, filepath.Join(t.TempDir(), "rules.json"))
	resp, _ := postJSON(t, srv.URL+"/api/rules/nope/enabled", `{"enabled":false}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// A group switch stops many rules at once without touching their own switches,
// so turning it back on restores exactly the set that was running.
func TestGroupSwitch(t *testing.T) {
	srv, _ := rulesServer(t, filepath.Join(t.TempDir(), "rules.json"))
	set := `{"groups":[{"name":"Отопление","enabled":true}],"rules":[` +
		`{"id":"a","name":"a","group":"Отопление","enabled":true,"when":[{"device":"d","code":"c","op":"<","value":1}],"actions":[{"kind":"command","device":"d","code":"c","value":1}]},` +
		`{"id":"b","name":"b","group":"Отопление","enabled":true,"when":[{"device":"d","code":"c","op":"<","value":1}],"actions":[{"kind":"command","device":"d","code":"c","value":1}]}]}`
	if resp, body := putJSON(t, srv.URL+"/api/rules", set); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}

	resp, body := postJSON(t, srv.URL+"/api/rules/groups/"+url.PathEscape("Отопление")+"/enabled", `{"enabled":false}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out rulesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Groups[0].Enabled {
		t.Error("the group is still enabled")
	}
	// The individual switches are untouched: this is the whole point of a group.
	for _, r := range out.Rules {
		if !r.Enabled {
			t.Errorf("rule %s was switched off by the group switch", r.ID)
		}
	}
}

// The check answers "what would fire" and sends nothing: inspecting a threshold
// must not switch the heating on.
func TestRulesCheckSendsNothing(t *testing.T) {
	exec := &countingExec{}
	store := rules.New("")
	if _, err := store.Save(mustSet(t, oneRule)); err != nil {
		t.Fatal(err)
	}
	hm := &mutableHome{}
	runner, err := rules.NewRunner(rules.Options{
		Store: store, Executor: exec, Snapshot: hm.snapshot,
		Interval: time.Minute, Location: time.UTC,
		Now: func() time.Time { return time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	// Prime with the condition false: a rule already true at startup waits for
	// the next real rise instead of acting.
	hm.set("d1", "temp_current", float64(250))
	runner.Step(context.Background())
	hm.set("d1", "temp_current", float64(150))

	srv := newTestServer(t, Options{Demo: true, Rules: runner, RulesLocation: time.UTC})
	resp, body := postJSON(t, srv.URL+"/api/rules/check", `{}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out rulesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if exec.count != 0 {
		t.Errorf("the check sent %d commands; it must send none", exec.count)
	}
	// It does explain what would happen.
	if len(out.Journal) != 1 || out.Journal[0].RuleID != "r1" {
		t.Errorf("journal = %+v, want the rule that would fire", out.Journal)
	}
}

func mustSet(t *testing.T, raw string) rules.Set {
	t.Helper()
	var req struct {
		Groups []rules.Group `json:"groups"`
		Rules  []rules.Rule  `json:"rules"`
	}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	set, err := rules.Normalize(rules.Set{Groups: req.Groups, Rules: req.Rules})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

type countingExec struct{ count int }

func (c *countingExec) Command(context.Context, string, []tuya.Command) (home.DeviceView, error) {
	c.count++
	return home.DeviceView{}, nil
}

type mutableHome struct {
	devices []home.DeviceView
}

func (m *mutableHome) set(id, code string, value any) {
	for i := range m.devices {
		if m.devices[i].ID == id {
			m.devices[i].Status = append(m.devices[i].Status, tuya.DeviceStatus{Code: code, Value: value})
			return
		}
	}
	v := home.DeviceView{Device: tuya.Device{ID: id, Online: true}}
	v.Status = []tuya.DeviceStatus{{Code: code, Value: value}}
	m.devices = append(m.devices, v)
}

func (m *mutableHome) snapshot() home.Snapshot { return home.Snapshot{Devices: m.devices} }
