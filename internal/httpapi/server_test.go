package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/demo"
	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

func newTestServer(t *testing.T, opts Options) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := home.New(demo.New(), nil, time.Minute, logger)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	opts.Logger = logger
	handler, err := New(svc, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func getJSON(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp, body
}

func postJSON(t *testing.T, url, body string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp, raw
}

func TestDevicesEndpoint(t *testing.T) {
	srv := newTestServer(t, Options{Demo: true})
	resp, body := getJSON(t, srv.URL+"/api/devices")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type = %q", ct)
	}
	var snap snapshotResponse
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(snap.Devices) != 6 {
		t.Fatalf("devices = %d, want 6", len(snap.Devices))
	}
	if !snap.Demo {
		t.Error("demo flag not set")
	}
	if snap.LastSync == nil {
		t.Error("last_sync missing")
	}

	var light *deviceDTO
	for i := range snap.Devices {
		if snap.Devices[i].ID == "demo-light-1" {
			light = &snap.Devices[i]
		}
	}
	if light == nil {
		t.Fatal("demo light missing from response")
	}
	if len(light.Functions) != 3 {
		t.Fatalf("light functions = %d, want 3", len(light.Functions))
	}
	bright := light.Functions[1]
	if bright.Code != "bright_value" || bright.Min == nil || *bright.Min != 10 || bright.Max == nil || *bright.Max != 1000 {
		t.Fatalf("brightness domain not exposed: %+v", bright)
	}
	if len(light.Readings) == 0 {
		t.Fatal("readings missing")
	}
}

func TestCommandEndpoint(t *testing.T) {
	srv := newTestServer(t, Options{})

	resp, body := postJSON(t, srv.URL+"/api/devices/demo-light-1/commands",
		`{"commands":[{"code":"switch_led","value":false}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out struct {
		Device deviceDTO `json:"device"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, s := range out.Device.Status {
		if s.Code == "switch_led" {
			found = true
			if s.Value != false {
				t.Fatalf("switch_led = %v, want false", s.Value)
			}
		}
	}
	if !found {
		t.Fatalf("switch_led missing from response: %s", body)
	}
}

func TestCommandEndpointRejectsBadInput(t *testing.T) {
	srv := newTestServer(t, Options{})
	base := srv.URL + "/api/devices/demo-light-1/commands"

	cases := []struct {
		name string
		url  string
		body string
		want int
	}{
		{"unknown function", base, `{"commands":[{"code":"nope","value":true}]}`, http.StatusBadRequest},
		{"out of range", base, `{"commands":[{"code":"bright_value","value":4000}]}`, http.StatusBadRequest},
		{"bad json", base, `{"commands":`, http.StatusBadRequest},
		{"unknown field", base, `{"commands":[],"extra":1}`, http.StatusBadRequest},
		{"unknown device", srv.URL + "/api/devices/nope/commands", `{"commands":[{"code":"switch_1","value":true}]}`, http.StatusNotFound},
		{"offline device", srv.URL + "/api/devices/demo-humidifier-1/commands", `{"commands":[{"code":"switch_1","value":true}]}`, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := postJSON(t, tc.url, tc.body)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, tc.want, body)
			}
			var payload map[string]string
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("error body is not JSON: %s", body)
			}
			if payload["error"] == "" {
				t.Fatalf("error message missing: %s", body)
			}
		})
	}
}

func TestDeviceEndpointNotFound(t *testing.T) {
	srv := newTestServer(t, Options{})
	resp, body := getJSON(t, srv.URL+"/api/devices/nope")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestIndexAndStaticAssets(t *testing.T) {
	srv := newTestServer(t, Options{Demo: true})

	resp, body := getJSON(t, srv.URL+"/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d", resp.StatusCode)
	}
	page := string(body)
	for _, needle := range []string{"CoolHome", "/static/app.js", "демо"} {
		if !strings.Contains(page, needle) {
			t.Errorf("index page is missing %q", needle)
		}
	}
	if csp := resp.Header.Get("Content-Security-Policy"); csp == "" {
		t.Error("CSP header missing")
	}

	resp, body = getJSON(t, srv.URL+"/static/app.js")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("app.js status = %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("data-action")) {
		t.Error("app.js looks wrong")
	}
}

func TestAPIRequiresToken(t *testing.T) {
	srv := newTestServer(t, Options{AuthToken: "s3cret"})

	resp, _ := getJSON(t, srv.URL+"/api/devices")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/devices", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer s3cret")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", resp2.StatusCode)
	}

	// The shell stays public so the UI can prompt for the token.
	resp3, _ := getJSON(t, srv.URL+"/")
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d, want 200", resp3.StatusCode)
	}
}

func TestHealthAndRefresh(t *testing.T) {
	srv := newTestServer(t, Options{})

	resp, body := getJSON(t, srv.URL+"/api/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, body = %s", resp.StatusCode, body)
	}
	var health map[string]any
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatal(err)
	}
	if health["ok"] != true {
		t.Fatalf("health = %s", body)
	}

	resp, body = postJSON(t, srv.URL+"/api/refresh", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", resp.StatusCode, body)
	}
	var snap snapshotResponse
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Devices) != 6 {
		t.Fatalf("devices after refresh = %d", len(snap.Devices))
	}
}

