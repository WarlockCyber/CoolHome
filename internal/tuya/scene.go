package tuya

import (
	"context"
	"net/http"
	"net/url"
)

// Tuya scene linkage rules: tap-to-run scenes and automations.
//
// This is the current service (the older "Smart Home Scene Linkage" is marked
// legacy). It is the only supported way to perform an action the data-point API
// cannot: a learned RF key, for instance, is accepted by the learning-code
// endpoint and never transmitted, while a tap-to-run scene that presses the
// same key does work, because the cloud executes it.
//
// Confirmed against the live cloud: the rule list and the trigger both answer
// with data rather than 1108/28841101.

// Scene is one linkage rule: a tap-to-run scene or an automation.
type Scene struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	RunningMode string `json:"running_mode"`
	SpaceID     string `json:"space_id"`
	Type        string `json:"type"`
}

// Enabled reports whether a scene may be triggered.
func (s Scene) Enabled() bool { return s.Status == "enable" }

// Scenes lists the tap-to-run scenes of a space. Automations are excluded: they
// run on their own conditions and are not buttons.
func (c *Client) Scenes(ctx context.Context, spaceID string) ([]Scene, error) {
	const pageSize = 50
	var out []Scene
	for page := 1; ; page++ {
		// Values are sorted by url.Values.Encode, which is required: the cloud
		// signs the canonical query string and rejects another order with
		// 1004 "sign invalid".
		query := url.Values{
			"space_id":  {spaceID},
			"type":      {"scene"},
			"page_no":   {itoa(page)},
			"page_size": {itoa(pageSize)},
		}
		var res struct {
			List    []Scene `json:"list"`
			HasMore bool    `json:"has_more"`
			Total   int     `json:"total"`
		}
		if err := c.call(ctx, http.MethodGet, "/v2.0/cloud/scene/rule", query, nil, &res); err != nil {
			return nil, err
		}
		out = append(out, res.List...)
		if !res.HasMore || len(res.List) == 0 {
			return out, nil
		}
	}
}

// TriggerScene runs a tap-to-run scene. The cloud performs the actions, so this
// is the supported path for a device whose own data points are not writable.
func (c *Client) TriggerScene(ctx context.Context, ruleID string) error {
	path := "/v2.0/cloud/scene/rule/" + url.PathEscape(ruleID) + "/actions/trigger"
	return c.call(ctx, http.MethodPost, path, nil, nil, nil)
}

// SpaceID returns the space (home) id the account's devices belong to. The
// scene API is addressed by space.
//
// The per-user device list does not carry bind_space_id — only the single-device
// detail does — so one device is fetched for it.
func (c *Client) SpaceID(ctx context.Context) (string, error) {
	devices, err := c.ListDevices(ctx, nil)
	if err != nil {
		return "", err
	}
	for _, d := range devices {
		if d.SpaceID != "" {
			return d.SpaceID, nil
		}
	}
	if len(devices) == 0 {
		return "", nil
	}
	// Only the thing endpoint carries bind_space_id: /v1.0/devices/{id} does
	// not include it.
	var thing struct {
		SpaceID string `json:"bind_space_id"`
	}
	path := "/v2.0/cloud/thing/" + url.PathEscape(devices[0].ID)
	if err := c.call(ctx, http.MethodGet, path, nil, nil, &thing); err != nil {
		return "", err
	}
	return thing.SpaceID, nil
}
