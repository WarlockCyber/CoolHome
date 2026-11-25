// Package dashboard stores which controls the home screen shows.
//
// A widget is one control — a device's single function — rather than a whole
// device card. On a wall tablet that is what a remote is: six buttons of the
// size the user chose, not six card-shaped blocks.
//
// The same file also holds the side panel: the devices whose full cards sit in
// the right-hand column, beside the remote. It is a separate list because it
// answers a different question — the remote is what you reach for, the panel is
// what you want to see at a glance — and because a device card carries every
// control of the device, while a widget carries one.
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
	"unicode/utf8"
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

// MaxPanel bounds the side panel. Its cards are full device cards, several rows
// tall each: more than a dozen would push the column past any screen and stop
// being "what you want to see at a glance".
const MaxPanel = 12

// Layout is the whole home screen: the remote's widgets and the side panel.
type Layout struct {
	// Widgets are the controls on the remote.
	Widgets []Widget `json:"widgets"`
	// Panel names the devices whose cards stand in the side column, in the
	// order the user chose. Only the id is stored: the name, the state and the
	// controls all come from the live device, and a copy here would go stale
	// the moment somebody renamed the device.
	Panel []string `json:"panel,omitempty"`
	// Names renames devices for this installation only. The cloud keeps its
	// own names — "WIFI Temperature and Humidity Sensor" is what Tuya says, and
	// no API call changes it — so the name a wall tablet shows is stored here,
	// next to the layout, and applied when the devices are served.
	//
	// Keyed by device id, which is the only identifier that survives a rename
	// in the app: the cloud's name is what the user is changing.
	Names map[string]string `json:"names,omitempty"`
}

// MaxNameLen bounds a local name. It is the same bound the remote's widget
// titles use: a card heading, not a description.
//
// Both are counted in runes. A byte count would make the limit depend on the
// alphabet: Cyrillic takes two bytes per letter, so a Russian name would be
// limited to half the length of an English one — in a product whose interface
// is Russian.
const MaxNameLen = MaxTitleLen

// MaxTitleLen bounds a widget's title.
const MaxTitleLen = 60

// ErrInvalid means a submitted layout is not storable as given.
var ErrInvalid = errors.New("dashboard: invalid layout")

// Store keeps the layout in a JSON file.
type Store struct {
	path string

	mu     sync.RWMutex
	layout Layout
	loaded bool
}

// New opens a store backed by path. An empty path keeps the layout in memory
// only, which is what tests and a transient run want.
func New(path string) *Store {
	return &Store{path: path}
}

// Path reports the backing file.
func (s *Store) Path() string { return s.path }

// Current returns a copy of the stored layout.
func (s *Store) Current() (Layout, error) {
	s.mu.RLock()
	if s.loaded {
		out := s.currentLocked()
		s.mu.RUnlock()
		return out, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return s.currentLocked(), nil
	}
	layout, err := s.readLocked()
	if err != nil {
		return Layout{}, err
	}
	s.layout = layout
	s.loaded = true
	return s.currentLocked(), nil
}

// currentLocked copies the in-memory layout. Callers hold the lock.
func (s *Store) currentLocked() Layout {
	return Layout{
		Widgets: append([]Widget(nil), s.layout.Widgets...),
		Panel:   append([]string(nil), s.layout.Panel...),
		Names:   copyNames(s.layout.Names),
	}
}

// copyNames copies the name map. A caller that assigns into the result must not
// reach the store's own copy through a shared map.
func copyNames(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for id, name := range in {
		out[id] = name
	}
	return out
}

// Names returns the local device names, keyed by device id. It is called on
// every snapshot, so it copies rather than handing out the store's map.
func (s *Store) Names() (map[string]string, error) {
	layout, err := s.Current()
	if err != nil {
		return nil, err
	}
	return layout.Names, nil
}

// Update reads the layout, lets `edit` change it, and stores the result — all
// under one lock. A read-then-save done by the caller would race with another
// caller doing the same: a phone adding a card to the panel and a tablet
// reordering the remote are two writers to one document, and each would save a
// copy that lacks the other's change.
func (s *Store) Update(edit func(*Layout) error) (Layout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		layout, err := s.readLocked()
		if err != nil {
			return Layout{}, err
		}
		s.layout = layout
		s.loaded = true
	}
	next := s.currentLocked()
	if err := edit(&next); err != nil {
		return Layout{}, err
	}
	clean, err := Normalize(next)
	if err != nil {
		return Layout{}, err
	}
	if err := s.writeLocked(clean); err != nil {
		return Layout{}, err
	}
	s.layout = clean
	return s.currentLocked(), nil
}

