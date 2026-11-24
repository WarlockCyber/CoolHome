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
