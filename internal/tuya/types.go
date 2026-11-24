package tuya

import (
	"encoding/json"
	"strings"
)

// FunctionTypeButton is not a Tuya type: it is how CoolHome presents an
// infrared remote key from the code library, which is a momentary action rather
// than a state. Writing it with value true presses the button.
const FunctionTypeButton = "Button"

// FunctionTypePassive is a key the cloud exposes but cannot transmit: a learned
// RF code. The cloud accepts the learning-code payload and reports success, yet
// nothing reaches the air — the blaster's ir_send data point never changes. It
// is shown so the user sees the device's real buttons, but never as a control
// that will not work.
const FunctionTypePassive = "Passive"

// Command is a single data-point write, e.g. {"code":"switch_1","value":true}.
type Command struct {
	Code  string `json:"code"`
	Value any    `json:"value"`
}

// DeviceStatus is one reported data point.
type DeviceStatus struct {
	Code  string `json:"code"`
	Value any    `json:"value"`
}

// Device is a device as returned by the Tuya cloud.
type Device struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	ProductName string `json:"product_name"`
	Online      bool   `json:"online"`
	Icon        string `json:"icon"`
	ActiveTime  int64  `json:"active_time"`
	// SpaceID is the home the device belongs to; the scene API is addressed
	// by it.
	SpaceID string         `json:"bind_space_id"`
	Status  []DeviceStatus `json:"status"`
	// Note explains a device that cannot be driven through this API. It is set
	// by a provider, never parsed from the cloud.
	Note string `json:"-"`
}

// Function describes one writable data point from a device's thing model.
type Function struct {
	Code   string          `json:"code"`
	Type   string          `json:"type"`
	Values json.RawMessage `json:"values"`
	Name   string          `json:"name"`
	DPID   int             `json:"dp_id"`
}

// Spec is the subset of a device's thing model this app uses.
type Spec struct {
	Category  string     `json:"category"`
	Functions []Function `json:"functions"`
	Status    []Function `json:"status"`
}

// ValueSpec is the parsed `values` field of a Function. Tuya sends it either as
// a nested JSON object or as a JSON string containing JSON, so both are handled.
type ValueSpec struct {
	Min    *int     `json:"min"`
	Max    *int     `json:"max"`
	Step   int      `json:"step"`
	Scale  int      `json:"scale"`
	Unit   string   `json:"unit"`
	Range  []string `json:"range"`
	MaxLen int      `json:"maxlen"`
}

// ValueSpec parses the function's value domain. It never fails: an unparsable
// domain yields the zero ValueSpec, which loosens validation instead of
// blocking usage of the device.
func (f Function) ValueSpec() ValueSpec {
	raw := strings.TrimSpace(string(f.Values))
	if raw == "" || raw == "null" {
		return ValueSpec{}
	}
	var inner string
	if err := json.Unmarshal(f.Values, &inner); err == nil {
		raw = inner
	}
	var vs ValueSpec
	if err := json.Unmarshal([]byte(raw), &vs); err != nil {
		return ValueSpec{}
	}
	return vs
}
