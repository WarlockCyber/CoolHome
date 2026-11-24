package home

import (
	"context"
	"errors"
	"testing"

	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// mockProvider is a provider whose answers a test sets directly.
type mockProvider struct {
	devices []tuya.Device
	spec    tuya.Spec
	cmds    []tuya.Command
	listErr error
	devErr  error
	specErr error
	cmdErr  error
	routed  string
}

func (m *mockProvider) ListDevices(context.Context, []string) ([]tuya.Device, error) {
	return m.devices, m.listErr
}

func (m *mockProvider) Device(_ context.Context, id string) (tuya.Device, error) {
	m.routed = id
	if m.devErr != nil {
		return tuya.Device{}, m.devErr
	}
	for _, d := range m.devices {
		if d.ID == id {
			return d, nil
		}
	}
	return tuya.Device{}, ErrNotFound
}

func (m *mockProvider) Spec(_ context.Context, id string) (tuya.Spec, error) {
	m.routed = id
	return m.spec, m.specErr
}

func (m *mockProvider) Command(_ context.Context, id string, cmds []tuya.Command) error {
	m.routed = id
	m.cmds = cmds
	return m.cmdErr
}

func TestMultiListsPrimaryThenOptional(t *testing.T) {
	primary := &mockProvider{devices: []tuya.Device{{ID: "lamp"}}}
	extra := &mockProvider{devices: []tuya.Device{{ID: "ir:blast:1"}}}
	m := NewMulti(primary, extra)

	out, err := m.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(out) != 2 || out[0].ID != "lamp" || out[1].ID != "ir:blast:1" {
		t.Fatalf("devices = %+v", out)
	}
}

// The primary device list is the home; losing it must surface as an error, not
// as an empty house.
func TestMultiFailsWhenThePrimaryFails(t *testing.T) {
	primary := &mockProvider{listErr: errors.New("cloud down")}
	m := NewMulti(primary, &mockProvider{})

	if _, err := m.ListDevices(context.Background(), nil); err == nil {
		t.Fatal("expected the primary error")
	}
}

// An optional surface that is down must not take the dashboard with it.
func TestMultiToleratesAFailingOptionalProvider(t *testing.T) {
	primary := &mockProvider{devices: []tuya.Device{{ID: "lamp"}}}
	extra := &mockProvider{listErr: errors.New("not subscribed")}
	m := NewMulti(primary, extra)

	out, err := m.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(out) != 1 || out[0].ID != "lamp" {
		t.Fatalf("devices = %+v", out)
	}
}

// Writes must reach the owner, and must not be spent on the other provider.
func TestMultiRoutesByOwnership(t *testing.T) {
	primary := &mockProvider{devices: []tuya.Device{{ID: "lamp"}}}
	extra := &mockProvider{devices: []tuya.Device{{ID: "ir:blast:1"}}}
	m := NewMulti(primary, extra)
	if _, err := m.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("ListDevices: %v", err)
	}

	primary.routed, extra.routed = "", ""
	if err := m.Command(context.Background(), "ir:blast:1", []tuya.Command{{Code: "power", Value: true}}); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if extra.routed != "ir:blast:1" {
		t.Errorf("the optional provider was not called (routed=%q)", extra.routed)
	}
	if primary.routed != "" {
		t.Errorf("the primary provider was asked about an IR remote (%q)", primary.routed)
	}

	primary.routed, extra.routed = "", ""
	if err := m.Command(context.Background(), "lamp", []tuya.Command{{Code: "switch_1", Value: true}}); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if primary.routed != "lamp" {
		t.Errorf("the primary provider was not called (routed=%q)", primary.routed)
	}
	if extra.routed != "" {
		t.Errorf("an IR provider was asked about a physical device (%q)", extra.routed)
	}
}

// Ownership comes from the last listing, so routing must follow a refresh that
// drops a device back to the primary.
func TestMultiRoutingFollowsTheLatestListing(t *testing.T) {
	primary := &mockProvider{devices: []tuya.Device{{ID: "lamp"}}}
	extra := &mockProvider{devices: []tuya.Device{{ID: "ir:blast:1"}}}
	m := NewMulti(primary, extra)

	if _, err := m.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	// The blaster disappears: the id is no longer served by the optional
	// provider, so a write for it belongs to the primary again.
	extra.devices = nil
	if _, err := m.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("ListDevices: %v", err)
	}

	primary.routed, extra.routed = "", ""
	primary.cmdErr = ErrNotFound
	_ = m.Command(context.Background(), "ir:blast:1", []tuya.Command{{Code: "power", Value: true}})
	if primary.routed != "ir:blast:1" {
		t.Errorf("the write should have gone to the primary (routed=%q)", primary.routed)
	}
	if extra.routed != "" {
		t.Errorf("the write should not have gone to the optional provider (%q)", extra.routed)
	}
}

func TestMultiMapsMissingDeviceToNotFound(t *testing.T) {
	primary := &mockProvider{devices: nil, cmdErr: ErrNotFound}
	m := NewMulti(primary)

	err := m.Command(context.Background(), "ghost", []tuya.Command{{Code: "x", Value: true}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestMultiSpecRoutesToTheOwner(t *testing.T) {
	primary := &mockProvider{spec: tuya.Spec{Category: "physical"}}
	extra := &mockProvider{spec: tuya.Spec{Category: "ir_remote"}}
	m := NewMulti(primary, extra)
	if _, err := m.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if _, err := m.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("ListDevices: %v", err)
	}

	// With no optional ownership recorded, the primary answers.
	spec, err := m.Spec(context.Background(), "lamp")
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if spec.Category != "physical" {
		t.Errorf("category = %q", spec.Category)
	}
}

// An infrared remote carries the id of the device it belongs to. Serving both
// would show the same physical device twice — once bare, once with buttons.
func TestMultiReplacesADeviceWithTheRicherEntry(t *testing.T) {
	primary := &mockProvider{devices: []tuya.Device{
		{ID: "conder", Name: "Кондер", Category: "infrared_ac"},
		{ID: "lamp", Name: "Лампа"},
	}}
	extra := &mockProvider{devices: []tuya.Device{
		{ID: "conder", Name: "Кондер", Category: "ir_remote"},
	}}
	m := NewMulti(primary, extra)

	out, err := m.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("devices = %d, want 2 (no duplicate): %+v", len(out), out)
	}
	for _, d := range out {
		if d.ID == "conder" && d.Category != "ir_remote" {
			t.Errorf("conder = %+v, want the remote entry with functions", d)
		}
	}

	// Ownership follows the replacement.
	primary.routed, extra.routed = "", ""
	if err := m.Command(context.Background(), "conder", []tuya.Command{{Code: "PowerOn", Value: true}}); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if extra.routed != "conder" {
		t.Errorf("the remote provider was not used (routed=%q)", extra.routed)
	}
}
