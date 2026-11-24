// Package dashboard stores which controls the home screen shows.
//
// A widget is one control — a device's single function — rather than a whole
// device card. On a wall tablet that is what a remote is: six buttons of the
// size the user chose, not six card-shaped blocks.
//
// The selection lives on the server, not in the browser. A tablet on a wall
// gets its cache cleared, its browser reinstalled, or the hardware replaced;
// localStorage would take the layout with it, a file would not. It also lets a
// phone and the tablet show the same remote.
package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Widget is one control placed on the home screen: the function `code` of the
// device `device_id`.
type Widget struct {
	// DeviceID is the device the control belongs to.
	DeviceID string `json:"device_id"`
	// Code is the function inside that device, e.g. "switch_led" or "trigger".
	Code string `json:"code"`
	// Title overrides the label the device's thing model provides. Useful when
	// a scene is called "главный свет вкл/выкл" but the button should read
	// "Свет".
	Title string `json:"title,omitempty"`
	// Span is how many grid columns the widget occupies. 0 and 1 mean one
	// column; 2 means two. It is bounded so a payload cannot ask for a wall of
	// one widget.
	Span int `json:"span,omitempty"`
}

// ID identifies a widget. A device function is unique, so the pair is enough.
func (w Widget) ID() string { return w.DeviceID + ":" + w.Code }

// MaxWidgets bounds a stored layout. A remote with hundreds of controls is a
// mistake, not a use case, and the bound keeps a bad payload from bloating the
// file.
const MaxWidgets = 200

// ErrInvalid means a submitted layout is not storable as given.
var ErrInvalid = errors.New("dashboard: invalid layout")

// Store keeps the layout in a JSON file.
type Store struct {
	path string

	mu      sync.RWMutex
	widgets []Widget
	loaded  bool
}

// New opens a store backed by path. An empty path keeps the layout in memory
// only, which is what tests and a transient run want.
func New(path string) *Store {
	return &Store{path: path}
}

// Path reports the backing file.
func (s *Store) Path() string { return s.path }

// Widgets returns a copy of the current layout.
func (s *Store) Widgets() ([]Widget, error) {
	s.mu.RLock()
	if s.loaded {
		out := append([]Widget(nil), s.widgets...)
		s.mu.RUnlock()
		return out, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return append([]Widget(nil), s.widgets...), nil
	}
	widgets, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	s.widgets = widgets
	s.loaded = true
	return append([]Widget(nil), widgets...), nil
}

// Replace validates and stores a new layout.
func (s *Store) Replace(widgets []Widget) error {
	clean, err := Normalize(widgets)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeLocked(clean); err != nil {
		return err
	}
	s.widgets = clean
	s.loaded = true
	return nil
}

// readLocked loads the file. A missing file is an empty layout, not an error:
// that is the state before anybody has configured a remote.
func (s *Store) readLocked() ([]Widget, error) {
	if s.path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: read %s: %w", s.path, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var file struct {
		Widgets []Widget `json:"widgets"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("dashboard: parse %s: %w", s.path, err)
	}
	// A file written by hand could hold duplicates or bad spans; normalizing on
	// read means a broken file degrades to a usable layout instead of an error
	// on every request.
	clean, err := Normalize(file.Widgets)
	if err != nil {
		return nil, fmt.Errorf("dashboard: %s: %w", s.path, err)
	}
	return clean, nil
}

// writeLocked writes the layout atomically: a crash mid-write must not leave a
// truncated file, or the next start would lose the whole remote.
func (s *Store) writeLocked(widgets []Widget) error {
	if s.path == "" {
		return nil
	}
	payload, err := json.MarshalIndent(struct {
		Widgets []Widget `json:"widgets"`
	}{Widgets: widgets}, "", "  ")
	if err != nil {
		return fmt.Errorf("dashboard: encode: %w", err)
	}
	payload = append(payload, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("dashboard: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".dashboard-*.json")
	if err != nil {
		return fmt.Errorf("dashboard: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return fmt.Errorf("dashboard: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("dashboard: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("dashboard: close: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("dashboard: chmod: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("dashboard: replace %s: %w", s.path, err)
	}
	return nil
}

// Normalize validates a layout and returns the canonical form: trimmed fields,
// spans clamped to 1..2, duplicates rejected.
func Normalize(widgets []Widget) ([]Widget, error) {
	if len(widgets) > MaxWidgets {
		return nil, fmt.Errorf("%w: %d widgets, max %d", ErrInvalid, len(widgets), MaxWidgets)
	}
	seen := make(map[string]struct{}, len(widgets))
	out := make([]Widget, 0, len(widgets))
	for i, w := range widgets {
		w.DeviceID = strings.TrimSpace(w.DeviceID)
		w.Code = strings.TrimSpace(w.Code)
		w.Title = strings.TrimSpace(w.Title)
		if w.DeviceID == "" {
			return nil, fmt.Errorf("%w: widget %d has no device_id", ErrInvalid, i)
		}
		if w.Code == "" {
			return nil, fmt.Errorf("%w: widget %d has no code", ErrInvalid, i)
		}
		if len(w.Title) > 60 {
			return nil, fmt.Errorf("%w: widget %d title is longer than 60 characters", ErrInvalid, i)
		}
		switch {
		case w.Span <= 1:
			w.Span = 1
		case w.Span > 2:
			w.Span = 2
		}
		id := w.ID()
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("%w: %s appears twice", ErrInvalid, id)
		}
		seen[id] = struct{}{}
		out = append(out, w)
	}
	return out, nil
}
