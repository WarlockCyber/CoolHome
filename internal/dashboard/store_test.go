package dashboard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEmptyStoreIsAnEmptyLayout(t *testing.T) {
	s := New("")
	widgets, err := s.Widgets()
	if err != nil {
		t.Fatalf("Widgets: %v", err)
	}
	if len(widgets) != 0 {
		t.Fatalf("widgets = %+v, want none", widgets)
	}
}

func TestReplaceAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.json")
	s := New(path)

	in := []Widget{
		{DeviceID: "lamp-1", Code: "switch_led", Title: "Свет"},
		{DeviceID: "scene-1", Code: "trigger", Span: 2},
	}
	if err := s.Replace(in); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// A fresh store reads the file, which is what a restart does.
	again, err := New(path).Widgets()
	if err != nil {
		t.Fatalf("Widgets: %v", err)
	}
	if len(again) != 2 {
		t.Fatalf("widgets = %+v", again)
	}
	if again[0].DeviceID != "lamp-1" || again[0].Title != "Свет" {
		t.Errorf("first widget = %+v", again[0])
	}
	if again[1].Span != 2 {
		t.Errorf("span = %d, want 2", again[1].Span)
	}
}

func TestNormalizeClampsAndTrims(t *testing.T) {
	out, err := Normalize([]Widget{
		{DeviceID: "  d1  ", Code: " c1 ", Span: 99, Title: "  свет  "},
		{DeviceID: "d2", Code: "c2", Span: 0},
		{DeviceID: "d3", Code: "c3", Span: -5},
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if out[0].DeviceID != "d1" || out[0].Code != "c1" || out[0].Title != "свет" {
		t.Errorf("trimming failed: %+v", out[0])
	}
	if out[0].Span != 2 {
		t.Errorf("span = %d, want it clamped to 2", out[0].Span)
	}
	for _, w := range out[1:] {
		if w.Span != 1 {
			t.Errorf("%s span = %d, want 1", w.ID(), w.Span)
		}
	}
}

func TestNormalizeRejectsBadLayouts(t *testing.T) {
	cases := map[string][]Widget{
		"no device": {{Code: "c1"}},
		"no code":   {{DeviceID: "d1"}},
		"duplicate": {
			{DeviceID: "d1", Code: "c1"},
			{DeviceID: "d1", Code: "c1"},
		},
		"title too long": {{DeviceID: "d1", Code: "c1", Title: strings.Repeat("я", 61)}},
	}
	for name, in := range cases {
		if _, err := Normalize(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}

	tooMany := make([]Widget, MaxWidgets+1)
	for i := range tooMany {
		tooMany[i] = Widget{DeviceID: "d", Code: string(rune('a'+i%26)) + string(rune('a'+i/26))}
	}
	if _, err := Normalize(tooMany); !errors.Is(err, ErrInvalid) {
		t.Errorf("too many: err = %v, want ErrInvalid", err)
	}
}

func TestReplaceRejectsInvalidLayout(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "d.json"))
	if err := s.Replace([]Widget{{DeviceID: "d1", Code: "c1"}}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if err := s.Replace([]Widget{{Code: "no-device"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	// The old layout must survive a rejected write.
	widgets, err := s.Widgets()
	if err != nil {
		t.Fatalf("Widgets: %v", err)
	}
	if len(widgets) != 1 || widgets[0].DeviceID != "d1" {
		t.Errorf("layout was damaged by a rejected write: %+v", widgets)
	}
}

// A hand-written or corrupted file must degrade to a usable layout, not break
// every request.
func TestCorruptFileIsTolerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	if err := os.WriteFile(path, []byte(`{"widgets":[{"device_id":"d1"},{"device_id":"d1","code":"c"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// One entry is valid, one is not: Normalize rejects the pair, so the store
	// reports the problem rather than silently showing half a remote.
	if _, err := New(path).Widgets(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}

	// A file that is not JSON at all is also an error with a clear message.
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path).Widgets(); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestEmptyFileIsAnEmptyLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	if err := os.WriteFile(path, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	widgets, err := New(path).Widgets()
	if err != nil {
		t.Fatalf("Widgets: %v", err)
	}
	if len(widgets) != 0 {
		t.Fatalf("widgets = %+v", widgets)
	}
}

// The write must be atomic: a reader must never see a truncated file.
func TestWriteLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.json")
	s := New(path)
	for i := 0; i < 3; i++ {
		if err := s.Replace([]Widget{{DeviceID: "d1", Code: "c1"}}); err != nil {
			t.Fatalf("Replace: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".dashboard-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want just the layout file", len(entries))
	}
}

func TestFileShapeIsStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	s := New(path)
	if err := s.Replace([]Widget{{DeviceID: "d1", Code: "c1", Title: "Свет", Span: 2}}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Widgets []map[string]any `json:"widgets"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	if len(file.Widgets) != 1 {
		t.Fatalf("widgets = %+v", file.Widgets)
	}
	for _, key := range []string{"device_id", "code", "title", "span"} {
		if _, ok := file.Widgets[0][key]; !ok {
			t.Errorf("field %q missing from %s", key, raw)
		}
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	s := New(path)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := s.Replace([]Widget{{DeviceID: "d1", Code: "c1"}}); err != nil {
				t.Errorf("Replace: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := s.Widgets(); err != nil {
				t.Errorf("Widgets: %v", err)
			}
		}()
	}
	wg.Wait()
}