// specRejecting returns the demo home but refuses to describe its devices the
// way the cloud refuses an RF switch behind an IR blaster.
type specRejecting struct {
	home.Provider
	err error
}

func (s specRejecting) Spec(context.Context, string) (tuya.Spec, error) {
	return tuya.Spec{}, s.err
}

func newServerWithProvider(t *testing.T, prov home.Provider) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := home.New(prov, nil, time.Minute, logger)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	handler, err := New(svc, Options{Logger: logger, Demo: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestDevicesReportUnsupportedSpec(t *testing.T) {
	srv := newServerWithProvider(t, specRejecting{
		Provider: demo.New(),
		err:      &tuya.APIError{Code: 2009, Msg: "not support this device", TID: "t1"},
	})
	resp, body := getJSON(t, srv.URL+"/api/devices")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var snap snapshotResponse
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snap.Devices) == 0 {
		t.Fatal("no devices")
	}
	for _, d := range snap.Devices {
		if !d.SpecUnsupported {
			t.Errorf("%s: spec_unsupported = false, want true", d.ID)
		}
		if len(d.Functions) != 0 || len(d.Readings) != 0 {
			t.Errorf("%s: expected no points, got %d functions and %d readings", d.ID, len(d.Functions), len(d.Readings))
		}
	}
}

func TestDevicesOmitSpecUnsupportedForNormalDevice(t *testing.T) {
	srv := newTestServer(t, Options{Demo: true})
	_, body := getJSON(t, srv.URL+"/api/devices")
	if strings.Contains(string(body), "spec_unsupported") {
		t.Error("spec_unsupported must be omitted for a device with a thing model")
	}
}

// buttonProvider serves one infrared remote: a device with button functions and
// no state.
type buttonProvider struct {
	home.Provider
}

func (buttonProvider) ListDevices(context.Context, []string) ([]tuya.Device, error) {
	return []tuya.Device{{ID: "ir:blast:1", Name: "Телевизор", Category: "ir_remote", Online: true}}, nil
}

func (buttonProvider) Device(context.Context, string) (tuya.Device, error) {
	return tuya.Device{ID: "ir:blast:1", Name: "Телевизор", Category: "ir_remote", Online: true}, nil
}

func (buttonProvider) Spec(context.Context, string) (tuya.Spec, error) {
	return tuya.Spec{
		Category: "ir_remote",
		Functions: []tuya.Function{
			{Code: "power", Type: tuya.FunctionTypeButton, Name: "Питание"},
		},
	}, nil
}

func (buttonProvider) Command(_ context.Context, _ string, cmds []tuya.Command) error {
	if err := home.ValidateCommand(cmds[0], &tuya.Spec{
		Functions: []tuya.Function{{Code: "power", Type: tuya.FunctionTypeButton}},
	}); err != nil {
		return err
	}
	return nil
}

func TestSnapshotReportsWhetherIRIsServed(t *testing.T) {
	srv := newServerWithProvider(t, buttonProvider{Provider: demo.New()})
	_, body := getJSON(t, srv.URL+"/api/devices")
	if strings.Contains(string(body), `"ir":true`) {
		t.Error("ir must be absent when the provider is off")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := home.New(buttonProvider{Provider: demo.New()}, nil, time.Minute, logger)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	handler, err := New(svc, Options{Logger: logger, IR: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	irSrv := httptest.NewServer(handler)
	defer irSrv.Close()

	_, body = getJSON(t, irSrv.URL+"/api/devices")
	if !strings.Contains(string(body), `"ir":true`) {
		t.Errorf("the snapshot must carry ir=true so the UI can explain a blaster, got %s", body)
	}

	// The single-device view carries it too.
	_, body = getJSON(t, irSrv.URL+"/api/devices/ir:blast:1")
	if !strings.Contains(string(body), `"ir":true`) {
		t.Errorf("the device view must carry ir=true, got %s", body)
	}
}

func TestIRRemoteIsServedAsAButton(t *testing.T) {
	srv := newServerWithProvider(t, buttonProvider{Provider: demo.New()})

	resp, body := getJSON(t, srv.URL+"/api/devices")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var snap snapshotResponse
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snap.Devices) != 1 {
		t.Fatalf("devices = %d", len(snap.Devices))
	}
	d := snap.Devices[0]
	if d.ID != "ir:blast:1" {
		t.Errorf("id = %q", d.ID)
	}
	if len(d.Functions) != 1 || d.Functions[0].Type != tuya.FunctionTypeButton {
		t.Fatalf("functions = %+v, want one Button", d.Functions)
	}
	if d.Functions[0].Name != "Питание" {
		t.Errorf("name = %q", d.Functions[0].Name)
	}
}

func TestAPressIsAcceptedAndANonPressIsRejected(t *testing.T) {
	srv := newServerWithProvider(t, buttonProvider{Provider: demo.New()})

	resp, body := postJSON(t, srv.URL+"/api/devices/ir:blast:1/commands", `{"commands":[{"code":"power","value":true}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("press status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = postJSON(t, srv.URL+"/api/devices/ir:blast:1/commands", `{"commands":[{"code":"power","value":false}]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "button") {
		t.Errorf("body = %s, want an explanation that the point is a button", body)
	}
}
