package home

import (
	"context"

	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// FilterProvider hides devices a predicate rejects. It exists so the choice of
// what to show lives where the providers are composed, instead of being baked
// into a client or a provider that should report what the cloud actually has.
type FilterProvider struct {
	inner Provider
	keep  func(tuya.Device) bool
}

// NewFilter wraps a provider, dropping devices for which keep returns false.
func NewFilter(inner Provider, keep func(tuya.Device) bool) *FilterProvider {
	return &FilterProvider{inner: inner, keep: keep}
}

// ListDevices forwards the listing and drops rejected devices.
func (f *FilterProvider) ListDevices(ctx context.Context, ids []string) ([]tuya.Device, error) {
	devices, err := f.inner.ListDevices(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := devices[:0]
	for _, d := range devices {
		if f.keep(d) {
			out = append(out, d)
		}
	}
	return out, nil
}

// Device forwards one device.
func (f *FilterProvider) Device(ctx context.Context, id string) (tuya.Device, error) {
	return f.inner.Device(ctx, id)
}

// Spec forwards the thing model.
func (f *FilterProvider) Spec(ctx context.Context, id string) (tuya.Spec, error) {
	return f.inner.Spec(ctx, id)
}

// Command forwards a write.
func (f *FilterProvider) Command(ctx context.Context, id string, cmds []tuya.Command) error {
	return f.inner.Command(ctx, id, cmds)
}
