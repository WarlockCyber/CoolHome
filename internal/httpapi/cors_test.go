package httpapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// do sends a request with arbitrary headers and returns the response and body.
func do(t *testing.T, method, url string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, url, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, string(body)
}

func TestCORSDisabledByDefault(t *testing.T) {
	srv := newTestServer(t, Options{})
	resp, _ := do(t, http.MethodGet, srv.URL+"/api/devices", map[string]string{
		"Origin": "https://mini.example",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty without configuration", got)
	}
}

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	srv := newTestServer(t, Options{AllowedOrigins: []string{"https://mini.example"}})
	resp, body := do(t, http.MethodGet, srv.URL+"/api/devices", map[string]string{
		"Origin": "https://mini.example",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://mini.example" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
	if got := resp.Header.Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("Vary = %q, want it to include Origin so caches do not mix origins", got)
	}
}

// A preflight carries no Authorization header, so it must be answered before
// auth runs or every cross-origin write would fail.
func TestCORSPreflightBypassesAuth(t *testing.T) {
	srv := newTestServer(t, Options{
		AuthToken:      "s3cret",
		AllowedOrigins: []string{"https://mini.example"},
	})
	resp, body := do(t, http.MethodOptions, srv.URL+"/api/devices/dev-1/commands", map[string]string{
		"Origin":                         "https://mini.example",
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "authorization,content-type",
	})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, body = %s; want 204", resp.StatusCode, body)
	}
	for header, want := range map[string]string{
		"Access-Control-Allow-Origin":  "https://mini.example",
		"Access-Control-Allow-Methods": "GET, POST, OPTIONS",
		"Access-Control-Max-Age":       "600",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	allowed := resp.Header.Get("Access-Control-Allow-Headers")
	for _, want := range []string{"Authorization", "X-Auth-Token", "Content-Type"} {
		if !strings.Contains(allowed, want) {
			t.Errorf("Access-Control-Allow-Headers = %q, missing %s", allowed, want)
		}
	}

	// The real request still needs the token.
	resp2, _ := do(t, http.MethodGet, srv.URL+"/api/devices", map[string]string{
		"Origin": "https://mini.example",
	})
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("authenticated requirement lost: status = %d, want 401", resp2.StatusCode)
	}
}

func TestCORSRejectsPreflightFromUnknownOrigin(t *testing.T) {
	srv := newTestServer(t, Options{AllowedOrigins: []string{"https://mini.example"}})
	resp, body := do(t, http.MethodOptions, srv.URL+"/api/devices", map[string]string{
		"Origin":                        "https://evil.example",
		"Access-Control-Request-Method": "GET",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("preflight status = %d, want 403; body = %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for a rejected origin", got)
	}
}

// A plain request from a foreign origin is served but left unlabelled, so the
// browser hides the body. Rejecting it outright would break non-browser clients
// that happen to send an Origin header.
func TestCORSActualRequestFromUnknownOriginIsUnlabelled(t *testing.T) {
	srv := newTestServer(t, Options{AllowedOrigins: []string{"https://mini.example"}})
	resp, _ := do(t, http.MethodGet, srv.URL+"/api/health", map[string]string{
		"Origin": "https://evil.example",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestCORSWildcard(t *testing.T) {
	srv := newTestServer(t, Options{AllowedOrigins: []string{"*"}})
	resp, _ := do(t, http.MethodGet, srv.URL+"/api/health", map[string]string{
		"Origin": "https://anything.example",
	})
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
}

// Config typos in case or a trailing slash must not silently block the client.
func TestCORSNormalizesConfiguredOrigin(t *testing.T) {
	srv := newTestServer(t, Options{AllowedOrigins: []string{"HTTPS://Mini.Example/"}})
	resp, _ := do(t, http.MethodGet, srv.URL+"/api/health", map[string]string{
		"Origin": "https://mini.example",
	})
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://mini.example" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the request origin echoed back", got)
	}
}

// Requests without an Origin header (curl, server-to-server, the native
// mini-app runtime) must be untouched by the CORS layer.
func TestCORSLeavesSameOriginRequestsAlone(t *testing.T) {
	srv := newTestServer(t, Options{AllowedOrigins: []string{"https://mini.example"}})
	resp, _ := do(t, http.MethodGet, srv.URL+"/api/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}
