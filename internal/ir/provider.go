// Package ir exposes Tuya infrared blasters as CoolHome devices.
//
// An IR blaster owns remote controls; each remote owns keys. A key is a
// momentary action, not a state, so a remote is presented as a device whose
// writable "functions" are buttons: writing value true presses the button.
package ir

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// Client is the slice of the Tuya client this provider needs.
type Client interface {
	Blasters(ctx context.Context) ([]string, error)
	IRRemotes(ctx context.Context, blaster string) ([]tuya.IRRemote, error)
	IRSubDevices(ctx context.Context, blaster string) ([]tuya.IRRemote, error)
	IRKeys(ctx context.Context, blaster, remoteID string) ([]tuya.IRKey, error)
	IRSendKey(ctx context.Context, blaster, remoteID string, key tuya.IRKey) error
}

// keysTTL bounds how stale a cached key list may be. Keys change when someone
// adds a remote in the Tuya app, which is rare.
const keysTTL = 30 * time.Minute

// Provider serves IR remotes through the same interface as physical devices.
type Provider struct {
	client   Client
	blasters []string
	log      *slog.Logger
	now      func() time.Time

	mu        sync.Mutex
	blasterOK bool
	cached    []string
	keys      map[string]cachedKeys
	// owners maps a remote id to the blaster that holds it, filled while
	// listing. The cloud's remote_id is the same id the device has in the home
	// listing, so the id alone does not say which blaster to address.
	owners map[string]string
	// disabled is set once the project turns out not to subscribe the IR
	// service. Retrying every poll would only produce the same refusal.
	disabled bool
	// skipLearned drops learned RF switches from the listing. They carry no
	// writable data point, and scenes provide the working alternative, so
	// showing both would offer the same light twice — once broken.
	skipLearned bool
}

// Options configures the provider.
type Options struct {
	// Blasters pins the blaster device ids. Empty means discover them.
	Blasters []string
	// SkipLearned hides learned RF switches when scenes cover them.
	SkipLearned bool
}

type cachedKeys struct {
	keys      []tuya.IRKey
	fetchedAt time.Time
}

// New builds a provider.
func New(client Client, opts Options, logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{
		client:      client,
		blasters:    opts.Blasters,
		skipLearned: opts.SkipLearned,
		log:         logger,
		now:         time.Now,
		keys:        map[string]cachedKeys{},
		owners:      map[string]string{},
	}
}

// isLearnedSwitch reports a remote that is a learned RF switch: a sub-device of
// the blaster in the rf_switch category. Such a remote has no code-library key,
// so the cloud cannot transmit it; its keys come out Passive.
func isLearnedSwitch(r tuya.IRRemote) bool {
	return r.IsSubDevice() && strings.HasPrefix(strings.ToLower(r.Category), "rf_switch")
}

// remoteKey is the identifier used in the remote's API path and as the device
// id CoolHome serves. Both come from the cloud's remote_id: addressing a remote
// by its remote_index answers 1106 "permission deny", which looks like an
// authorization fault but is simply the wrong identifier.
//
// The cloud's remote_id equals the device id of the remote in the home listing,
// so serving it unchanged lets a composite provider replace the bare device
// entry rather than showing the same remote twice.
func remoteKey(r tuya.IRRemote) string {
	return r.ID
}

// blasterOf finds the blaster holding a remote, using what the last listing
// recorded.
func (p *Provider) blasterOf(id string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, ok := p.owners[id]
	return b, ok
}

// remember records which blaster holds a remote.
func (p *Provider) remember(id, blaster string) {
	p.mu.Lock()
	p.owners[id] = blaster
	p.mu.Unlock()
}

