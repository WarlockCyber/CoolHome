package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store keeps the rule set in a JSON file, the way the dashboard keeps the
// layout: atomic writes, one lock for read-modify-write, and a missing file is
// an empty set rather than an error.
type Store struct {
	path string

	mu     sync.RWMutex
	set    Set
	loaded bool
}

// New opens a store backed by path. An empty path keeps the set in memory only,
// which is what tests and a transient run want.
func New(path string) *Store {
	return &Store{path: path}
}

// Path reports the backing file.
func (s *Store) Path() string { return s.path }

// Get returns a copy of the current set.
func (s *Store) Get() (Set, error) {
	s.mu.RLock()
	if s.loaded {
		out := copySet(s.set)
		s.mu.RUnlock()
		return out, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return copySet(s.set), nil
	}
	set, err := s.readLocked()
	if err != nil {
		return Set{}, err
	}
	s.set = set
	s.loaded = true
	return copySet(set), nil
}

// Save validates and stores a whole set.
func (s *Store) Save(set Set) (Set, error) {
	clean, err := Normalize(set)
	if err != nil {
		return Set{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeLocked(clean); err != nil {
		return Set{}, err
	}
	s.set = clean
	s.loaded = true
	return copySet(clean), nil
}

// Update reads, edits and stores the set under one lock, so two editors — a
// phone toggling a rule, the page saving a new one — cannot overwrite each
// other's changes.
func (s *Store) Update(edit func(*Set) error) (Set, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		set, err := s.readLocked()
		if err != nil {
			return Set{}, err
		}
		s.set = set
		s.loaded = true
	}
	next := copySet(s.set)
	if err := edit(&next); err != nil {
		return Set{}, err
	}
	clean, err := Normalize(next)
	if err != nil {
		return Set{}, err
	}
	if err := s.writeLocked(clean); err != nil {
		return Set{}, err
	}
	s.set = clean
	return copySet(clean), nil
}

// copySet deep-copies the parts a caller could mutate in place. The rule slices
// and condition slices are what an editor touches, so a shallow copy would let
// one request change another's view of the set.
func copySet(in Set) Set {
	out := Set{
		Groups: append([]Group(nil), in.Groups...),
		Rules:  make([]Rule, len(in.Rules)),
	}
	for i, r := range in.Rules {
		r.When = append([]Condition(nil), r.When...)
		r.Actions = append([]Action(nil), r.Actions...)
		if r.At != nil {
			at := *r.At
			at.Days = append([]string(nil), r.At.Days...)
			r.At = &at
		}
		out.Rules[i] = r
	}
	return out
}

func (s *Store) readLocked() (Set, error) {
	if s.path == "" {
		return Set{}, nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Set{}, nil
		}
		return Set{}, fmt.Errorf("rules: read %s: %w", s.path, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Set{}, nil
	}
	var set Set
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&set); err != nil {
		return Set{}, fmt.Errorf("rules: parse %s: %w", s.path, err)
	}
	// A hand-written file can be wrong; normalizing on read turns that into a
	// clear message instead of a surprise on every evaluation.
	clean, err := Normalize(set)
	if err != nil {
		return Set{}, fmt.Errorf("rules: %s: %w", s.path, err)
	}
	return clean, nil
}

// writeLocked writes atomically: a crash mid-write must not leave a truncated
// file, or a restart would lose every automation at once.
func (s *Store) writeLocked(set Set) error {
	if s.path == "" {
		return nil
	}
	payload, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return fmt.Errorf("rules: encode: %w", err)
	}
	payload = append(payload, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("rules: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".rules-*.json")
	if err != nil {
		return fmt.Errorf("rules: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return fmt.Errorf("rules: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("rules: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("rules: close: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("rules: chmod: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("rules: replace %s: %w", s.path, err)
	}
	return nil
}
