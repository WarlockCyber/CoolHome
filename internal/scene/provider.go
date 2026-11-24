// Package scene exposes Tuya tap-to-run scenes as CoolHome devices.
//
// A scene is the supported way to perform an action the data-point API cannot:
// a learned RF key, for example, is accepted by the learning-code endpoint and
// never transmitted, while a scene that presses the same key works, because the
// cloud executes it. Each scene is presented as a device with one button.
package scene

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// ButtonCode is the single function every scene device exposes.
const ButtonCode = "trigger"

// Category is what the UI sees for a scene device.
const Category = "scene"

// Client is the slice of the Tuya client this provider needs.
type Client interface {
	Scenes(ctx context.Context, spaceID string) ([]tuya.Scene, error)
	TriggerScene(ctx context.Context, ruleID string) error
	SpaceID(ctx context.Context) (string, error)
}

// cacheTTL bounds how stale the scene list may be. Scenes change when someone
// edits them in the app, which is rare.
const cacheTTL = 5 * time.Minute

// Provider serves scenes through the home.Provider interface.
type Provider struct {
	client  Client
	spaceID string
	log     *slog.Logger
	now     func() time.Time

	mu        sync.Mutex
	cached    []tuya.Scene
	fetchedAt time.Time
	disabled  bool
}

// New builds a provider. spaceID optionally pins the space; when empty it is
// discovered from the account's devices.
func New(client Client, spaceID string, logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{client: client, spaceID: spaceID, log: logger, now: time.Now}
}

// space resolves the space id once and remembers it.
func (p *Provider) space(ctx context.Context) (string, error) {
	if p.spaceID != "" {
		return p.spaceID, nil
	}
	id, err := p.client.SpaceID(ctx)
	if err != nil {
		return "", fmt.Errorf("scene: resolve space: %w", err)
	}
	if id == "" {
		return "", fmt.Errorf("scene: no space id found; set COOLHOME_SPACE_ID")
	}
	p.spaceID = id
	return id, nil
}

// scenes returns the cached scene list, refreshing it when stale.
func (p *Provider) scenes(ctx context.Context) ([]tuya.Scene, error) {
	p.mu.Lock()
	if p.disabled {
		p.mu.Unlock()
		return nil, nil
	}
	if p.cached != nil && p.now().Sub(p.fetchedAt) < cacheTTL {
		out := p.cached
		p.mu.Unlock()
		return out, nil
	}
	p.mu.Unlock()

	space, err := p.space(ctx)
	if err != nil {
		return nil, err
	}
	list, err := p.client.Scenes(ctx, space)
	if err != nil {
		if tuya.IsServiceNotSubscribed(err) {
			p.mu.Lock()
			first := !p.disabled
			p.disabled = true
			p.mu.Unlock()
			if first {
				p.log.Warn("scene linkage is unavailable: subscribe the service in the Tuya console "+
					"(Cloud → Development → your project → Service API)", "err", err)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("scene: list scenes: %w", err)
	}

	p.mu.Lock()
	p.cached = list
	p.fetchedAt = p.now()
	p.mu.Unlock()
	return list, nil
}

// ListDevices returns every enabled scene as a device.
func (p *Provider) ListDevices(ctx context.Context, ids []string) ([]tuya.Device, error) {
	list, err := p.scenes(ctx)
	if err != nil {
		return nil, err
	}
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	out := make([]tuya.Device, 0, len(list))
	for _, s := range list {
		if len(want) > 0 {
			if _, ok := want[s.ID]; !ok {
				continue
			}
		}
		out = append(out, toDevice(s))
	}
	return out, nil
}

// toDevice maps a scene onto the device shape the rest of CoolHome speaks.
func toDevice(s tuya.Scene) tuya.Device {
	note := ""
	if !s.Enabled() {
		note = "Сцена выключена в приложении Tuya — нажатие не сработает, пока её не включат."
	}
	return tuya.Device{
		ID:       s.ID,
		Name:     s.Name,
		Category: Category,
		Online:   s.Enabled(),
		Note:     note,
	}
}

// Device returns one scene.
func (p *Provider) Device(ctx context.Context, id string) (tuya.Device, error) {
	list, err := p.scenes(ctx)
	if err != nil {
		return tuya.Device{}, err
	}
	for _, s := range list {
		if s.ID == id {
			return toDevice(s), nil
		}
	}
	return tuya.Device{}, home.ErrNotFound
}

// Spec gives the scene its single button.
func (p *Provider) Spec(ctx context.Context, id string) (tuya.Spec, error) {
	device, err := p.Device(ctx, id)
	if err != nil {
		return tuya.Spec{}, err
	}
	return tuya.Spec{
		Category: Category,
		Functions: []tuya.Function{{
			Code: ButtonCode,
			Type: tuya.FunctionTypeButton,
			Name: device.Name,
		}},
	}, nil
}

// Command runs the scene.
func (p *Provider) Command(ctx context.Context, id string, cmds []tuya.Command) error {
	if _, err := p.Device(ctx, id); err != nil {
		return err
	}
	for _, cmd := range cmds {
		if cmd.Code != ButtonCode {
			return fmt.Errorf("scene: %s has no function %q", id, cmd.Code)
		}
		if err := p.client.TriggerScene(ctx, id); err != nil {
			return fmt.Errorf("scene: trigger %s: %w", id, err)
		}
	}
	return nil
}