// resolveBlaster returns the blaster holding id, discovering it when the remote
// has not been listed yet. Spec and Command can be reached before the first
// listing (a direct GET, or a test), so they must not depend on that cache.
func (p *Provider) resolveBlaster(ctx context.Context, id string) (string, error) {
	if b, ok := p.blasterOf(id); ok {
		return b, nil
	}
	ids, err := p.blasterIDs(ctx)
	if err != nil {
		if tuya.IsServiceNotSubscribed(err) {
			p.disable(err)
			return "", ErrNotFound
		}
		return "", fmt.Errorf("ir: list blasters: %w", err)
	}
	for _, b := range ids {
		remotes, rerr := p.remotesOf(ctx, b)
		if rerr != nil {
			if tuya.IsServiceNotSubscribed(rerr) {
				p.disable(rerr)
				return "", ErrNotFound
			}
			continue
		}
		for _, r := range remotes {
			if remoteKey(r) == id {
				p.remember(id, b)
				return b, nil
			}
		}
	}
	return "", fmt.Errorf("%w: %s", ErrNotFound, id)
}

// BlasterIDs resolves the blasters once and remembers them, so a home with no
// blasters does not re-list devices on every call.
func (p *Provider) blasterIDs(ctx context.Context) ([]string, error) {
	if len(p.blasters) > 0 {
		return p.blasters, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blasterOK {
		return p.cached, nil
	}
	ids, err := p.client.Blasters(ctx)
	if err != nil {
		return nil, err
	}
	p.cached = ids
	p.blasterOK = true
	return ids, nil
}

// unavailable reports whether the IR service is known to be unsubscribed.
func (p *Provider) unavailable() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.disabled
}

// disable records that the IR service is not available and explains it once.
// The provider then serves nothing instead of failing every refresh.
func (p *Provider) disable(err error) {
	p.mu.Lock()
	first := !p.disabled
	p.disabled = true
	p.mu.Unlock()
	if first {
		p.log.Warn("IR Control is unavailable: subscribe the service in the Tuya console "+
			"(Cloud → Development → your project → Service API) to control infrared remotes",
			"err", err)
	}
}

// remotesOf returns every remote the blaster can address: code-library remotes
// plus the blaster's own sub-devices. A learned remote — every RF switch paired
// to the blaster, and appliances paired through it — is a sub-device and is
// absent from the /remotes list, so both must be asked for.
func (p *Provider) remotesOf(ctx context.Context, blaster string) ([]tuya.IRRemote, error) {
	library, err := p.client.IRRemotes(ctx, blaster)
	if err != nil {
		if tuya.IsServiceNotSubscribed(err) {
			return nil, err
		}
		return nil, fmt.Errorf("ir: list remotes of %s: %w", blaster, err)
	}
	subs, err := p.client.IRSubDevices(ctx, blaster)
	if err != nil {
		if tuya.IsServiceNotSubscribed(err) {
			return nil, err
		}
		return nil, fmt.Errorf("ir: list sub-devices of %s: %w", blaster, err)
	}

	seen := make(map[string]struct{}, len(library)+len(subs))
	out := make([]tuya.IRRemote, 0, len(library)+len(subs))
	for _, r := range append(library, subs...) {
		if r.ID == "" {
			continue
		}
		if _, dup := seen[r.ID]; dup {
			continue
		}
		seen[r.ID] = struct{}{}
		out = append(out, r)
	}
	return out, nil
}

// ListDevices returns every IR remote as a device.
func (p *Provider) ListDevices(ctx context.Context, ids []string) ([]tuya.Device, error) {
	if p.unavailable() {
		return nil, nil
	}
	blasters, err := p.blasterIDs(ctx)
	if err != nil {
		if tuya.IsServiceNotSubscribed(err) {
			p.disable(err)
			return nil, nil
		}
		return nil, fmt.Errorf("ir: list blasters: %w", err)
	}

	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}

	var out []tuya.Device
	for _, blaster := range blasters {
		remotes, err := p.remotesOf(ctx, blaster)
		if err != nil {
			if tuya.IsServiceNotSubscribed(err) {
				p.disable(err)
				return nil, nil
			}
			return nil, err
		}
		for _, r := range remotes {
			id := remoteKey(r)
			if id == "" {
				continue
			}
			if p.skipLearned && isLearnedSwitch(r) {
				// Scenes are the working path for these; hide the broken twin.
				continue
			}
			p.remember(id, blaster)
			if len(want) > 0 {
				if _, ok := want[id]; !ok {
					continue
				}
			}
			out = append(out, tuya.Device{
				ID:       id,
				Name:     r.Name,
				Category: "ir_remote",
				Online:   r.Available(),
			})
		}
	}
	return out, nil
}

