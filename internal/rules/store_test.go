package rules

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func sampleRule(id string) Rule {
	return Rule{
		ID: id, Name: id, Enabled: true,
		When:    []Condition{{Device: "d", Code: "c", Op: "<", Value: 1}},
		Actions: []Action{{Kind: ActionCommand, Device: "p", Code: "s", Value: true}},
	}
}

func TestEmptyStoreIsAnEmptySet(t *testing.T) {
	set, err := New("").Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(set.Rules) != 0 || len(set.Groups) != 0 {
		t.Fatalf("set = %+v, want empty", set)
	}
}

func TestSaveAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	s := New(path)
	in := Set{
		Groups: []Group{{Name: "Отопление", Enabled: true}},
		Rules:  []Rule{sampleRule("r1")},
	}
	in.Rules[0].Group = "Отопление"
	if _, err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A fresh store reads the file, which is what a restart does.
	again, err := New(path).Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(again.Rules) != 1 || again.Rules[0].ID != "r1" {
		t.Fatalf("rules = %+v", again.Rules)
	}
	if again.Rules[0].Group != "Отопление" || !again.Groups[0].Enabled {
		t.Errorf("group lost: %+v", again)
	}
	if again.Rules[0].Mode != DefaultMode {
		t.Errorf("mode = %q, want the default to be filled in", again.Rules[0].Mode)
	}
}

func TestSaveRejectsInvalidSet(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "r.json"))
	if _, err := s.Save(Set{Rules: []Rule{sampleRule("r1")}}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := s.Save(Set{Rules: []Rule{{ID: "bad"}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	// The stored set must survive a rejected write.
	set, err := s.Get()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Rules) != 1 || set.Rules[0].ID != "r1" {
		t.Errorf("set was damaged by a rejected write: %+v", set)
	}
}

func TestEmptyAndMissingFilesAreEmptySets(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"missing.json": "", "empty.json": "  \n"} {
		path := filepath.Join(dir, name)
		if content != "" {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		set, err := New(path).Get()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(set.Rules) != 0 {
			t.Errorf("%s: rules = %+v", name, set.Rules)
		}
	}
}

func TestCorruptFileIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path).Get(); err == nil {
		t.Fatal("a corrupt file was read as an empty set")
	}
}

// Update is read-modify-write under one lock: two editors, a phone toggling a
// rule and the page saving another, must not lose each other's work.
func TestConcurrentUpdatesKeepBothChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	s := New(path)
	if _, err := s.Save(Set{Rules: []Rule{sampleRule("a"), sampleRule("b")}}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			// Toggle rule a off and on, always through Update.
			if _, err := s.Update(func(set *Set) error {
				for j := range set.Rules {
					if set.Rules[j].ID == "a" {
						set.Rules[j].Enabled = !set.Rules[j].Enabled
					}
				}
				return nil
			}); err != nil {
				t.Errorf("update a: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			// Rename rule b.
			if _, err := s.Update(func(set *Set) error {
				for j := range set.Rules {
					if set.Rules[j].ID == "b" {
						set.Rules[j].Name = "переименовано"
					}
				}
				return nil
			}); err != nil {
				t.Errorf("update b: %v", err)
			}
		}()
	}
	wg.Wait()

	final, err := New(path).Get()
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Rules) != 2 {
		t.Fatalf("rules = %+v, want both", final.Rules)
	}
	if final.Rules[1].Name != "переименовано" {
		t.Errorf("the rename was lost: %+v", final.Rules)
	}
}

// The returned set must not share slices with the store: an editor that appends
// to Actions in place would otherwise change the stored rule.
func TestGetReturnsACopy(t *testing.T) {
	s := New("")
	if _, err := s.Save(Set{Rules: []Rule{sampleRule("r1")}}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Get()
	if err != nil {
		t.Fatal(err)
	}
	first.Rules[0].Actions[0].Device = "подменено"
	first.Rules[0].When[0].Device = "подменено"
	first.Rules = append(first.Rules, sampleRule("r2"))

	second, err := s.Get()
	if err != nil {
		t.Fatal(err)
	}
	if second.Rules[0].Actions[0].Device == "подменено" || second.Rules[0].When[0].Device == "подменено" {
		t.Error("the store was changed through the returned set")
	}
	if len(second.Rules) != 1 {
		t.Errorf("the store gained a rule: %+v", second.Rules)
	}
}

// The file shape is what a person edits by hand: stable keys, readable values.
func TestFileShapeIsReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	s := New(path)
	rule := sampleRule("r1")
	rule.Name = "Холодно в спальне"
	rule.At = &Schedule{From: "07:30", Days: []string{"mon"}}
	rule.Cooldown = "10m"
	if _, err := s.Save(Set{Rules: []Rule{rule}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	for _, key := range []string{"id", "name", "enabled", "when", "at", "cooldown", "actions"} {
		if _, ok := file.Rules[0][key]; !ok {
			t.Errorf("field %q missing from %s", key, raw)
		}
	}
	if strings.Contains(string(raw), `"value":"180"`) {
		t.Error("numbers were written as strings")
	}
}

func TestWritesLeaveNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	s := New(path)
	for i := 0; i < 3; i++ {
		if _, err := s.Save(Set{Rules: []Rule{sampleRule("r")}}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".rules-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want just the rule file", len(entries))
	}
}
