package tuya

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Data-center hosts. Pick the one that matches the country your app account
// was registered in; the wrong host answers 1004/sign invalid.
var baseURLs = map[string]string{
	"cn":   "https://openapi.tuyacn.com",
	"us":   "https://openapi.tuyaus.com",
	"ueaz": "https://openapi-ueaz.tuyaus.com",
	"eu":   "https://openapi.tuyaeu.com",
	"weaz": "https://openapi-weaz.tuyaeu.com",
	"in":   "https://openapi.tuyain.com",
	"sg":   "https://openapi-sg.iotbing.com",
}

// Config configures the Tuya Cloud client.
type Config struct {
	ClientID string
	Secret   string
	// Region is one of the keys of baseURLs (cn, us, ueaz, eu, weaz, in, sg).
	Region string
	// BaseURL overrides Region when set (useful for tests or a proxy).
	BaseURL string
	// UID is optional; when empty it is taken from the token response.
	UID string

	HTTPClient *http.Client
	// Now is injectable for tests.
	Now func() time.Time
}

// Client is a minimal Tuya Cloud OpenAPI client.
type Client struct {
	http     *http.Client
	baseURL  string
	clientID string
	secret   string
	uid      string
	now      func() time.Time

	mu    sync.Mutex
	token *tokenState

	uidMu     sync.Mutex
	linkedUID string // uid of a Smart Life account linked to the project
}

type tokenState struct {
	access string
	uid    string
	expiry time.Time
}

// New validates the configuration and builds a client.
func New(cfg Config) (*Client, error) {
	if cfg.ClientID == "" || cfg.Secret == "" {
		return nil, fmt.Errorf("tuya: client id and secret are required")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		host, ok := baseURLs[strings.ToLower(strings.TrimSpace(cfg.Region))]
		if !ok {
			return nil, fmt.Errorf("tuya: unknown region %q (want one of cn, us, ueaz, eu, weaz, in, sg, or set an explicit base URL)", cfg.Region)
		}
		base = host
	}
	c := &Client{
		http:     cfg.HTTPClient,
		baseURL:  base,
		clientID: cfg.ClientID,
		secret:   cfg.Secret,
		uid:      cfg.UID,
		now:      cfg.Now,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 20 * time.Second}
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

// BaseURL reports the resolved data-center endpoint.
func (c *Client) BaseURL() string { return c.baseURL }

type envelope struct {
	Success bool            `json:"success"`
	Code    FlexInt         `json:"code"`
	Msg     string          `json:"msg"`
	T       int64           `json:"t"`
	TID     string          `json:"tid"`
	Result  json.RawMessage `json:"result"`
}

// FlexInt accepts both 1004 and "1004", which the API uses interchangeably: the
// error envelope sends numbers, while the IR endpoints send everything as
// strings. It is exported because the IR types below carry these fields.
type FlexInt int

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("FlexInt: %q is not an integer", s)
	}
	*f = FlexInt(n)
	return nil
}

// UID returns the account uid used by the per-user device listing.
func (c *Client) UID(ctx context.Context) (string, error) {
	if c.uid != "" {
		return c.uid, nil
	}
	tok, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	if tok.uid == "" {
		return "", fmt.Errorf("tuya: token response did not include a uid; set TUYA_UID")
	}
	return tok.uid, nil
}