// Device returns one remote.
func (p *Provider) Device(ctx context.Context, id string) (tuya.Device, error) {
	if p.unavailable() {
		return tuya.Device{}, ErrNotFound
	}
	blaster, err := p.resolveBlaster(ctx, id)
	if err != nil {
		return tuya.Device{}, err
	}
	remotes, err := p.remotesOf(ctx, blaster)
	if err != nil {
		if tuya.IsServiceNotSubscribed(err) {
			p.disable(err)
			return tuya.Device{}, ErrNotFound
		}
		return tuya.Device{}, err
	}
	for _, r := range remotes {
		if remoteKey(r) == id {
			return tuya.Device{ID: id, Name: r.Name, Category: "ir_remote", Online: r.Available()}, nil
		}
	}
	return tuya.Device{}, ErrNotFound
}

// ErrNotFound reports a remote this provider does not own. It wraps the home
// sentinel so a composite provider and the HTTP layer map it to 404 by the same
// rule they use for physical devices.
var ErrNotFound = fmt.Errorf("ir: remote not found: %w", home.ErrNotFound)

// Spec turns the remote's keys into button functions.
func (p *Provider) Spec(ctx context.Context, id string) (tuya.Spec, error) {
	keys, err := p.keysFor(ctx, id)
	if err != nil {
		return tuya.Spec{}, err
	}
	spec := tuya.Spec{Category: "ir_remote", Functions: make([]tuya.Function, 0, len(keys))}
	for _, k := range keys {
		name := k.Name
		if name == "" {
			name = k.Key
		}
		kind := tuya.FunctionTypeButton
		if !k.Standard {
			// A learned key cannot be transmitted by the cloud: the payload is
			// accepted and reported as sent, but nothing is emitted. Exposing it
			// as a button would put a control on screen that never works.
			kind = tuya.FunctionTypePassive
		}
		spec.Functions = append(spec.Functions, tuya.Function{
			Code: k.Key,
			Type: kind,
			Name: name,
			DPID: k.ID,
		})
	}
	return spec, nil
}

// Command presses the named keys in order.
func (p *Provider) Command(ctx context.Context, id string, cmds []tuya.Command) error {
	keys, err := p.keysFor(ctx, id)
	if err != nil {
		return err
	}
	blaster, err := p.resolveBlaster(ctx, id)
	if err != nil {
		return err
	}
	byCode := make(map[string]tuya.IRKey, len(keys))
	for _, k := range keys {
		byCode[k.Key] = k
	}
	for _, cmd := range cmds {
		key, found := byCode[cmd.Code]
		if !found {
			return fmt.Errorf("ir: remote %s has no button %q", id, cmd.Code)
		}
		if err := p.client.IRSendKey(ctx, blaster, id, key); err != nil {
			return fmt.Errorf("ir: send %q: %w", cmd.Code, err)
		}
	}
	return nil
}

// keysFor returns the remote's keys, caching them for keysTTL.
func (p *Provider) keysFor(ctx context.Context, id string) ([]tuya.IRKey, error) {
	p.mu.Lock()
	if entry, ok := p.keys[id]; ok && p.now().Sub(entry.fetchedAt) < keysTTL {
		p.mu.Unlock()
		return entry.keys, nil
	}
	p.mu.Unlock()

	blaster, berr := p.resolveBlaster(ctx, id)
	if berr != nil {
		return nil, berr
	}
	keys, err := p.client.IRKeys(ctx, blaster, id)
	if err != nil {
		if tuya.IsServiceNotSubscribed(err) {
			p.disable(err)
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("ir: keys of %s: %w", id, err)
	}

	p.mu.Lock()
	p.keys[id] = cachedKeys{keys: keys, fetchedAt: p.now()}
	p.mu.Unlock()
	return keys, nil
}
