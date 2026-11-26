package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WarlockCyber/CoolHome/internal/dashboard"
)

func newDashboardServer(t *testing.T, path string) *httptest.Server {
	t.Helper()
	return newTestServer(t, Options{Demo: true, Dashboard: dashboard.New(path)})
}

func putJSON(t *testing.T, url, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	defer resp.Body.Close()
	var buf strings.Builder
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		t.Fatal(err)
	}
	return resp, []byte(buf.String())
}

func TestDashboardStartsEmpty(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	resp, body := getJSON(t, srv.URL+"/api/dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out dashboardResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(out.Widgets) != 0 {
		t.Errorf("widgets = %+v, want none", out.Widgets)
	}
	if !out.Editable {
		t.Error("a store with a file must report editable")
	}
	if out.MaxWidgets != dashboard.MaxWidgets {
		t.Errorf("max_widgets = %d", out.MaxWidgets)
	}
}

func TestDashboardSavesAndReturnsTheLayout(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))

	body := `{"widgets":[{"device_id":"demo-light-1","code":"switch_led","title":"Свет"},{"device_id":"demo-curtain-1","code":"control","span":2}]}`
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Widgets) != 2 {
		t.Fatalf("widgets = %+v", out.Widgets)
	}
	if out.Widgets[0].DeviceID != "demo-light-1" || out.Widgets[0].Title != "Свет" {
		t.Errorf("first widget = %+v", out.Widgets[0])
	}
	if out.Widgets[1].Span != 2 {
		t.Errorf("span = %d", out.Widgets[1].Span)
	}

	// The layout is served back.
	_, raw = getJSON(t, srv.URL+"/api/dashboard")
	if !strings.Contains(string(raw), "demo-light-1") {
		t.Errorf("saved layout not served: %s", raw)
	}
}

func TestDashboardRejectsABadLayout(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"widgets":[{"code":"no-device"}]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "device_id") {
		t.Errorf("body = %s, want an explanation", raw)
	}
}

