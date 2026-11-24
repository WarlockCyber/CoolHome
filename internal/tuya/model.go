package tuya

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// The legacy data-point API answers "not support this device" (2009) for
// products it has no legacy model for: a sensor the cloud recorded under
// another category, a thermostat paired through a gateway, an IR air
// conditioner. Those devices are real and online — they simply describe
// themselves through IoT Core instead:
//
//	GET /v2.0/cloud/thing/{id}/model             → {"model": "<JSON string>"}
//	GET /v2.0/cloud/thing/{id}/shadow/properties → {"properties":[{code,value}]}
//
// This file maps that model onto the Spec the rest of CoolHome already speaks,
// so no other layer needs a second dialect.

// thingModelDoc is the thing model document: a product's services, each holding
// the data points (properties) of one capability.
type thingModelDoc struct {
	ModelID  string `json:"modelId"`
	Services []struct {
		Properties []thingProperty `json:"properties"`
	} `json:"services"`
}

type thingProperty struct {
	AbilityID  int           `json:"abilityId"`
	AccessMode string        `json:"accessMode"`
	Code       string        `json:"code"`
	Name       string        `json:"name"`
	TypeSpec   thingTypeSpec `json:"typeSpec"`
}

type thingTypeSpec struct {
	Type   string   `json:"type"`
	Min    *int     `json:"min"`
	Max    *int     `json:"max"`
	Scale  int      `json:"scale"`
	Step   int      `json:"step"`
	Unit   string   `json:"unit"`
	Range  []string `json:"range"`
	MaxLen int      `json:"maxlen"`
}

// ParseThingModel turns the model document into a Spec.
//
// Two properties are deliberately dropped. A value whose type is a blob
// (string, raw, json, bitmap) is a protocol payload — an IR command, a colour
// blob, a stored scene — not something a person sets on a wall panel. An enum
// with fewer than two options is a fixed command rather than a choice; that is
// how an RF remote hides its learned keys behind "scene_1..4 = scene", and
// showing four buttons that all say "scene" would be worse than showing none.
func ParseThingModel(raw string) (Spec, error) {
	var doc thingModelDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return Spec{}, fmt.Errorf("tuya: parse thing model: %w", err)
	}
	var spec Spec
	for _, service := range doc.Services {
		for _, p := range service.Properties {
			fn, ok := p.function()
			if !ok {
				continue
			}
			if p.writable() {
				spec.Functions = append(spec.Functions, fn)
			} else {
				spec.Status = append(spec.Status, fn)
			}
		}
	}
	return spec, nil
}

// writable reports whether the model allows writing the point. "rw" and "wr"
// both do; "ro" does not.
//
// An access mode that says neither — a model variant that omits the field — is
// treated as read-only on purpose. The conservative error is a control that is
// not offered; the other one is a control that promises a write the cloud may
// refuse, and a wall panel that lies about what it can do is worse.
func (p thingProperty) writable() bool {
	return strings.Contains(strings.ToLower(p.AccessMode), "w")
}

// function converts one property, reporting false for a point the UI cannot
// usefully present.
func (p thingProperty) function() (Function, bool) {
	if p.Code == "" {
		return Function{}, false
	}
	var typ string
	switch strings.ToLower(p.TypeSpec.Type) {
	case "bool", "boolean":
		typ = "Boolean"
	case "value", "int", "integer", "float", "double":
		typ = "Integer"
	case "enum":
		if len(p.TypeSpec.Range) < 2 {
			return Function{}, false
		}
		typ = "Enum"
	default:
		return Function{}, false
	}
	values, err := json.Marshal(thingValueSpec{
		Min:    p.TypeSpec.Min,
		Max:    p.TypeSpec.Max,
		Step:   p.TypeSpec.Step,
		Scale:  p.TypeSpec.Scale,
		Unit:   p.TypeSpec.Unit,
		Range:  p.TypeSpec.Range,
		MaxLen: p.TypeSpec.MaxLen,
	})
	if err != nil {
		return Function{}, false
	}
	// The name is left empty on purpose: the model's names are Chinese, and the
	// UI labels known codes in Russian and falls back to the code itself, which
	// beats showing 温标切换 to someone reading the wall.
	return Function{Code: p.Code, Type: typ, Values: values, DPID: p.AbilityID}, true
}

// thingValueSpec is the domain written back into Function.Values, in the shape
// ValueSpec already parses.
type thingValueSpec struct {
	Min    *int     `json:"min,omitempty"`
	Max    *int     `json:"max,omitempty"`
	Step   int      `json:"step,omitempty"`
	Scale  int      `json:"scale,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	Range  []string `json:"range,omitempty"`
	MaxLen int      `json:"maxlen,omitempty"`
}

// ThingModel fetches the product model of a device and converts it.
func (c *Client) ThingModel(ctx context.Context, id string) (Spec, error) {
	var res struct {
		Model string `json:"model"`
	}
	path := "/v2.0/cloud/thing/" + url.PathEscape(id) + "/model"
	if err := c.call(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return Spec{}, err
	}
	if strings.TrimSpace(res.Model) == "" {
		return Spec{}, &APIError{Code: codeNoThingModel, Msg: "device has no thing model"}
	}
	return ParseThingModel(res.Model)
}

// IssueProperties writes data points through IoT Core. It is the write path of
// the same devices the thing model describes: the legacy /commands endpoint
// answers 2008 for them, while this one accepts the same values.
//
// The cloud does not validate here — it accepted an out-of-range enum value
// when this was measured — so the thing model is the only guard. That is why
// CoolHome validates every command before it gets here.
func (c *Client) IssueProperties(ctx context.Context, id string, properties map[string]any) error {
	body := struct {
		Properties map[string]any `json:"properties"`
	}{Properties: properties}
	path := "/v2.0/cloud/thing/" + url.PathEscape(id) + "/shadow/properties/issue"
	return c.call(ctx, http.MethodPost, path, nil, body, nil)
}

// ShadowProperties returns the latest value of every data point the cloud keeps
// for a device. It is the only source of current values for devices the legacy
// status field leaves empty.
func (c *Client) ShadowProperties(ctx context.Context, id string) ([]DeviceStatus, error) {
	var res struct {
		Properties []struct {
			Code  string `json:"code"`
			Value any    `json:"value"`
		} `json:"properties"`
	}
	path := "/v2.0/cloud/thing/" + url.PathEscape(id) + "/shadow/properties"
	if err := c.call(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return nil, err
	}
	out := make([]DeviceStatus, 0, len(res.Properties))
	for _, p := range res.Properties {
		// A data point that was never reported arrives with a null value; there
		// is nothing to show and nothing to compare against.
		if p.Code == "" || p.Value == nil {
			continue
		}
		out = append(out, DeviceStatus{Code: p.Code, Value: p.Value})
	}
	return out, nil
}
