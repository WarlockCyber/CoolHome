package home

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/demo"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(demo.New(), nil, time.Minute, logger)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	return svc
}

func statusValue(device tuya.Device, code string) any {
	for _, s := range device.Status {
		if s.Code == code {
			return s.Value
		}
	}
	return nil
}

func TestRefreshLoadsDevicesAndSpecs(t *testing.T) {
	svc := newTestService(t)
	snap := svc.Snapshot()
	if len(snap.Devices) != 6 {
		t.Fatalf("devices = %d, want 6", len(snap.Devices))
	}
	if snap.LastSync.IsZero() {
		t.Fatal("last sync is zero")
	}
	light, ok := svc.Device("demo-light-1")
	if !ok {
		t.Fatal("demo light missing")
	}
	if light.Spec == nil || len(light.Spec.Functions) != 3 {
		t.Fatalf("light spec = %+v", light.Spec)
	}
	if light.Status[0].Code != "switch_led" {
		t.Fatalf("unexpected status: %+v", light.Status)
	}
}

func TestCommandAppliesAndRefreshes(t *testing.T) {
	svc := newTestService(t)
	view, err := svc.Command(context.Background(), "demo-light-1", []tuya.Command{{Code: "switch_led", Value: false}})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if got := statusValue(view.Device, "switch_led"); got != false {
		t.Fatalf("switch_led = %v, want false", got)
	}
	// The cached snapshot must reflect the write too.
	cached, _ := svc.Device("demo-light-1")
	if got := statusValue(cached.Device, "switch_led"); got != false {
		t.Fatalf("cached switch_led = %v, want false", got)
	}

	if _, err := svc.Command(context.Background(), "demo-thermo-1", []tuya.Command{{Code: "temp_set", Value: 250}}); err != nil {
		t.Fatalf("thermostat command: %v", err)
	}
}

func TestCommandValidation(t *testing.T) {
	svc := newTestService(t)
	cases := []struct {
		name string
		cmd  tuya.Command
	}{
		{"unknown code", tuya.Command{Code: "nope", Value: true}},
		{"below minimum", tuya.Command{Code: "bright_value", Value: 5}},
		{"above maximum", tuya.Command{Code: "bright_value", Value: 2000}},
		{"fractional integer", tuya.Command{Code: "bright_value", Value: 50.5}},
		{"wrong bool type", tuya.Command{Code: "switch_led", Value: "yes"}},
		{"invalid enum", tuya.Command{Code: "work_mode", Value: "rainbow"}},
		{"enum as bool", tuya.Command{Code: "work_mode", Value: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Command(context.Background(), "demo-light-1", []tuya.Command{tc.cmd})
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("err = %v (%T), want *ValidationError", err, err)
			}
		})
	}

	if _, err := svc.Command(context.Background(), "demo-light-1", []tuya.Command{{Code: "work_mode", Value: "colour"}}); err != nil {
		t.Fatalf("valid enum rejected: %v", err)
	}
}