// Save validates and stores a new layout.
func (s *Store) Save(layout Layout) (Layout, error) {
	clean, err := Normalize(layout)
	if err != nil {
		return Layout{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeLocked(clean); err != nil {
		return Layout{}, err
	}
	s.layout = clean
	s.loaded = true
	return s.currentLocked(), nil
}

// readLocked loads the file. A missing file is an empty layout, not an error:
// that is the state before anybody has configured a remote.
func (s *Store) readLocked() (Layout, error) {
	if s.path == "" {
		return Layout{}, nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Layout{}, nil
		}
		return Layout{}, fmt.Errorf("dashboard: read %s: %w", s.path, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Layout{}, nil
	}
	var file Layout
	if err := json.Unmarshal(raw, &file); err != nil {
		return Layout{}, fmt.Errorf("dashboard: parse %s: %w", s.path, err)
	}
	// A file written by hand could hold duplicates or bad spans; normalizing on
	// read means a broken file degrades to a usable layout instead of an error
	// on every request. A file from before the panel existed carries no panel
	// key at all, and lands here as an empty one.
	clean, err := Normalize(file)
	if err != nil {
		return Layout{}, fmt.Errorf("dashboard: %s: %w", s.path, err)
	}
	return clean, nil
}

// writeLocked writes the layout atomically: a crash mid-write must not leave a
// truncated file, or the next start would lose the whole remote.
func (s *Store) writeLocked(layout Layout) error {
	if s.path == "" {
		return nil
	}
	payload, err := json.MarshalIndent(layout, "", "  ")
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
// spans clamped to 1..2, duplicates rejected, and a panel list that names each
// device once.
func Normalize(layout Layout) (Layout, error) {
	widgets := layout.Widgets
	if len(widgets) > MaxWidgets {
		return Layout{}, fmt.Errorf("%w: %d widgets, max %d", ErrInvalid, len(widgets), MaxWidgets)
	}
	seen := make(map[string]struct{}, len(widgets))
	out := make([]Widget, 0, len(widgets))
	for i, w := range widgets {
		w.DeviceID = strings.TrimSpace(w.DeviceID)
		w.Code = strings.TrimSpace(w.Code)
		w.Title = strings.TrimSpace(w.Title)
		if w.DeviceID == "" {
			return Layout{}, fmt.Errorf("%w: widget %d has no device_id", ErrInvalid, i)
		}
		if w.Code == "" {
			return Layout{}, fmt.Errorf("%w: widget %d has no code", ErrInvalid, i)
		}
		// Counted in runes, like a local device name: len() on a string counts
		// bytes, so a Cyrillic title hit this limit at half the length a Latin
		// one did — and because a rename stores the name and the titles it
		// rewrites in one request, a title that was too long by bytes took the
		// name down with it.
		if utf8.RuneCountInString(w.Title) > MaxTitleLen {
			return Layout{}, fmt.Errorf("%w: widget %d title is longer than %d characters", ErrInvalid, i, MaxTitleLen)
		}
		switch {
		case w.Span <= 1:
			w.Span = 1
		case w.Span > 2:
			w.Span = 2
		}
		id := w.ID()
		if _, dup := seen[id]; dup {
			return Layout{}, fmt.Errorf("%w: %s appears twice", ErrInvalid, id)
		}
		seen[id] = struct{}{}
		out = append(out, w)
	}

	panel := layout.Panel
	if len(panel) > MaxPanel {
		return Layout{}, fmt.Errorf("%w: %d panel cards, max %d", ErrInvalid, len(panel), MaxPanel)
	}
	devices := make(map[string]struct{}, len(panel))
	panelOut := make([]string, 0, len(panel))
	for i, id := range panel {
		id = strings.TrimSpace(id)
		if id == "" {
			return Layout{}, fmt.Errorf("%w: panel entry %d is empty", ErrInvalid, i)
		}
		if len(id) > 64 {
			return Layout{}, fmt.Errorf("%w: panel entry %d is longer than 64 characters", ErrInvalid, i)
		}
		if _, dup := devices[id]; dup {
			return Layout{}, fmt.Errorf("%w: device %s is in the panel twice", ErrInvalid, id)
		}
		devices[id] = struct{}{}
		panelOut = append(panelOut, id)
	}
	names, err := NormalizeNames(layout.Names)
	if err != nil {
		return Layout{}, err
	}
	return Layout{Widgets: out, Panel: panelOut, Names: names}, nil
}

// NormalizeNames validates local device names: a trimmed name of a bounded
// length against a device that exists. An empty name is dropped, which is how
// the caller says "show the cloud's name again" — keeping an empty entry would
// only mean the same thing less clearly.
func NormalizeNames(in map[string]string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for id, name := range in {
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id == "" {
			return nil, fmt.Errorf("%w: a device name has no device_id", ErrInvalid)
		}
		if len(id) > 64 {
			return nil, fmt.Errorf("%w: device id %q is longer than 64 characters", ErrInvalid, id)
		}
		if name == "" {
			continue
		}
		if len([]rune(name)) > MaxNameLen {
			return nil, fmt.Errorf("%w: the name for %s is longer than %d characters", ErrInvalid, id, MaxNameLen)
		}
		out[id] = name
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
