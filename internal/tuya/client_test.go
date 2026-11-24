package tuya

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testClientID = "client-id-123"
	testSecret   = "client-secret-456"
)

// verifySign re-derives the expected signature independently of the client.
func verifySign(t *testing.T, r *http.Request, body []byte, accessToken string) {
	t.Helper()
	ts := r.Header.Get("t")
	if ts == "" {
		t.Error("request has no t header")
		return
	}
	if got := r.Header.Get("sign_method"); got != "HMAC-SHA256" {
		t.Errorf("sign_method = %q, want HMAC-SHA256", got)
	}
	sum := sha256.Sum256(body)
	path := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	stringToSign := r.Method + "\n" + hex.EncodeToString(sum[:]) + "\n\n" + path
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(r.Header.Get("client_id") + accessToken + ts + stringToSign))
	want := strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
	if got := r.Header.Get("sign"); got != want {
		t.Errorf("bad signature for %s: got %s want %s", r.URL.Path, got, want)
	}
}

func newTestClient(t *testing.T, srv *httptest.Server, now *time.Time) *Client {
	t.Helper()
	c, err := New(Config{
		ClientID:   testClientID,
		Secret:     testSecret,
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
		Now:        func() time.Time { return *now },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestClientIssuesTokenAndListsDevices(t *testing.T) {
	var (
		mu          sync.Mutex
		tokenCalls  int
		deviceCalls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1.0/token":
			verifySign(t, r, body, "")
			if r.URL.RawQuery != "grant_type=1" {
				t.Errorf("token query = %q", r.URL.RawQuery)
			}
			mu.Lock()
			tokenCalls++
			mu.Unlock()
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"refresh_token":"r","uid":"uid-1"},"t":0,"tid":"t1"}`)
		case "/v1.0/users/uid-1/devices":
			if got := r.Header.Get("access_token"); got != "token-a" {
				t.Errorf("access_token = %q, want token-a", got)
			}
			verifySign(t, r, body, "token-a")
			mu.Lock()
			deviceCalls++
			mu.Unlock()
			io.WriteString(w, `{"success":true,"result":[{"id":"dev-1","name":"Лампа","category":"dj","online":true,"status":[{"code":"switch_led","value":true}]}],"t":0,"tid":"t2"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)

	devices, err := c.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devices) != 1 || devices[0].ID != "dev-1" || devices[0].Status[0].Code != "switch_led" {
		t.Fatalf("unexpected devices: %+v", devices)
	}

	// The token is cached and reused across requests.
	if _, err := c.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("second ListDevices: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if tokenCalls != 1 {
		t.Errorf("token requests = %d, want 1", tokenCalls)
	}
	if deviceCalls != 2 {
		t.Errorf("device requests = %d, want 2", deviceCalls)
	}
}

func TestClientRefreshesTokenWhenExpired(t *testing.T) {
	var (
		mu         sync.Mutex
		tokenCalls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1.0/token":
			verifySign(t, r, body, "")
			mu.Lock()
			tokenCalls++
			mu.Unlock()
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"uid-1"},"t":0,"tid":"t1"}`)
		case "/v1.0/users/uid-1/devices":
			io.WriteString(w, `{"success":true,"result":[],"t":0,"tid":"t2"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	if _, err := c.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	now = now.Add(3 * time.Hour)
	if _, err := c.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("second call: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if tokenCalls != 2 {
		t.Errorf("token requests = %d, want 2 after expiry", tokenCalls)
	}
}

func TestClientRetriesOnceOnInvalidToken(t *testing.T) {
	var (
		mu          sync.Mutex
		tokenCalls  int
		deviceCalls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			mu.Lock()
			tokenCalls++
			seq := tokenCalls
			mu.Unlock()
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-`+strconv.Itoa(seq)+`","expire_time":7200,"uid":"uid-1"},"t":0,"tid":"t1"}`)
		case "/v1.0/devices/dev-1":
			mu.Lock()
			deviceCalls++
			mu.Unlock()
			if r.Header.Get("access_token") == "token-1" {
				io.WriteString(w, `{"success":false,"code":1010,"msg":"token invalid","t":0,"tid":"t2"}`)
				return
			}
			io.WriteString(w, `{"success":true,"result":{"id":"dev-1","name":"Лампа","online":true},"t":0,"tid":"t3"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	dev, err := c.Device(context.Background(), "dev-1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if dev.ID != "dev-1" {
		t.Fatalf("device id = %q", dev.ID)
	}

	mu.Lock()
	defer mu.Unlock()
	if tokenCalls != 2 {
		t.Errorf("token requests = %d, want 2", tokenCalls)
	}
	if deviceCalls != 2 {
		t.Errorf("device requests = %d, want 2", deviceCalls)
	}
}

func TestClientReportsAPIErrorWithoutRetry(t *testing.T) {
	var (
		mu          sync.Mutex
		tokenCalls  int
		deviceCalls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			mu.Lock()
			tokenCalls++
			mu.Unlock()
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"uid-1"},"t":0,"tid":"t1"}`)
		case "/v1.0/devices/dev-1":
			mu.Lock()
			deviceCalls++
			mu.Unlock()
			// Codes sometimes arrive as strings.
			io.WriteString(w, `{"success":false,"code":"1004","msg":"sign invalid","t":0,"tid":"tid-9"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	_, err := c.Device(context.Background(), "dev-1")
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError: %v", err, err)
	}
	if apiErr.Code != 1004 || apiErr.Msg != "sign invalid" || apiErr.TID != "tid-9" {
		t.Fatalf("unexpected APIError: %+v", apiErr)
	}

	mu.Lock()
	defer mu.Unlock()
	if tokenCalls != 1 || deviceCalls != 1 {
		t.Errorf("calls = token %d, device %d; want 1 and 1", tokenCalls, deviceCalls)
	}
}

func TestCommandSendsBody(t *testing.T) {
	var gotBody map[string][]Command
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1.0/token":
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"uid-1"},"t":0,"tid":"t1"}`)
		case "/v1.0/devices/dev-1/commands":
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", r.Method)
			}
			verifySign(t, r, body, "token-a")
			if err := json.Unmarshal(body, &gotBody); err != nil {
				t.Errorf("decode command body: %v", err)
			}
			io.WriteString(w, `{"success":true,"result":true,"t":0,"tid":"t2"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	err := c.Command(context.Background(), "dev-1", []Command{{Code: "switch_1", Value: true}})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if len(gotBody["commands"]) != 1 || gotBody["commands"][0].Code != "switch_1" {
		t.Fatalf("unexpected command body: %+v", gotBody)
	}
}

func TestFlexIntAcceptsNumberAndString(t *testing.T) {
	cases := []struct {
		in   string
		want FlexInt
	}{
		{`{"code":1010}`, 1010},
		{`{"code":"1010"}`, 1010},
		{`{"code":null}`, 0},
		{`{}`, 0},
	}
	for _, tc := range cases {
		var v struct {
			Code FlexInt `json:"code"`
		}
		if err := json.Unmarshal([]byte(tc.in), &v); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.in, err)
		}
		if v.Code != tc.want {
			t.Errorf("%s: code = %d, want %d", tc.in, v.Code, tc.want)
		}
	}
}

func TestValueSpecParsesBothShapes(t *testing.T) {
	nested := Function{Type: "Integer", Values: json.RawMessage(`{"min":10,"max":1000,"step":5,"scale":1,"unit":"°C"}`)}
	asString := Function{Type: "Integer", Values: json.RawMessage(`"{\"min\":0,\"max\":100}"`)}
	empty := Function{Type: "Boolean", Values: json.RawMessage(`{}`)}

	vs := nested.ValueSpec()
	if vs.Min == nil || *vs.Min != 10 || vs.Max == nil || *vs.Max != 1000 || vs.Step != 5 || vs.Scale != 1 || vs.Unit != "°C" {
		t.Fatalf("nested values parsed as %+v", vs)
	}
	vs = asString.ValueSpec()
	if vs.Min == nil || *vs.Min != 0 || vs.Max == nil || *vs.Max != 100 {
		t.Fatalf("string-encoded values parsed as %+v", vs)
	}
	if got := empty.ValueSpec(); got.Min != nil || got.Max != nil {
		t.Fatalf("empty values parsed as %+v", got)
	}
}

// The uid from a token response is the Cloud project owner, which the per-user
// listing rejects with 1106; the client must retry with a linked account's uid.
func TestListDevicesFallsBackToLinkedAccount(t *testing.T) {
	var (
		mu          sync.Mutex
		ownerCalls  int
		assocCalls  int
		linkedCalls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1.0/token":
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"owner-uid"},"t":0,"tid":"t1"}`)
		case "/v1.0/users/owner-uid/devices":
			verifySign(t, r, body, "token-a")
			mu.Lock()
			ownerCalls++
			mu.Unlock()
			io.WriteString(w, `{"success":false,"code":1106,"msg":"permission deny","t":0,"tid":"tid-1106"}`)
		case "/v1.0/iot-01/associated-users/devices":
			verifySign(t, r, body, "token-a")
			mu.Lock()
			assocCalls++
			mu.Unlock()
			io.WriteString(w, `{"success":true,"result":{"devices":[{"id":"dev-1","uid":"linked-uid"}]},"t":0,"tid":"t3"}`)
		case "/v1.0/users/linked-uid/devices":
			verifySign(t, r, body, "token-a")
			mu.Lock()
			linkedCalls++
			mu.Unlock()
			io.WriteString(w, `{"success":true,"result":[{"id":"dev-1","name":"Лампа","online":true}],"t":0,"tid":"t4"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)

	devices, err := c.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devices) != 1 || devices[0].ID != "dev-1" {
		t.Fatalf("unexpected devices: %+v", devices)
	}
	// The linked uid is remembered, so the next poll skips both the rejected
	// owner uid and the account lookup.
	if _, err := c.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("second ListDevices: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if ownerCalls != 1 {
		t.Errorf("owner uid requests = %d, want 1", ownerCalls)
	}
	if assocCalls != 1 {
		t.Errorf("associated-users requests = %d, want 1 (result cached)", assocCalls)
	}
	if linkedCalls != 2 {
		t.Errorf("linked uid requests = %d, want 2", linkedCalls)
	}
}

func TestListDevicesKeepsConfiguredUIDOnPermissionDeny(t *testing.T) {
	var assocCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"owner-uid"},"t":0,"tid":"t1"}`)
		case "/v1.0/users/explicit-uid/devices":
			io.WriteString(w, `{"success":false,"code":1106,"msg":"permission deny","t":0,"tid":"tid-1106"}`)
		case "/v1.0/iot-01/associated-users/devices":
			assocCalls++
			io.WriteString(w, `{"success":true,"result":{"devices":[{"id":"dev-1","uid":"linked-uid"}]},"t":0,"tid":"t3"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c, err := New(Config{
		ClientID:   testClientID,
		Secret:     testSecret,
		BaseURL:    srv.URL,
		UID:        "explicit-uid",
		HTTPClient: srv.Client(),
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.ListDevices(context.Background(), nil)
	if err == nil {
		t.Fatal("expected a permission error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 1106 {
		t.Fatalf("error = %v, want APIError 1106", err)
	}
	if !strings.Contains(err.Error(), "Link Tuya App Account") {
		t.Errorf("error has no actionable hint: %v", err)
	}
	if assocCalls != 0 {
		t.Errorf("associated-users requests = %d, want 0: an explicit uid must not be overridden", assocCalls)
	}
}

func TestListDevicesReportsMissingLinkedAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/token":
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"owner-uid"},"t":0,"tid":"t1"}`)
		case "/v1.0/users/owner-uid/devices":
			io.WriteString(w, `{"success":false,"code":1106,"msg":"permission deny","t":0,"tid":"tid-1106"}`)
		case "/v1.0/iot-01/associated-users/devices":
			io.WriteString(w, `{"success":true,"result":{"devices":[]},"t":0,"tid":"t3"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)

	_, err := c.ListDevices(context.Background(), nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "no Smart Life account linked") {
		t.Errorf("error = %v, want a missing-linked-account hint", err)
	}
	if !strings.Contains(err.Error(), "1106") {
		t.Errorf("error = %v, want the original 1106 preserved", err)
	}
}

// A device the cloud refuses to describe is a permanent property, not a
// failure: callers must be able to tell it apart and stop retrying.
func TestIsUnsupportedDevice(t *testing.T) {
	if !IsUnsupportedDevice(&APIError{Code: 2009, Msg: "not support this device"}) {
		t.Error("2009 must be recognised as an unsupported device")
	}
	for _, err := range []error{
		&APIError{Code: 1106, Msg: "permission deny"},
		&APIError{Code: 1004, Msg: "sign invalid"},
		errors.New("plain error"),
		nil,
	} {
		if IsUnsupportedDevice(err) {
			t.Errorf("%v must not be treated as an unsupported device", err)
		}
	}
}

func TestIsServiceNotSubscribed(t *testing.T) {
	if !IsServiceNotSubscribed(&APIError{Code: 28841101, Msg: "No permissions. This API is not subscribed."}) {
		t.Error("28841101 must be recognised as an unsubscribed service")
	}
	for _, err := range []error{
		&APIError{Code: 2009, Msg: "not support this device"},
		&APIError{Code: 1106, Msg: "permission deny"},
		errors.New("plain"),
		nil,
	} {
		if IsServiceNotSubscribed(err) {
			t.Errorf("%v must not be treated as an unsubscribed service", err)
		}
	}
}

func TestIRClientPaths(t *testing.T) {
	var (
		mu     sync.Mutex
		seen   []string
		gotKey map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()

		switch r.URL.Path {
		case "/v1.0/token":
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"uid-1"},"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/categories":
			verifySign(t, r, body, "token-a")
			// Numbers arrive as strings, and the list is a bare array.
			io.WriteString(w, `{"success":true,"result":[{"category_id":"2","category_name":"TV"}],"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes":
			verifySign(t, r, body, "token-a")
			io.WriteString(w, `{"success":true,"result":[{"remote_id":"r-1","remote_name":"TV","remote_index":"449101998","category_id":"2","brand_name":"Aura"}],"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/r-1/keys":
			verifySign(t, r, body, "token-a")
			// The list is wrapped in key_list, and key_id is always 0.
			io.WriteString(w, `{"success":true,"result":{"key_list":[{"key":"PowerOn","key_id":0,"key_name":"power on","standard_key":true}],"remote_index":449101998,"category_id":5},"t":0,"tid":"t"}`)
		case "/v1.0/devices/blast-1/sub-devices":
			verifySign(t, r, body, "token-a")
			// Learned remotes: device field names, and an online flag.
			io.WriteString(w, `{"success":true,"result":[{"id":"rf-1","name":"Второй этаж","category":"rf_switch1","online":true}],"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/rf-1/keys":
			verifySign(t, r, body, "token-a")
			// A learned key carries a name only; the payload comes separately.
			io.WriteString(w, `{"success":true,"result":{"key_list":[{"key":"one_on","key_id":12,"key_name":"one ON","standard_key":false}],"remote_index":1755924745,"category_id":6},"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/rf-1/learning-codes":
			verifySign(t, r, body, "token-a")
			if r.Method == http.MethodPost {
				var decoded map[string]any
				if err := json.Unmarshal(body, &decoded); err != nil {
					t.Errorf("decode learning-code body: %v", err)
				}
				mu.Lock()
				gotKey = decoded
				mu.Unlock()
				io.WriteString(w, `{"success":true,"result":true,"t":0,"tid":"t"}`)
				return
			}
			io.WriteString(w, `{"success":true,"result":[{"code":"{\"control\":\"rfstudy_send\"}","id":21492971,"key_name":"one ON","name":"one_on","remote_id":"rf-1"}],"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/r-1/learning-codes":
			verifySign(t, r, body, "token-a")
			// A library remote has no learned payloads.
			io.WriteString(w, `{"success":true,"result":[],"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/r-1/command":
			if r.Method != http.MethodPost {
				t.Errorf("command method = %s, want POST", r.Method)
			}
			verifySign(t, r, body, "token-a")
			var decoded map[string]any
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Errorf("decode command body: %v", err)
			}
			mu.Lock()
			gotKey = decoded
			mu.Unlock()
			io.WriteString(w, `{"success":true,"result":true,"t":0,"tid":"t"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	ctx := context.Background()

	cats, err := c.IRCategories(ctx, "blast-1")
	if err != nil {
		t.Fatalf("IRCategories: %v", err)
	}
	if len(cats) != 1 || cats[0].CategoryName != "TV" {
		t.Fatalf("categories = %+v", cats)
	}

	remotes, err := c.IRRemotes(ctx, "blast-1")
	if err != nil {
		t.Fatalf("IRRemotes: %v", err)
	}
	if len(remotes) != 1 || remotes[0].Name != "TV" || remotes[0].ID != "r-1" {
		t.Fatalf("remotes = %+v", remotes)
	}
	if int(remotes[0].CategoryID) != 2 || int(remotes[0].Index) != 449101998 {
		t.Errorf("string-encoded numbers parsed as category=%d index=%d", remotes[0].CategoryID, remotes[0].Index)
	}

	subs, err := c.IRSubDevices(ctx, "blast-1")
	if err != nil {
		t.Fatalf("IRSubDevices: %v", err)
	}
	if len(subs) != 1 || subs[0].ID != "rf-1" || subs[0].Category != "rf_switch1" {
		t.Fatalf("sub-devices = %+v, want the RF switch with device field names", subs)
	}
	if !subs[0].Available() {
		t.Error("an online sub-device must be available")
	}
	if !subs[0].IsSubDevice() {
		t.Error("a remote with a category comes from the sub-device listing")
	}

	keys, err := c.IRKeys(ctx, "blast-1", "r-1")
	if err != nil {
		t.Fatalf("IRKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].Key != "PowerOn" || keys[0].Name != "power on" || !keys[0].Standard {
		t.Fatalf("keys = %+v, want the entry from key_list", keys)
	}

	if err := c.IRSendKey(ctx, "blast-1", "r-1", keys[0]); err != nil {
		t.Fatalf("IRSendKey: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotKey["key"] != "PowerOn" {
		t.Errorf("body key = %v, want PowerOn", gotKey["key"])
	}
	// key_id is always 0 in real code libraries, so it must not be sent.
	if _, sent := gotKey["key_id"]; sent {
		t.Errorf("body must carry the key name only, got %+v", gotKey)
	}
	// The path must address the remote by remote_id: the numeric index answers
	// 1106 on the real cloud.
	for _, line := range seen {
		if strings.Contains(line, "/remotes/449101998") {
			t.Errorf("path addressed the remote by index: %s", line)
		}
		if strings.HasPrefix(line, "GET /v1.0/infrareds/blast-1/remotes?") {
			if !strings.Contains(line, "page_size=100") {
				t.Errorf("remote list query = %q, want pagination", line)
			}
		}
	}
}

func TestIRBlasterDetection(t *testing.T) {
	for _, category := range []string{"wnykq", "WNYkQ", "ir", "infrared"} {
		if !IsIRBlaster(category) {
			t.Errorf("%q must be a blaster", category)
		}
	}
	for _, category := range []string{"rf_switch1", "infrared_ac", "dj", "", "cz"} {
		if IsIRBlaster(category) {
			t.Errorf("%q must not be a blaster", category)
		}
	}
}

// An RF switch key exists only because a physical remote was pressed; sending
// its name is refused by the cloud, so the learned payload must be replayed.
func TestLearnedKeyIsSentAsLearningCode(t *testing.T) {
	var (
		mu       sync.Mutex
		sentBody map[string]any
		cmdCalls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1.0/token":
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"uid-1"},"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/rf-1/keys":
			io.WriteString(w, `{"success":true,"result":{"key_list":[{"key":"one_on","key_id":12,"key_name":"one ON","standard_key":false}]},"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/rf-1/learning-codes":
			if r.Method == http.MethodPost {
				var decoded map[string]any
				json.Unmarshal(body, &decoded)
				mu.Lock()
				sentBody = decoded
				mu.Unlock()
				io.WriteString(w, `{"success":true,"result":true,"t":0,"tid":"t"}`)
				return
			}
			io.WriteString(w, `{"success":true,"result":[{"code":"{\"control\":\"rfstudy_send\"}","id":1,"key_name":"one ON","name":"one_on","remote_id":"rf-1"}],"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/rf-1/command":
			mu.Lock()
			cmdCalls++
			mu.Unlock()
			io.WriteString(w, `{"success":false,"code":30706,"msg":"command or value not support","t":0,"tid":"t"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	ctx := context.Background()

	keys, err := c.IRKeys(ctx, "blast-1", "rf-1")
	if err != nil {
		t.Fatalf("IRKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("keys = %+v", keys)
	}
	if keys[0].Code == "" {
		t.Fatal("the learned key must carry its payload")
	}

	if err := c.IRSendKey(ctx, "blast-1", "rf-1", keys[0]); err != nil {
		t.Fatalf("IRSendKey: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if cmdCalls != 0 {
		t.Errorf("/command was called %d times; a learned key must not go there", cmdCalls)
	}
	if sentBody["code"] != keys[0].Code {
		t.Errorf("sent body = %+v, want the learned code", sentBody)
	}
}

// A library key has no payload and keeps using the named-command endpoint.
func TestLibraryKeyStillUsesNamedCommand(t *testing.T) {
	var gotKey map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1.0/token":
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"uid-1"},"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/r-1/keys":
			io.WriteString(w, `{"success":true,"result":{"key_list":[{"key":"PowerOn","key_id":0,"key_name":"power on","standard_key":true}]},"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/r-1/learning-codes":
			io.WriteString(w, `{"success":true,"result":[],"t":0,"tid":"t"}`)
		case "/v1.0/infrareds/blast-1/remotes/r-1/command":
			var decoded map[string]any
			json.Unmarshal(body, &decoded)
			gotKey = decoded
			io.WriteString(w, `{"success":true,"result":true,"t":0,"tid":"t"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	keys, err := c.IRKeys(context.Background(), "blast-1", "r-1")
	if err != nil {
		t.Fatalf("IRKeys: %v", err)
	}
	if keys[0].Code != "" {
		t.Errorf("a library key must have no learned payload, got %q", keys[0].Code)
	}
	if err := c.IRSendKey(context.Background(), "blast-1", "r-1", keys[0]); err != nil {
		t.Fatalf("IRSendKey: %v", err)
	}
	if gotKey["key"] != "PowerOn" {
		t.Errorf("body = %+v, want the key name", gotKey)
	}
}

// The cloud signs the canonical query string and rejects another parameter
// order with 1004 "sign invalid". url.Values.Encode sorts, so the client is
// correct only as long as it builds queries that way.
func TestSceneQueriesAreSortedAndSigned(t *testing.T) {
	var (
		mu       sync.Mutex
		gotQuery []string
		gotPath  []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/v1.0/token":
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"uid-1"},"t":0,"tid":"t"}`)
		case r.URL.Path == "/v1.0/users/uid-1/devices":
			io.WriteString(w, `{"success":true,"result":[{"id":"dev-1","name":"Lamp","category":"dj"}],"t":0,"tid":"t"}`)
		case r.URL.Path == "/v2.0/cloud/thing/dev-1":
			io.WriteString(w, `{"success":true,"result":{"id":"dev-1","bind_space_id":"space-1"},"t":0,"tid":"t"}`)
		case r.URL.Path == "/v2.0/cloud/scene/rule":
			mu.Lock()
			gotQuery = append(gotQuery, r.URL.RawQuery)
			mu.Unlock()
			verifySign(t, r, body, "token-a")
			io.WriteString(w, `{"success":true,"result":{"list":[{"id":"s-1","name":"scene one","status":"enable","type":"scene"}],"has_more":false,"total":1},"t":0,"tid":"t"}`)
		case r.URL.Path == "/v2.0/cloud/scene/rule/s-1/actions/trigger":
			mu.Lock()
			gotPath = append(gotPath, r.Method+" "+r.URL.Path)
			mu.Unlock()
			verifySign(t, r, body, "token-a")
			io.WriteString(w, `{"success":true,"result":true,"t":0,"tid":"t"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	ctx := context.Background()

	space, err := c.SpaceID(ctx)
	if err != nil {
		t.Fatalf("SpaceID: %v", err)
	}
	if space != "space-1" {
		t.Fatalf("space = %q, want space-1 from the thing endpoint", space)
	}

	scenes, err := c.Scenes(ctx, space)
	if err != nil {
		t.Fatalf("Scenes: %v", err)
	}
	if len(scenes) != 1 || scenes[0].Name != "scene one" || !scenes[0].Enabled() {
		t.Fatalf("scenes = %+v", scenes)
	}

	if err := c.TriggerScene(ctx, "s-1"); err != nil {
		t.Fatalf("TriggerScene: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(gotQuery) == 0 {
		t.Fatal("no scene query was made")
	}
	want := "page_no=1&page_size=50&space_id=space-1&type=scene"
	if gotQuery[0] != want {
		t.Errorf("query = %q, want %q (sorted, or the cloud answers 1004 sign invalid)", gotQuery[0], want)
	}
	if len(gotPath) != 1 || gotPath[0] != "POST /v2.0/cloud/scene/rule/s-1/actions/trigger" {
		t.Errorf("trigger request = %v", gotPath)
	}
}