// ListDevices returns every device for the account, or the devices named by
// ids when provided.
func (c *Client) ListDevices(ctx context.Context, ids []string) ([]Device, error) {
	if len(ids) > 0 {
		out := make([]Device, 0, len(ids))
		for _, id := range ids {
			d, err := c.Device(ctx, id)
			if err != nil {
				return nil, permissionHint(err)
			}
			out = append(out, d)
		}
		return out, nil
	}
	uid, err := c.UID(ctx)
	if err != nil {
		return nil, err
	}
	// Once the linked account is known, address it directly: the token uid is
	// the project owner and the per-user listing rejects it.
	if linked := c.cachedLinkedUID(); linked != "" {
		uid = linked
	}
	devices, err := c.devicesOfUser(ctx, uid)
	if err == nil {
		return devices, nil
	}
	if !isPermissionError(err) {
		return nil, err
	}
	// The uid in a token response belongs to the Cloud project owner, while
	// /v1.0/users/{uid}/devices only accepts a Smart Life account linked to the
	// project. An explicitly configured uid is never overridden silently.
	if c.uid != "" {
		return nil, permissionHint(err)
	}
	linked, lerr := c.linkedAccountUID(ctx)
	if lerr != nil {
		return nil, permissionHint(errors.Join(err, lerr))
	}
	if linked == uid {
		return nil, permissionHint(err)
	}
	devices, lerr = c.devicesOfUser(ctx, linked)
	if lerr != nil {
		return nil, permissionHint(lerr)
	}
	return devices, nil
}

func (c *Client) devicesOfUser(ctx context.Context, uid string) ([]Device, error) {
	var devices []Device
	if err := c.call(ctx, http.MethodGet, "/v1.0/users/"+url.PathEscape(uid)+"/devices", nil, nil, &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

// linkedAccountUID finds the uid of a Smart Life account linked to the project
// by asking the cloud for the devices of associated accounts. The answer is
// cached: it changes only when somebody re-links an account.
func (c *Client) linkedAccountUID(ctx context.Context) (string, error) {
	c.uidMu.Lock()
	defer c.uidMu.Unlock()
	if c.linkedUID != "" {
		return c.linkedUID, nil
	}
	var res struct {
		Devices []struct {
			UID string `json:"uid"`
		} `json:"devices"`
	}
	query := url.Values{"size": {"20"}}
	if err := c.call(ctx, http.MethodGet, "/v1.0/iot-01/associated-users/devices", query, nil, &res); err != nil {
		return "", err
	}
	for _, d := range res.Devices {
		if d.UID != "" {
			c.linkedUID = d.UID
			return d.UID, nil
		}
	}
	return "", fmt.Errorf("tuya: no Smart Life account linked to the project owns devices; set TUYA_UID or link an account in Cloud → Devices → Link Tuya App Account")
}

func (c *Client) cachedLinkedUID() string {
	c.uidMu.Lock()
	defer c.uidMu.Unlock()
	return c.linkedUID
}

// Device returns one device including its current status.
func (c *Client) Device(ctx context.Context, id string) (Device, error) {
	var d Device
	err := c.call(ctx, http.MethodGet, "/v1.0/devices/"+url.PathEscape(id), nil, nil, &d)
	return d, err
}

// Spec returns the writable functions and reported status points of a device.
func (c *Client) Spec(ctx context.Context, id string) (Spec, error) {
	var s Spec
	err := c.call(ctx, http.MethodGet, "/v1.0/devices/"+url.PathEscape(id)+"/specifications", nil, nil, &s)
	return s, err
}

// Command sends data points to a device.
func (c *Client) Command(ctx context.Context, id string, cmds []Command) error {
	body := struct {
		Commands []Command `json:"commands"`
	}{Commands: cmds}
	return c.call(ctx, http.MethodPost, "/v1.0/devices/"+url.PathEscape(id)+"/commands", nil, body, nil)
}

// call performs a signed request, refreshing the token once when the cloud
// reports it as invalid or expired.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, body, out any) error {
	payload, err := encodeBody(body)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		tok, err := c.accessToken(ctx)
		if err != nil {
			return err
		}
		err = c.send(ctx, method, path, query, payload, tok.access, out)
		if err == nil {
			return nil
		}
		if attempt == 0 && isTokenError(err) {
			c.invalidateToken()
			continue
		}
		return err
	}
}

func encodeBody(body any) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("tuya: encode request: %w", err)
	}
	return payload, nil
}