func TestDashboardRejectsAnEmptyCode(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	resp, _ := putJSON(t, srv.URL+"/api/dashboard", `{"widgets":[{"device_id":"d1"}]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// A server without a file keeps the layout in memory and says so, rather than
// letting the UI believe a change will survive a restart.
func TestDashboardWithoutAFileIsNotEditable(t *testing.T) {
	srv := newTestServer(t, Options{Demo: true})
	resp, raw := getJSON(t, srv.URL+"/api/dashboard")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Editable {
		t.Error("a store without a file must not report editable")
	}
}

func TestDashboardStartsWithAnEmptyPanel(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	_, raw := getJSON(t, srv.URL+"/api/dashboard")
	if !strings.Contains(string(raw), `"panel":[]`) {
		t.Errorf("body = %s, want an empty panel list, not null", raw)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.MaxPanel != dashboard.MaxPanel || out.MaxPanel == 0 {
		t.Errorf("max_panel = %d, want %d", out.MaxPanel, dashboard.MaxPanel)
	}
}

func TestDashboardSavesThePanel(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	body := `{"widgets":[{"device_id":"demo-light-1","code":"switch_led"}],"panel":["demo-sensor-1","demo-plug-1"]}`
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Panel) != 2 || out.Panel[0] != "demo-sensor-1" || out.Panel[1] != "demo-plug-1" {
		t.Fatalf("panel = %v, want the saved order", out.Panel)
	}
	if len(out.Widgets) != 1 {
		t.Errorf("widgets = %+v, want the remote kept", out.Widgets)
	}

	// Served back after a reread, which is what a reload does.
	_, raw = getJSON(t, srv.URL+"/api/dashboard")
	if !strings.Contains(string(raw), "demo-plug-1") {
		t.Errorf("saved panel not served: %s", raw)
	}
}

func TestDashboardRejectsADuplicatePanelEntry(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"widgets":[],"panel":["d1","d1"]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "twice") {
		t.Errorf("body = %s, want an explanation", raw)
	}
}

func TestDashboardRejectsTooManyPanelCards(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	ids := make([]string, dashboard.MaxPanel+1)
	for i := range ids {
		ids[i] = `"dev-` + string(rune('a'+i)) + `"`
	}
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"widgets":[],"panel":[`+strings.Join(ids, ",")+`]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
	}
}

// Clearing the panel must be possible, and must not touch the remote.
func TestDashboardAcceptsAnEmptyPanel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	srv := newDashboardServer(t, path)
	if resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"widgets":[{"device_id":"d1","code":"c1"}],"panel":["d2"]}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", resp.StatusCode, raw)
	}
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"widgets":[{"device_id":"d1","code":"c1"}],"panel":[]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Panel) != 0 {
		t.Errorf("panel = %v, want cleared", out.Panel)
	}
	if len(out.Widgets) != 1 {
		t.Errorf("widgets = %+v, want kept", out.Widgets)
	}
}

// A client that predates the panel — an old page, or a script that only edits
// the remote — sends widgets and nothing else. That must not wipe the panel:
// two lists edited from different places share one document, and a missing key
// is not a request to empty it. Sending "panel": [] still clears it.
func TestDashboardKeepsPanelWhenTheClientOmitsIt(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	if resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"widgets":[{"device_id":"d1","code":"c1"}],"panel":["thermo-1"]}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", resp.StatusCode, raw)
	}

	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"widgets":[{"device_id":"d1","code":"c1"},{"device_id":"d2","code":"c2"}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Panel) != 1 || out.Panel[0] != "thermo-1" {
		t.Errorf("panel = %v, want it kept when the key is absent", out.Panel)
	}
	if len(out.Widgets) != 2 {
		t.Errorf("widgets = %+v, want the new list", out.Widgets)
	}
}

// The remote and the panel are edited from different places, possibly on
// different devices. Sending only the list that changed must leave the other
// one alone: otherwise a phone adding a card to the panel would be undone by a
// tablet that only reordered the remote.
func TestDashboardSavesOneListWithoutTouchingTheOther(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	setup := `{"widgets":[{"device_id":"d1","code":"c1"}],"panel":["thermo-1"]}`
	if resp, raw := putJSON(t, srv.URL+"/api/dashboard", setup); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", resp.StatusCode, raw)
	}

	// Only the panel: the remote must not be touched.
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"panel":["thermo-1","sensor-1"]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("panel-only: %d %s", resp.StatusCode, raw)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Widgets) != 1 || out.Widgets[0].DeviceID != "d1" {
		t.Errorf("widgets = %+v, want the remote kept", out.Widgets)
	}
	if len(out.Panel) != 2 {
		t.Errorf("panel = %v, want the new list", out.Panel)
	}

	// Only the widgets: the panel must survive, which is the case two tabs hit.
	resp, raw = putJSON(t, srv.URL+"/api/dashboard", `{"widgets":[{"device_id":"d2","code":"c2"}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("widgets-only: %d %s", resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Panel) != 2 || out.Panel[1] != "sensor-1" {
		t.Errorf("panel = %v, want it kept when only widgets are sent", out.Panel)
	}
	if len(out.Widgets) != 1 || out.Widgets[0].DeviceID != "d2" {
		t.Errorf("widgets = %+v, want the new list", out.Widgets)
	}
}

func TestDashboardRejectsARequestWithNeitherList(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "widgets") {
		t.Errorf("body = %s, want an explanation", raw)
	}
}

// A local name renames the device in every answer that carries it — the list,
// the single device and the echo after a command — because a page and a script
// must not disagree about what a device is called.
func TestLocalNamesRenameDevicesEverywhere(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))

	// The demo home has a thermostat; name it locally.
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"names":{"demo-thermo-1":"Кухня"}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("names: %d %s", resp.StatusCode, raw)
	}
	var board dashboardResponse
	if err := json.Unmarshal(raw, &board); err != nil {
		t.Fatal(err)
	}
	if board.Names["demo-thermo-1"] != "Кухня" {
		t.Fatalf("names = %v", board.Names)
	}
	if board.MaxNameLen != 60 {
		t.Errorf("max_name_len = %d, want 60", board.MaxNameLen)
	}

	_, list := getJSON(t, srv.URL+"/api/devices")
	var snap struct {
		Devices []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(list, &snap); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range snap.Devices {
		if d.ID == "demo-thermo-1" {
			found = true
			if d.Name != "Кухня" {
				t.Errorf("list name = %q, want the local name", d.Name)
			}
		}
	}
	if !found {
		t.Fatal("the demo thermostat is missing from the snapshot")
	}

	_, single := getJSON(t, srv.URL+"/api/devices/demo-thermo-1")
	if !strings.Contains(string(single), `"name":"Кухня"`) {
		t.Errorf("single device = %s, want the local name", single)
	}

	// Clearing the name brings the cloud's own back.
	if resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"names":{}}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("clear: %d %s", resp.StatusCode, raw)
	}
	_, single = getJSON(t, srv.URL+"/api/devices/demo-thermo-1")
	if strings.Contains(string(single), `"name":"Кухня"`) {
		t.Errorf("the local name survived its removal: %s", single)
	}
}

func TestLocalNamesRejectALongName(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	long := strings.Repeat("я", 61)
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"names":{"demo-thermo-1":"`+long+`"}}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "60") {
		t.Errorf("body = %s, want the limit named", raw)
	}
}

