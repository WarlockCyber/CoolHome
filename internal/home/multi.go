package home

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// Multi combines several providers into one home. The first provider is
// authoritative: if it fails, the error is returned, because a silently empty
// device list would look like an empty home. Later providers may fail softly —
// an optional surface such as infrared must not take the whole dashboard down.
//
// Routing is decided by ownership recorded during the last ListDevices, not by
// guessing from the id: asking the wrong provider about a device would spend a
// cloud request only to learn it does not own it.
type Multi struct {
	primary  Provider
	optional []Provider

	mu    sync.RWMutex
	owner map[string]int // device id -> index into optional
}

// NewMulti builds a composite provider.
func NewMulti(primary Provider, optional ...Provider) *Multi {
	return &Multi{primary: primary, optional: optional, owner: map[string]int{}}
}

// ListDevices returns the primary home followed by whatever the optional
// providers could contribute.
func (m *Multi) ListDevices(ctx context.Context, ids []string) ([]tuya.Device, error) {
	out, err := m.primary.ListDevices(ctx, ids)
	if err != nil {
		return nil, err
	}
	owner := make(map[string]int)
	index := make(map[string]int, len(out))
	for i, d := range out {
		index[d.ID] = i
	}
	for i, p := range m.optional {
		extra, err := p.ListDevices(ctx, ids)
		if err != nil {
			// Optional surfaces are additive; their absence is not a failure.
			continue
		}
		for _, d := range extra {
			owner[d.ID] = i
			// The same physical device can appear in both surfaces — an
			// infrared remote carries the id of the device it belongs to. The
			// richer entry wins: a remote with buttons replaces the bare device
			// card, so the home does not show it twice.
			if pos, seen := index[d.ID]; seen {
				out[pos] = d
				continue
			}
			index[d.ID] = len(out)
			out = append(out, d)
		}
	}
	m.mu.Lock()
	m.owner = owner
	m.mu.Unlock()
	return out, nil
}

// optionalFor returns the optional provider that owns id, if any.
func (m *Multi) optionalFor(id string) (Provider, bool) {
	m.mu.RLock()
	idx, ok := m.owner[id]
	m.mu.RUnlock()
	if !ok || idx >= len(m.optional) {
		return nil, false
	}
	return m.optional[idx], true
}

// Device returns one device from whichever provider owns it.
func (m *Multi) Device(ctx context.Context, id string) (tuya.Device, error) {
	if p, ok := m.optionalFor(id); ok {
		return p.Device(ctx, id)
	}
	return m.primary.Device(ctx, id)
}

// Spec returns the thing model from the provider that owns the device.
func (m *Multi) Spec(ctx context.Context, id string) (tuya.Spec, error) {
	if p, ok := m.optionalFor(id); ok {
		return p.Spec(ctx, id)
	}
	return m.primary.Spec(ctx, id)
}

// Command routes a write to the provider that owns the device.
func (m *Multi) Command(ctx context.Context, id string, cmds []tuya.Command) error {
	if p, ok := m.optionalFor(id); ok {
		return p.Command(ctx, id, cmds)
	}
	err := m.primary.Command(ctx, id, cmds)
	if err != nil && errors.Is(err, ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return err
}