func (c *Client) send(ctx context.Context, method, path string, query url.Values, payload []byte, accessToken string, out any) error {
	pathWithQuery := path
	if len(query) > 0 {
		pathWithQuery += "?" + query.Encode()
	}
	contentHash := sha256Hex(string(payload))
	ts := c.now().UnixMilli()
	sign := signBusiness(c.clientID, accessToken, c.secret, ts, method, contentHash, pathWithQuery)

	var bodyReader io.Reader
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+pathWithQuery, bodyReader)
	if err != nil {
		return fmt.Errorf("tuya: build request: %w", err)
	}
	req.Header.Set("client_id", c.clientID)
	req.Header.Set("access_token", accessToken)
	req.Header.Set("sign", sign)
	req.Header.Set("t", strconv.FormatInt(ts, 10))
	req.Header.Set("sign_method", "HMAC-SHA256")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tuya: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("tuya: read response: %w", err)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("tuya: %s %s: http %d: %s", method, path, resp.StatusCode, snippet(raw))
	}
	if !env.Success {
		msg := env.Msg
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return &APIError{Code: int(env.Code), Msg: msg, Status: resp.StatusCode, TID: env.TID}
	}
	if out != nil && len(env.Result) > 0 && string(env.Result) != "null" {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("tuya: decode result of %s %s: %w", method, path, err)
		}
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	if s == "" {
		return "<empty body>"
	}
	return s
}

func (c *Client) accessToken(ctx context.Context) (tokenState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != nil && c.now().Before(c.token.expiry) {
		return *c.token, nil
	}
	tok, err := c.fetchToken(ctx)
	if err != nil {
		return tokenState{}, err
	}
	c.token = tok
	return *tok, nil
}

func (c *Client) invalidateToken() {
	c.mu.Lock()
	c.token = nil
	c.mu.Unlock()
}

func (c *Client) fetchToken(ctx context.Context) (*tokenState, error) {
	const path = "/v1.0/token"
	const method = http.MethodGet
	pathWithQuery := path + "?grant_type=1"
	ts := c.now().UnixMilli()
	sign := signToken(c.clientID, c.secret, ts, method, sha256Hex(""), pathWithQuery)

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+pathWithQuery, nil)
	if err != nil {
		return nil, fmt.Errorf("tuya: build token request: %w", err)
	}
	req.Header.Set("client_id", c.clientID)
	req.Header.Set("sign", sign)
	req.Header.Set("t", strconv.FormatInt(ts, 10))
	req.Header.Set("sign_method", "HMAC-SHA256")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tuya: token request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("tuya: read token response: %w", err)
	}
	var env struct {
		Success bool    `json:"success"`
		Code    FlexInt `json:"code"`
		Msg     string  `json:"msg"`
		TID     string  `json:"tid"`
		Result  struct {
			AccessToken  string `json:"access_token"`
			ExpireTime   int64  `json:"expire_time"`
			RefreshToken string `json:"refresh_token"`
			UID          string `json:"uid"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("tuya: token: http %d: %s", resp.StatusCode, snippet(raw))
	}
	if !env.Success || env.Result.AccessToken == "" {
		return nil, &APIError{Code: int(env.Code), Msg: env.Msg, Status: resp.StatusCode, TID: env.TID}
	}

	lifetime := time.Duration(env.Result.ExpireTime) * time.Second
	if lifetime <= 0 {
		lifetime = time.Hour
	}
	// Refresh well before the cloud expires the token.
	margin := 60 * time.Second
	if lifetime/10 < margin {
		margin = lifetime / 10
	}
	return &tokenState{
		access: env.Result.AccessToken,
		uid:    env.Result.UID,
		expiry: c.now().Add(lifetime - margin),
	}, nil
}

// itoa is a tiny helper so scene query values read as strings without pulling
// strconv into every call site.
func itoa(n int) string { return strconv.Itoa(n) }
