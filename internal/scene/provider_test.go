package scene

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

type stub struct {
	scenes   []tuya.Scene
	space    string
	spaceErr error
	listErr  error
	trigErr  error

	listCalls    int
	triggerCalls int
	triggered    []string
}

func (s *stub) Scenes(context.Context, string) ([]tuya.Scene, error) {
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.scenes, nil
}

func (s *stub) TriggerScene(_ context.Context, id string) error {
	s.triggerCalls++
	if s.trigErr != nil {
		return s.trigErr
	}
	s.triggered = append(s.triggered, id)
	return nil
}

func (s *stub) SpaceID(context.Context) (string, error) {
	return s.space, s.spaceErr
}

func newStub() *stub {
	return &stub{
		space: "space-1",
		scenes: []tuya.Scene{
			{ID: "s-1", Name: "главный свет", Status: "enable", Type: "scene"},
			{ID: "s-2", Name: "коридор", Status: "disable", Type: "scene"},
		},
	}
}

func newProvider(t *testing.T, s *stub, now *time.Time) *Provider {
	t.Helper()
	p := New(s, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if now != nil {
		p.now = func() time.Time { return *now }
	}
	return p
}

func TestScenesBecomeDevices(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	devices, err := p.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(devices))
	}
	if devices[0].ID != "s-1" || devices[0].Name != "главный свет" || devices[0].Category != Category {
		t.Fatalf("first device = %+v", devices[0])
	}
	if !devices[0].Online {
		t.Error("an enabled scene must be online")
	}
	// A disabled scene cannot be triggered, and must say why.
	if devices[1].Online {
		t.Error("a disabled scene must not be online")
	}
	if devices[1].Note == "" {
		t.Error("a disabled scene must carry a note")
	}
}

func TestSceneSpecHasOneButton(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	spec, err := p.Spec(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if len(spec.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(spec.Functions))
	}
	fn := spec.Functions[0]
	if fn.Code != ButtonCode || fn.Type != tuya.FunctionTypeButton {
		t.Fatalf("function = %+v", fn)
	}
	if fn.Name != "главный свет" {
		t.Errorf("name = %q, want the scene name", fn.Name)
	}
}

func TestCommandTriggersTheScene(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	if err := p.Command(context.Background(), "s-1", []tuya.Command{{Code: ButtonCode, Value: true}}); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if len(s.triggered) != 1 || s.triggered[0] != "s-1" {
		t.Fatalf("triggered = %v", s.triggered)
	}
}

func TestCommandRejectsAnUnknownFunction(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	err := p.Command(context.Background(), "s-1", []tuya.Command{{Code: "switch_1", Value: true}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if s.triggerCalls != 0 {
		t.Error("nothing may be triggered for an unknown function")
	}
}

func TestCommandRejectsAnUnknownScene(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	err := p.Command(context.Background(), "nope", []tuya.Command{{Code: ButtonCode, Value: true}})
	if !errors.Is(err, home.ErrNotFound) {
		t.Fatalf("error = %v, want home.ErrNotFound", err)
	}
	if s.triggerCalls != 0 {
		t.Error("nothing may be triggered for an unknown scene")
	}
}

func TestSceneListIsCached(t *testing.T) {
	now := time.Unix(1700000000, 0)
	s := newStub()
	p := newProvider(t, s, &now)

	for i := 0; i < 3; i++ {
		if _, err := p.ListDevices(context.Background(), nil); err != nil {
			t.Fatalf("ListDevices: %v", err)
		}
	}
	if s.listCalls != 1 {
		t.Errorf("list calls = %d, want 1", s.listCalls)
	}

	now = now.Add(cacheTTL + time.Second)
	if _, err := p.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if s.listCalls == 1 {
		t.Error("the list should be refetched after the TTL")
	}
}

func TestSpaceIsResolvedOnce(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	if _, err := p.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if p.spaceID != "space-1" {
		t.Errorf("resolved space = %q", p.spaceID)
	}
}

func TestMissingSpaceIsAnError(t *testing.T) {
	s := newStub()
	s.space = ""
	p := newProvider(t, s, nil)

	if _, err := p.ListDevices(context.Background(), nil); err == nil {
		t.Fatal("expected an error when no space can be resolved")
	}
}

func TestPinnedSpaceSkipsDiscovery(t *testing.T) {
	s := newStub()
	s.space = ""
	p := New(s, "pinned", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := p.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if p.spaceID != "pinned" {
		t.Errorf("space = %q", p.spaceID)
	}
}

// An unsubscribed service must not fail the whole home.
func TestUnsubscribedServiceDisablesTheProvider(t *testing.T) {
	s := newStub()
	s.listErr = &tuya.APIError{Code: 28841101, Msg: "No permissions. This API is not subscribed."}
	p := newProvider(t, s, nil)

	devices, err := p.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices must not fail: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("devices = %+v, want none", devices)
	}
	calls := s.listCalls
	if _, err := p.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("second ListDevices: %v", err)
	}
	if s.listCalls != calls {
		t.Errorf("list calls = %d, want %d: a disabled provider must stop asking", s.listCalls, calls)
	}
}