// Saving names alone must not disturb the remote or the panel: the three lists
// are edited from different places.
func TestSavingNamesLeavesLayoutAlone(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	if resp, raw := putJSON(t, srv.URL+"/api/dashboard",
		`{"widgets":[{"device_id":"d1","code":"c1"}],"panel":["p1"]}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", resp.StatusCode, raw)
	}
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"names":{"d1":"Свет"}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("names: %d %s", resp.StatusCode, raw)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Widgets) != 1 || out.Widgets[0].DeviceID != "d1" {
		t.Errorf("widgets = %+v", out.Widgets)
	}
	if len(out.Panel) != 1 || out.Panel[0] != "p1" {
		t.Errorf("panel = %v", out.Panel)
	}
	if out.Names["d1"] != "Свет" {
		t.Errorf("names = %v", out.Names)
	}
}

// A scene's description is local text about what the scene does. It travels
// with the layout, and saving it must not disturb the remote, the panel or the
// names.
func TestDashboardSavesSceneDescription(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	if resp, raw := putJSON(t, srv.URL+"/api/dashboard",
		`{"widgets":[{"device_id":"d1","code":"c1"}],"panel":["p1"],"names":{"d1":"Кухня"}}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", resp.StatusCode, raw)
	}

	resp, raw := putJSON(t, srv.URL+"/api/dashboard",
		`{"descriptions":{"scene-1":"  Выключает свет в зале и на кухне  "}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("descriptions: %d %s", resp.StatusCode, raw)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Descriptions["scene-1"] != "Выключает свет в зале и на кухне" {
		t.Errorf("description = %q, want it trimmed", out.Descriptions["scene-1"])
	}
	if out.MaxDescLen != 280 {
		t.Errorf("max_desc_len = %d, want 280", out.MaxDescLen)
	}
	// The rest is untouched.
	if len(out.Widgets) != 1 || len(out.Panel) != 1 || out.Names["d1"] != "Кухня" {
		t.Errorf("saving a description disturbed the rest: %+v", out)
	}

	// Served back after a reread, which is what a reload does.
	_, raw = getJSON(t, srv.URL+"/api/dashboard")
	if !strings.Contains(string(raw), "Выключает свет") {
		t.Errorf("the description was not served back: %s", raw)
	}
}

func TestDashboardRejectsATooLongDescription(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	long := strings.Repeat("я", 281)
	resp, raw := putJSON(t, srv.URL+"/api/dashboard",
		`{"descriptions":{"scene-1":"`+long+`"}}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "280") {
		t.Errorf("body = %s, want the limit named", raw)
	}
}

// Clearing a description is an empty string, and it must not clear anything else.
func TestDashboardClearsADescription(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	if resp, raw := putJSON(t, srv.URL+"/api/dashboard",
		`{"names":{"d1":"Кухня"},"descriptions":{"scene-1":"было"}}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", resp.StatusCode, raw)
	}
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"descriptions":{}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Descriptions) != 0 {
		t.Errorf("descriptions = %v, want cleared", out.Descriptions)
	}
	if out.Names["d1"] != "Кухня" {
		t.Errorf("clearing a description lost a name: %v", out.Names)
	}
}

// Hiding a scene is the substitute for deleting one: the API cannot remove a
// scene made in the Tuya app, so the page hides it locally. Saving the hidden
// list must not disturb anything else, and an empty list shows everything again.
func TestDashboardHidesAndShowsScenes(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	if resp, raw := putJSON(t, srv.URL+"/api/dashboard",
		`{"names":{"d1":"Кухня"},"descriptions":{"scene-1":"что делает"}}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", resp.StatusCode, raw)
	}

	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"hidden":["scene-1","scene-2"]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hidden: %d %s", resp.StatusCode, raw)
	}
	var out dashboardResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Hidden) != 2 || out.Hidden[0] != "scene-1" {
		t.Fatalf("hidden = %v", out.Hidden)
	}
	if out.Names["d1"] != "Кухня" || out.Descriptions["scene-1"] != "что делает" {
		t.Errorf("hiding disturbed the rest: %+v", out)
	}

	// Showing everything again.
	resp, raw = putJSON(t, srv.URL+"/api/dashboard", `{"hidden":[]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("show: %d %s", resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Hidden) != 0 {
		t.Errorf("hidden = %v, want none", out.Hidden)
	}
}

func TestDashboardRejectsADuplicateHiddenEntry(t *testing.T) {
	srv := newDashboardServer(t, filepath.Join(t.TempDir(), "d.json"))
	resp, raw := putJSON(t, srv.URL+"/api/dashboard", `{"hidden":["s1","s1"]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "twice") {
		t.Errorf("body = %s, want an explanation", raw)
	}
}
