package home

import (
	"context"
	"strings"
	"testing"

	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

func TestFilterDropsRejectedDevices(t *testing.T) {
	inner := &mockProvider{devices: []tuya.Device{
		{ID: "rf-1", Category: "rf_switch1"},
		{ID: "sensor-1", Category: "tdq"},
		{ID: "lamp-1", Category: "dj"},
	}}
	f := NewFilter(inner, func(d tuya.Device) bool {
		return !strings.HasPrefix(d.Category, "rf_switch")
	})

	devices, err := f.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2: %+v", len(devices), devices)
	}
	for _, d := range devices {
		if d.Category == "rf_switch1" {
			t.Error("a rejected device was not dropped")
		}
	}
}

// Filtering must not break the rest of the provider contract.
func TestFilterForwardsOtherCalls(t *testing.T) {
	inner := &mockProvider{
		devices: []tuya.Device{{ID: "d1"}},
		spec:    tuya.Spec{Category: "dj"},
	}
	f := NewFilter(inner, func(tuya.Device) bool { return true })

	if _, err := f.Device(context.Background(), "d1"); err != nil {
		t.Fatalf("Device: %v", err)
	}
	if _, err := f.Spec(context.Background(), "d1"); err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if err := f.Command(context.Background(), "d1", []tuya.Command{{Code: "x", Value: true}}); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if len(inner.cmds) != 1 {
		t.Errorf("command was not forwarded: %+v", inner.cmds)
	}
}

func TestFilterPropagatesErrors(t *testing.T) {
	inner := &mockProvider{listErr: context.Canceled}
	f := NewFilter(inner, func(tuya.Device) bool { return true })

	if _, err := f.ListDevices(context.Background(), nil); err == nil {
		t.Fatal("expected the inner error")
	}
}
