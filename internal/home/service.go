// Package home turns a device provider into the state the web UI serves:
// a cached snapshot, an explicit command surface, and validation of every
// write against the device's thing model.
package home

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// Provider is what the Tuya client and the demo home both implement.
type Provider interface {
	ListDevices(ctx context.Context, ids []string) ([]tuya.Device, error)
	Device(ctx context.Context, id string) (tuya.Device, error)
	Spec(ctx context.Context, id string) (tuya.Spec, error)
	Command(ctx context.Context, id string, cmds []tuya.Command) error
}

var ErrNotFound = errors.New("device not found")

// ValidationError means the request was well-formed but names a data point the
// device does not expose, or a value outside its declared domain.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// DeviceView is a device plus its thing model (when it could be fetched).
type DeviceView struct {
	tuya.Device
	Spec      *tuya.Spec `json:"-"`
	SpecError string     `json:"-"`
	// SpecUnsupported marks a device the cloud will never describe over the
	// data-point API — an RF switch behind an IR blaster, or the blaster
	// itself. It is a permanent property, not a transient failure, so callers
	// should explain it rather than report an error.
	SpecUnsupported bool `json:"-"`
}

// Snapshot is an immutable view of the home at a point in time.
type Snapshot struct {
	Devices  []DeviceView
	LastSync time.Time
	Error    string
}

// Service keeps a warm cache of devices and serialises cloud access.
type Service struct {
	prov     Provider
	ids      []string
	interval time.Duration
	log      *slog.Logger
	now      func() time.Time

	mu       sync.RWMutex
	devices  map[string]DeviceView
	order    []string
	lastSync time.Time
	lastErr  string
	specs    map[string]specEntry
}

type specEntry struct {
	spec        *tuya.Spec
	fetchedAt   time.Time
	err         string
	unsupported bool
}

const (
	specTTL      = 30 * time.Minute
	specErrRetry = 2 * time.Minute
	// specUnsupportedTTL applies to devices that will never describe
	// themselves. Asking again every two minutes costs a request per poll for
	// every RF switch in the home and cannot succeed.
	specUnsupportedTTL = 24 * time.Hour
	maxCommands        = 20
)

// New builds a service. ids optionally restricts the home to specific devices.
func New(prov Provider, ids []string, interval time.Duration, logger *slog.Logger) *Service {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		prov:     prov,
		ids:      ids,
		interval: interval,
		log:      logger,
		now:      time.Now,
		devices:  map[string]DeviceView{},
		specs:    map[string]specEntry{},
	}
}

// Run refreshes immediately and then every interval until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	if err := s.Refresh(ctx); err != nil {
		s.log.Error("initial refresh failed", "err", err)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Refresh(ctx); err != nil {
				s.log.Warn("refresh failed", "err", err)
			}
		}
	}
}

// Refresh reloads the device list, its statuses, and (cached) specs.
func (s *Service) Refresh(ctx context.Context) error {
	devices, err := s.prov.ListDevices(ctx, s.ids)
	if err != nil {
		s.setError(err)
		return err
	}
	views := make(map[string]DeviceView, len(devices))
	order := make([]string, 0, len(devices))
	for _, d := range devices {
		if len(d.Status) == 0 {
			// Some list responses omit status; fall back to the detail endpoint.
			if full, ferr := s.prov.Device(ctx, d.ID); ferr == nil {
				d = full
			}
		}
		spec, specErr, unsupported := s.specFor(ctx, d.ID)
		if spec != nil && d.Note == "" {
			// A spec may describe keys the cloud cannot transmit. Say so once,
			// at the device level, so no client has to infer it from types.
			for _, fn := range spec.Functions {
				if fn.Type == tuya.FunctionTypePassive {
					d.Note = "Устройство отдаёт кнопки, но Tuya Cloud не передаёт " +
						"выученные RF-коды: команда принимается и не доходит до устройства. " +
						"Управление — с физического пульта или в приложении Tuya."
					break
				}
			}
		}
		views[d.ID] = DeviceView{
			Device:          d,
			Spec:            spec,
			SpecError:       specErr,
			SpecUnsupported: unsupported,
		}
		order = append(order, d.ID)
	}

	s.mu.Lock()
	s.devices = views
	s.order = order
	s.lastSync = s.now()
	s.lastErr = ""
	s.mu.Unlock()
	return nil
}

func (s *Service) setError(err error) {
	s.mu.Lock()
	s.lastErr = err.Error()
	s.mu.Unlock()
}

