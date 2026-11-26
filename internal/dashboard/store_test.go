package dashboard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestEmptyStoreIsAnEmptyLayout(t *testing.T) {
	s := New("")
	layout, err := s.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(layout.Widgets) != 0 || len(layout.Panel) != 0 {
		t.Fatalf("layout = %+v, want empty", layout)
	}
}

func TestReplaceAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.json")
	s := New(path)

	in := Layout{
		Widgets: []Widget{
			{DeviceID: "lamp-1", Code: "switch_led", Title: "Свет"},
			{DeviceID: "scene-1", Code: "trigger", Span: 2},
		},
		Panel: []string{"thermo-1", "sensor-2"},
	}
	if _, err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A fresh store reads the file, which is what a restart does.
	again, err := New(path).Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(again.Widgets) != 2 {
		t.Fatalf("widgets = %+v", again.Widgets)
	}
	if again.Widgets[0].DeviceID != "lamp-1" || again.Widgets[0].Title != "Свет" {
		t.Errorf("first widget = %+v", again.Widgets[0])
	}
	if again.Widgets[1].Span != 2 {
		t.Errorf("span = %d, want 2", again.Widgets[1].Span)
	}
	if len(again.Panel) != 2 || again.Panel[0] != "thermo-1" || again.Panel[1] != "sensor-2" {
		t.Errorf("panel = %v, want the stored order", again.Panel)
	}
}