func TestCommandErrors(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.Command(context.Background(), "unknown", []tuya.Command{{Code: "switch_1", Value: true}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown device error = %v, want ErrNotFound", err)
	}

	if _, err := svc.Command(context.Background(), "demo-light-1", nil); err == nil {
		t.Fatal("empty command list accepted")
	}

	_, err := svc.Command(context.Background(), "demo-humidifier-1", []tuya.Command{{Code: "switch_1", Value: true}})
	if err == nil {
		t.Fatal("offline device accepted a command")
	}
	var verr *ValidationError
	if errors.As(err, &verr) {
		t.Fatalf("offline error misclassified as validation: %v", err)
	}
}

func TestCommandLimit(t *testing.T) {
	svc := newTestService(t)
	cmds := make([]tuya.Command, 21)
	for i := range cmds {
		cmds[i] = tuya.Command{Code: "switch_led", Value: true}
	}
	if _, err := svc.Command(context.Background(), "demo-light-1", cmds); err == nil {
		t.Fatal("oversized command batch accepted")
	}
}

func TestSpecCacheSurvivesRefresh(t *testing.T) {
	svc := newTestService(t)
	first, _ := svc.Device("demo-light-1")
	if first.Spec == nil {
		t.Fatal("missing spec")
	}
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	snap := svc.Snapshot()
	if len(snap.Devices) != 6 {
		t.Fatalf("devices after refresh = %d", len(snap.Devices))
	}
}

// specStub returns fixed spec answers so the cache policy can be tested without
// the cloud.
type specStub struct {
	*demo.Provider
	calls int
	err   error
	spec  tuya.Spec
}

func (s *specStub) Spec(ctx context.Context, id string) (tuya.Spec, error) {
	s.calls++
	if s.err != nil {
		return tuya.Spec{}, s.err
	}
	return s.spec, nil
}

func newUnsupportedService(t *testing.T, now *time.Time) (*Service, *specStub) {
	t.Helper()
	stub := &specStub{Provider: demo.New(), err: &tuya.APIError{Code: 2009, Msg: "not support this device"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(stub, nil, time.Minute, logger)
	svc.now = func() time.Time { return *now }
	return svc, stub
}

func TestUnsupportedDeviceIsFlaggedAndNotRetried(t *testing.T) {
	now := time.Unix(1700000000, 0)
	svc, stub := newUnsupportedService(t, &now)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	view, ok := svc.Device("demo-light-1")
	if !ok {
		t.Fatal("device missing")
	}
	if !view.SpecUnsupported {
		t.Fatal("SpecUnsupported must be set for a 2009 answer")
	}
	if view.Spec != nil {
		t.Fatalf("spec = %+v, want nil", view.Spec)
	}
	if view.SpecError == "" {
		t.Fatal("the raw error should stay available for diagnostics")
	}

	first := stub.calls
	// Past the ordinary error retry window, but inside the unsupported one.
	now = now.Add(10 * time.Minute)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if stub.calls != first {
		t.Errorf("spec calls = %d after 10m, want %d: an unsupported device must not be re-probed every few minutes", stub.calls, first)
	}

	// A day later it is worth asking again: the product may have been changed.
	now = now.Add(specUnsupportedTTL)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if stub.calls == first {
		t.Error("spec should be re-probed after the unsupported TTL")
	}
}

func TestTransientSpecErrorStillRetriesQuickly(t *testing.T) {
	now := time.Unix(1700000000, 0)
	stub := &specStub{Provider: demo.New(), err: &tuya.APIError{Code: 500, Msg: "internal error", Status: 500}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(stub, nil, time.Minute, logger)
	svc.now = func() time.Time { return now }
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	view, _ := svc.Device("demo-light-1")
	if view.SpecUnsupported {
		t.Fatal("a 500 must not be flagged as unsupported")
	}

	first := stub.calls
	now = now.Add(specErrRetry + time.Second)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if stub.calls == first {
		t.Error("a transient error must be retried after the short window")
	}
}

func TestValidateCommandButton(t *testing.T) {
	spec := &tuya.Spec{
		Category: "ir_remote",
		Functions: []tuya.Function{
			{Code: "power", Type: tuya.FunctionTypeButton, Name: "Питание"},
		},
	}

	if err := ValidateCommand(tuya.Command{Code: "power", Value: true}, spec); err != nil {
		t.Fatalf("true must press the button: %v", err)
	}
	for _, bad := range []any{false, "press", 1, nil} {
		err := ValidateCommand(tuya.Command{Code: "power", Value: bad}, spec)
		if err == nil {
			t.Errorf("value %#v must be refused: a button only accepts a press", bad)
		}
	}
	if err := ValidateCommand(tuya.Command{Code: "missing", Value: true}, spec); err == nil {
		t.Error("an unknown key must be refused")
	}
}

// A learned RF key is visible but not writable: the cloud accepts the payload
// and never transmits it, so the API must refuse rather than pretend.
func TestPassiveKeyIsRefused(t *testing.T) {
	spec := &tuya.Spec{
		Category: "ir_remote",
		Functions: []tuya.Function{
			{Code: "one_on", Type: tuya.FunctionTypePassive, Name: "one ON"},
		},
	}
	err := ValidateCommand(tuya.Command{Code: "one_on", Value: true}, spec)
	if err == nil {
		t.Fatal("a learned RF key must not be writable")
	}
	if !strings.Contains(err.Error(), "RF") {
		t.Errorf("error = %v, want it to name the reason", err)
	}
}

// A spec with a passive key must produce a device-level note, so no client has
// to infer the limitation from a type name.
type passiveStub struct {
	*demo.Provider
}

func (passiveStub) Spec(context.Context, string) (tuya.Spec, error) {
	return tuya.Spec{
		Category: "ir_remote",
		Functions: []tuya.Function{
			{Code: "one_on", Type: tuya.FunctionTypePassive, Name: "one ON"},
		},
	}, nil
}

func TestPassiveSpecSetsADeviceNote(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(passiveStub{demo.New()}, nil, time.Minute, logger)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	view, ok := svc.Device("demo-light-1")
	if !ok {
		t.Fatal("device missing")
	}
	if view.Note == "" {
		t.Fatal("a device with a passive function must carry a note")
	}
	if !strings.Contains(view.Note, "не передаёт") {
		t.Errorf("note = %q, want it to explain that the cloud does not transmit", view.Note)
	}
}