func (s *Service) specFor(ctx context.Context, id string) (*tuya.Spec, string, bool) {
	s.mu.RLock()
	entry, cached := s.specs[id]
	s.mu.RUnlock()

	ttl := specTTL
	switch {
	case entry.unsupported:
		ttl = specUnsupportedTTL
	case entry.err != "":
		ttl = specErrRetry
	}
	if cached && s.now().Sub(entry.fetchedAt) < ttl {
		return entry.spec, entry.err, entry.unsupported
	}

	spec, err := s.prov.Spec(ctx, id)
	next := specEntry{fetchedAt: s.now()}
	if err != nil {
		next.err = err.Error()
		next.unsupported = tuya.IsUnsupportedDevice(err)
	} else {
		next.spec = &spec
	}
	s.mu.Lock()
	s.specs[id] = next
	s.mu.Unlock()
	return next.spec, next.err, next.unsupported
}

// Snapshot returns the current home state.
func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := Snapshot{LastSync: s.lastSync, Error: s.lastErr}
	out.Devices = make([]DeviceView, 0, len(s.order))
	for _, id := range s.order {
		if v, ok := s.devices[id]; ok {
			out.Devices = append(out.Devices, v)
		}
	}
	return out
}

// Device returns one cached device.
func (s *Service) Device(id string) (DeviceView, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.devices[id]
	return v, ok
}

// Command validates and sends data points, then returns the refreshed device.
func (s *Service) Command(ctx context.Context, id string, cmds []tuya.Command) (DeviceView, error) {
	view, ok := s.Device(id)
	if !ok {
		return DeviceView{}, ErrNotFound
	}
	if len(cmds) == 0 {
		return view, invalid("no commands given")
	}
	if len(cmds) > maxCommands {
		return view, invalid("too many commands: %d (max %d)", len(cmds), maxCommands)
	}
	if view.Spec != nil {
		for _, cmd := range cmds {
			if err := ValidateCommand(cmd, view.Spec); err != nil {
				return view, err
			}
		}
	}
	if err := s.prov.Command(ctx, id, cmds); err != nil {
		return view, err
	}

	// Give the cloud a moment to apply the write, then show fresh state.
	timer := time.NewTimer(350 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	if dev, err := s.prov.Device(ctx, id); err == nil {
		s.mu.Lock()
		if current, ok := s.devices[id]; ok {
			current.Device = dev
			s.devices[id] = current
			view = current
		}
		s.mu.Unlock()
	}
	return view, nil
}

// ValidateCommand checks a write against the device's declared functions.
// Devices whose thing model is unavailable are not blocked here; the cloud
// remains the final authority.
func ValidateCommand(cmd tuya.Command, spec *tuya.Spec) error {
	var fn *tuya.Function
	for i := range spec.Functions {
		if spec.Functions[i].Code == cmd.Code {
			fn = &spec.Functions[i]
			break
		}
	}
	if fn == nil {
		return invalid("device has no writable function %q", cmd.Code)
	}
	vs := fn.ValueSpec()

	switch strings.ToLower(fn.Type) {
	case "boolean":
		if _, ok := cmd.Value.(bool); !ok {
			return invalid("%s expects a boolean, got %T", cmd.Code, cmd.Value)
		}
	case "integer":
		f, ok := toFloat(cmd.Value)
		if !ok {
			return invalid("%s expects a number, got %T", cmd.Code, cmd.Value)
		}
		if math.Trunc(f) != f {
			return invalid("%s expects an integer, got %v", cmd.Code, f)
		}
		if vs.Min != nil && f < float64(*vs.Min) {
			return invalid("%s is below its minimum %d", cmd.Code, *vs.Min)
		}
		if vs.Max != nil && f > float64(*vs.Max) {
			return invalid("%s is above its maximum %d", cmd.Code, *vs.Max)
		}
	case "enum":
		v, ok := cmd.Value.(string)
		if !ok {
			return invalid("%s expects one of %v, got %T", cmd.Code, vs.Range, cmd.Value)
		}
		if len(vs.Range) > 0 && !slices.Contains(vs.Range, v) {
			return invalid("%s does not accept %q (allowed: %s)", cmd.Code, v, strings.Join(vs.Range, ", "))
		}
	case "string":
		if _, ok := cmd.Value.(string); !ok {
			return invalid("%s expects a string, got %T", cmd.Code, cmd.Value)
		}
	case strings.ToLower(tuya.FunctionTypePassive):
		return invalid("%s: это выученный RF-код, Tuya Cloud не передаёт такие команды", cmd.Code)
	case strings.ToLower(tuya.FunctionTypeButton):
		// A button is a momentary action: it has no state and no domain, so the
		// only meaningful write is "press". Refusing anything else keeps a
		// slider or switch value from being sent as a key press.
		b, ok := cmd.Value.(bool)
		if !ok {
			return invalid("%s is a button and expects true, got %T", cmd.Code, cmd.Value)
		}
		if !b {
			return invalid("%s is a button: only true (press) is accepted", cmd.Code)
		}
	}
	return nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		// The cloud accepts some numeric strings, but a silent conversion here
		// would hide client bugs; keep it explicit.
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