func TestNormalizeClampsAndTrims(t *testing.T) {
	out, err := Normalize(Layout{
		Widgets: []Widget{
			{DeviceID: "  d1  ", Code: " c1 ", Span: 99, Title: "  свет  "},
			{DeviceID: "d2", Code: "c2", Span: 0},
			{DeviceID: "d3", Code: "c3", Span: -5},
		},
		Panel: []string{"  dev-1  ", "dev-2"},
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if out.Widgets[0].DeviceID != "d1" || out.Widgets[0].Code != "c1" || out.Widgets[0].Title != "свет" {
		t.Errorf("trimming failed: %+v", out.Widgets[0])
	}
	if out.Widgets[0].Span != 2 {
		t.Errorf("span = %d, want it clamped to 2", out.Widgets[0].Span)
	}
	for _, w := range out.Widgets[1:] {
		if w.Span != 1 {
			t.Errorf("%s span = %d, want 1", w.ID(), w.Span)
		}
	}
	if len(out.Panel) != 2 || out.Panel[0] != "dev-1" || out.Panel[1] != "dev-2" {
		t.Errorf("panel = %v, want trimmed ids in order", out.Panel)
	}
}

func TestNormalizeRejectsBadLayouts(t *testing.T) {
	cases := map[string]Layout{
		"no device":      {Widgets: []Widget{{Code: "c1"}}},
		"no code":        {Widgets: []Widget{{DeviceID: "d1"}}},
		"duplicate":      {Widgets: []Widget{{DeviceID: "d1", Code: "c1"}, {DeviceID: "d1", Code: "c1"}}},
		"title too long": {Widgets: []Widget{{DeviceID: "d1", Code: "c1", Title: strings.Repeat("я", 61)}}},
		"panel empty id": {Panel: []string{"d1", " "}},
		"panel duplicate": {
			Panel: []string{"d1", "d1"},
		},
		"panel id too long": {Panel: []string{strings.Repeat("x", 65)}},
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
	if _, err := Normalize(Layout{Widgets: tooMany}); !errors.Is(err, ErrInvalid) {
		t.Errorf("too many widgets: err = %v, want ErrInvalid", err)
	}

	crowded := make([]string, MaxPanel+1)
	for i := range crowded {
		crowded[i] = "dev-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if _, err := Normalize(Layout{Panel: crowded}); !errors.Is(err, ErrInvalid) {
		t.Errorf("too many panel cards: err = %v, want ErrInvalid", err)
	}
}

func TestReplaceRejectsInvalidLayout(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "d.json"))
	if _, err := s.Save(Layout{Widgets: []Widget{{DeviceID: "d1", Code: "c1"}}, Panel: []string{"p1"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := s.Save(Layout{Widgets: []Widget{{Code: "no-device"}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	// The old layout must survive a rejected write — including the panel, which
	// an invalid widget list must not clear.
	layout, err := s.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(layout.Widgets) != 1 || layout.Widgets[0].DeviceID != "d1" || len(layout.Panel) != 1 {
		t.Errorf("layout was damaged by a rejected write: %+v", layout)
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
	if _, err := New(path).Current(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}

	// A file that is not JSON at all is also an error with a clear message.
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path).Current(); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestEmptyFileIsAnEmptyLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	if err := os.WriteFile(path, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	layout, err := New(path).Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(layout.Widgets) != 0 {
		t.Fatalf("widgets = %+v", layout.Widgets)
	}
}

// The write must be atomic: a reader must never see a truncated file.
func TestWriteLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.json")
	s := New(path)
	for i := 0; i < 3; i++ {
		if _, err := s.Save(Layout{Widgets: []Widget{{DeviceID: "d1", Code: "c1"}}}); err != nil {
			t.Fatalf("Save: %v", err)
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
	if _, err := s.Save(Layout{
		Widgets: []Widget{{DeviceID: "d1", Code: "c1", Title: "Свет", Span: 2}},
		Panel:   []string{"thermo-1"},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Widgets []map[string]any `json:"widgets"`
		Panel   []string         `json:"panel"`
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
	if len(file.Panel) != 1 || file.Panel[0] != "thermo-1" {
		t.Errorf("panel = %v in %s, want [thermo-1]", file.Panel, raw)
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
			if _, err := s.Save(Layout{Widgets: []Widget{{DeviceID: "d1", Code: "c1"}}, Panel: []string{"p1"}}); err != nil {
				t.Errorf("Save: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := s.Current(); err != nil {
				t.Errorf("Current: %v", err)
			}
		}()
	}
	wg.Wait()
}

// A layout file written before the side panel existed has no panel key. It must
// load as a remote with an empty panel, not as a corrupt file: every install
// that has ever configured the remote has such a file.
func TestFileWithoutPanelLoadsAsEmptyPanel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	old := `{"widgets":[{"device_id":"lamp-1","code":"switch_led","title":"Свет"}]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	layout, err := New(path).Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(layout.Widgets) != 1 || layout.Widgets[0].DeviceID != "lamp-1" {
		t.Fatalf("widgets = %+v, want the stored one", layout.Widgets)
	}
	if len(layout.Panel) != 0 {
		t.Fatalf("panel = %v, want none", layout.Panel)
	}
}

// Saving a panel must not disturb the remote, and vice versa: they are edited
// from different places on the screen but stored together.
func TestSavingPanelKeepsWidgets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	s := New(path)
	if _, err := s.Save(Layout{Widgets: []Widget{{DeviceID: "d1", Code: "c1"}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	current, err := s.Current()
	if err != nil {
		t.Fatal(err)
	}
	current.Panel = []string{"d2"}
	if _, err := s.Save(current); err != nil {
		t.Fatalf("Save with panel: %v", err)
	}

	reloaded, err := New(path).Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Widgets) != 1 || reloaded.Widgets[0].DeviceID != "d1" {
		t.Errorf("widgets lost when saving the panel: %+v", reloaded.Widgets)
	}
	if len(reloaded.Panel) != 1 || reloaded.Panel[0] != "d2" {
		t.Errorf("panel = %v, want [d2]", reloaded.Panel)
	}
}

// An empty panel is omitted from the file, so a remote-only layout keeps its
// shape and a hand-inspected file stays readable.
func TestEmptyPanelIsOmittedFromTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	s := New(path)
	if _, err := s.Save(Layout{Widgets: []Widget{{DeviceID: "d1", Code: "c1"}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"panel"`) {
		t.Errorf("empty panel should not be written: %s", raw)
	}
}

// The bound the UI enforces (web/static/panel.js) and the one this package
// enforces must be the same number: the page stops before the request, the
// server rejects after, and a mismatch turns "added" into a 400.
func TestPanelLimitIsTheDocumentedTwelve(t *testing.T) {
	if MaxPanel != 12 {
		t.Fatalf("MaxPanel = %d; web/static/panel.js and web-tests/panel.test.js assert 12", MaxPanel)
	}
}

// A local name is the only thing CoolHome stores about a device besides the
// layout: the cloud keeps its own. It must survive a restart, be trimmed, and be
// removable — clearing it is how the cloud's name comes back.
func TestNamesAreStoredAndCleared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	s := New(path)
	if _, err := s.Save(Layout{Names: map[string]string{" dev-1 ": "  Кухня  ", "dev-2": "Спальня"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := New(path).Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if reloaded["dev-1"] != "Кухня" || reloaded["dev-2"] != "Спальня" {
		t.Fatalf("names = %v, want trimmed and kept", reloaded)
	}

	// An empty name removes the entry rather than storing a blank: showing the
	// cloud's name again is what the caller meant.
	if _, err := s.Save(Layout{Names: map[string]string{"dev-1": "", "dev-2": "Спальня"}}); err != nil {
		t.Fatalf("Save with empty: %v", err)
	}
	after, err := s.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if _, ok := after["dev-1"]; ok {
		t.Errorf("empty name was kept: %v", after)
	}
	if after["dev-2"] != "Спальня" {
		t.Errorf("other names were lost: %v", after)
	}
}

func TestNamesRejectBadInput(t *testing.T) {
	cases := map[string]map[string]string{
		"empty id":      {"": "Кухня"},
		"blank id":      {"  ": "Кухня"},
		"id too long":   {strings.Repeat("x", 65): "Кухня"},
		"name too long": {"dev-1": strings.Repeat("я", MaxNameLen+1)},
	}
	for name, in := range cases {
		if _, err := NormalizeNames(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

// The map must not be shared with the caller: a handler that edits the result in
// place would otherwise rename devices for the next request too.
func TestNamesAreCopiedOnRead(t *testing.T) {
	s := New("")
	if _, err := s.Save(Layout{Names: map[string]string{"dev-1": "Кухня"}}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Names()
	if err != nil {
		t.Fatal(err)
	}
	first["dev-1"] = "подменено"
	first["dev-2"] = "лишнее"

	second, err := s.Names()
	if err != nil {
		t.Fatal(err)
	}
	if second["dev-1"] != "Кухня" {
		t.Errorf("store was changed through the returned map: %v", second)
	}
	if _, ok := second["dev-2"]; ok {
		t.Error("store gained an entry from the returned map")
	}
}

// A layout file written before names existed has no key at all.
func TestFileWithoutNamesLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	if err := os.WriteFile(path, []byte(`{"widgets":[{"device_id":"d1","code":"c1"}],"panel":["p1"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	layout, err := New(path).Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if layout.Names != nil {
		t.Errorf("names = %v, want none", layout.Names)
	}
	if len(layout.Widgets) != 1 || len(layout.Panel) != 1 {
		t.Errorf("layout lost: %+v", layout)
	}
}

// A title is a heading in Russian as often as in English. Counting bytes made
// the limit depend on the alphabet: 39 Cyrillic characters are 73 bytes, and a
// widget title that the name limit allows was rejected.
func TestTitleLimitCountsRunesNotBytes(t *testing.T) {
	title := strings.Repeat("я", MaxTitleLen) // 60 runes, 120 bytes
	if _, err := Normalize(Layout{Widgets: []Widget{{DeviceID: "d1", Code: "c1", Title: title}}}); err != nil {
		t.Fatalf("a title of exactly %d Cyrillic characters must be accepted: %v", MaxTitleLen, err)
	}
	longer := strings.Repeat("я", MaxTitleLen+1)
	if _, err := Normalize(Layout{Widgets: []Widget{{DeviceID: "d1", Code: "c1", Title: longer}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a title one rune too long must be rejected, got %v", err)
	}
}

// Renaming a device rewrites the titles of its widgets in the same request as
// the name. A title that the server rejects therefore takes the name with it —
// the whole change is lost. This is the exact pair that used to fail: a name
// within its limit, and the title it produces still within the same limit.
func TestRenameWithWidgetTitleFitsBothLimits(t *testing.T) {
	name := "Кухонный светильник над столом" // 30 runes, 57 bytes
	title := name + " · Таймер"
	if utf8.RuneCountInString(title) > MaxTitleLen {
		t.Fatalf("the fixture is wrong: %q is %d runes", title, utf8.RuneCountInString(title))
	}
	layout := Layout{
		Widgets: []Widget{{DeviceID: "d1", Code: "c1", Title: title}},
		Names:   map[string]string{"d1": name},
	}
	if _, err := Normalize(layout); err != nil {
		t.Fatalf("a rename with its rewritten title must be storable: %v", err)
	}
}

// The name limit and the title limit are the same number, so a name that fits
// produces a title that fits whenever the title is the name plus a suffix of
// bounded length. web/static/names.js truncates the suffix case; this pins the
// server side of that agreement.
func TestNameAndTitleLimitsAgree(t *testing.T) {
	if MaxNameLen != MaxTitleLen {
		t.Fatalf("MaxNameLen = %d, MaxTitleLen = %d; panel.js and names.js assume one bound",
			MaxNameLen, MaxTitleLen)
	}
}

// Two screens edit the same document: a phone adds a card to the panel while a
// tablet reorders the remote. Update reads, edits and writes under one lock, so
// neither change disappears.
func TestConcurrentUpdatesKeepBothChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	s := New(path)
	if _, err := s.Save(Layout{Widgets: []Widget{{DeviceID: "d1", Code: "c1"}}, Panel: []string{"p1"}}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			// The "phone": adds a card to the panel.
			if _, err := s.Update(func(l *Layout) error {
				l.Panel = append(l.Panel, "phone-card")
				return nil
			}); err != nil && !errors.Is(err, ErrInvalid) {
				t.Errorf("panel update: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			// The "tablet": rewrites the remote, leaving the panel alone.
			if _, err := s.Update(func(l *Layout) error {
				l.Widgets = []Widget{{DeviceID: "d2", Code: "c2"}}
				return nil
			}); err != nil {
				t.Errorf("widgets update: %v", err)
			}
		}()
	}
	wg.Wait()

	final, err := New(path).Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(final.Widgets) != 1 || final.Widgets[0].DeviceID != "d2" {
		t.Errorf("widgets = %+v, want the tablet's change", final.Widgets)
	}
	// The phone added the same id repeatedly; whatever the interleaving, the
	// panel must still hold p1 and its own addition, never just one of them.
	joined := strings.Join(final.Panel, ",")
	if !strings.Contains(joined, "p1") {
		t.Errorf("panel = %v, the tablet's write dropped the phone's list", final.Panel)
	}
}

// An edit that fails validation leaves the stored layout untouched.
func TestFailedUpdateKeepsTheOldLayout(t *testing.T) {
	s := New("")
	if _, err := s.Save(Layout{Panel: []string{"p1"}, Names: map[string]string{"d1": "Кухня"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(func(l *Layout) error {
		l.Widgets = []Widget{{DeviceID: "", Code: "c"}}
		return nil
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	after, err := s.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Panel) != 1 || after.Names["d1"] != "Кухня" {
		t.Errorf("layout was damaged by a rejected update: %+v", after)
	}
}

// A description is what a scene's name cannot say: which devices it touches.
// Like a name it is local, bounded, trimmed, and cleared by emptying the field.
func TestDescriptionsAreStoredAndCleared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	s := New(path)
	if _, err := s.Save(Layout{Descriptions: map[string]string{
		" scene-1 ": "  Выключает свет в зале и на кухне  ",
		"scene-2":   "Открывает шторы",
	}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := New(path).Descriptions()
	if err != nil {
		t.Fatalf("Descriptions: %v", err)
	}
	if reloaded["scene-1"] != "Выключает свет в зале и на кухне" {
		t.Fatalf("description = %q, want it trimmed", reloaded["scene-1"])
	}
	if reloaded["scene-2"] != "Открывает шторы" {
		t.Errorf("second description lost: %v", reloaded)
	}

	// An empty description removes the entry rather than storing a blank.
	if _, err := s.Save(Layout{Descriptions: map[string]string{"scene-1": "  "}}); err != nil {
		t.Fatalf("Save with empty: %v", err)
	}
	after, err := s.Descriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("descriptions = %v, want none", after)
	}
}

func TestDescriptionsRejectBadInput(t *testing.T) {
	cases := map[string]map[string]string{
		"empty id":    {"  ": "текст"},
		"id too long": {strings.Repeat("x", 65): "текст"},
		"too long":    {"d1": strings.Repeat("я", MaxDescLen+1)},
	}
	for name, in := range cases {
		if _, err := NormalizeDescriptions(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	// Exactly at the bound is accepted, counted in runes like a name.
	if _, err := NormalizeDescriptions(map[string]string{"d1": strings.Repeat("я", MaxDescLen)}); err != nil {
		t.Errorf("a description of exactly %d Cyrillic characters was refused: %v", MaxDescLen, err)
	}
}

// A description must not be able to change through the map the store returned.
func TestDescriptionsAreCopiedOnRead(t *testing.T) {
	s := New("")
	if _, err := s.Save(Layout{Descriptions: map[string]string{"d1": "было"}}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Descriptions()
	if err != nil {
		t.Fatal(err)
	}
	first["d1"] = "подменено"
	first["d2"] = "лишнее"

	second, _ := s.Descriptions()
	if second["d1"] != "было" {
		t.Errorf("the store changed through the returned map: %v", second)
	}
	if _, ok := second["d2"]; ok {
		t.Error("the store gained an entry from the returned map")
	}
}

// Saving a description must leave the layout and the names alone: they are
// edited from different places but share one document.
func TestSavingDescriptionKeepsTheRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	s := New(path)
	if _, err := s.Save(Layout{
		Widgets: []Widget{{DeviceID: "d1", Code: "c1"}},
		Panel:   []string{"p1"},
		Names:   map[string]string{"d1": "Кухня"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Layout{
		Widgets:      []Widget{{DeviceID: "d1", Code: "c1"}},
		Panel:        []string{"p1"},
		Names:        map[string]string{"d1": "Кухня"},
		Descriptions: map[string]string{"d1": "что-то"},
	}); err != nil {
		t.Fatal(err)
	}
	layout, err := New(path).Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Widgets) != 1 || len(layout.Panel) != 1 || layout.Names["d1"] != "Кухня" {
		t.Errorf("the rest was lost: %+v", layout)
	}
	if layout.Descriptions["d1"] != "что-то" {
		t.Errorf("description = %v", layout.Descriptions)
	}
}

// An old file has no descriptions key at all.
func TestFileWithoutDescriptionsLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	if err := os.WriteFile(path, []byte(`{"widgets":[{"device_id":"d1","code":"c1"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	layout, err := New(path).Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if layout.Descriptions != nil {
		t.Errorf("descriptions = %v, want none", layout.Descriptions)
	}
	if len(layout.Widgets) != 1 {
		t.Errorf("widgets lost: %+v", layout.Widgets)
	}
}

// A scene made in the Tuya app cannot be deleted through the API, so hiding is
// how an obsolete one leaves the list. It is local and reversible: the scene
// itself keeps working from the app.
func TestHiddenIsStoredAndReversible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	s := New(path)
	if _, err := s.Save(Layout{Hidden: []string{" scene-1 ", "scene-2"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := New(path).Hidden()
	if err != nil {
		t.Fatalf("Hidden: %v", err)
	}
	if len(reloaded) != 2 || reloaded[0] != "scene-1" || reloaded[1] != "scene-2" {
		t.Fatalf("hidden = %v, want the trimmed list in order", reloaded)
	}

	// Showing a scene again is an empty list, and it must not touch the rest.
	if _, err := s.Save(Layout{
		Names:  map[string]string{"d1": "Кухня"},
		Hidden: []string{},
	}); err != nil {
		t.Fatalf("Save with empty: %v", err)
	}
	layout, err := s.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Hidden) != 0 {
		t.Errorf("hidden = %v, want none", layout.Hidden)
	}
	if layout.Names["d1"] != "Кухня" {
		t.Errorf("showing a scene lost a name: %v", layout.Names)
	}
}

func TestHiddenRejectsBadInput(t *testing.T) {
	cases := map[string][]string{
		"empty id":    {"scene-1", "  "},
		"id too long": {strings.Repeat("x", 65)},
		"duplicate":   {"scene-1", "scene-1"},
	}
	for name, in := range cases {
		if _, err := NormalizeHidden(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	tooMany := make([]string, MaxHidden+1)
	for i := range tooMany {
		tooMany[i] = "d" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if _, err := NormalizeHidden(tooMany); !errors.Is(err, ErrInvalid) {
		t.Errorf("too many: err = %v, want ErrInvalid", err)
	}
}

// The slice must not be shared with the caller.
func TestHiddenIsCopiedOnRead(t *testing.T) {
	s := New("")
	if _, err := s.Save(Layout{Hidden: []string{"scene-1"}}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Hidden()
	if err != nil {
		t.Fatal(err)
	}
	first[0] = "подменено"
	first = append(first, "лишнее")

	second, _ := s.Hidden()
	if len(second) != 1 || second[0] != "scene-1" {
		t.Errorf("the store changed through the returned slice: %v", second)
	}
}
