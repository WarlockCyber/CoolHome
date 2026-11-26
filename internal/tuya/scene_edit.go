package tuya

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Creating and editing scenes, on top of the same Scene Linkage Rules service
// the read side uses.
//
// Two facts shape this file, both measured against the live cloud:
//
//  1. A create is validated field by field, in order: space_id, name, type,
//     actions, and then each action's executor. A missing field is answered
//     with 40003001 naming it, so a mistake is reported precisely.
//  2. A scene returned by the detail endpoint carries fields this code does not
//     model — conditions, running_mode, and actions of executors it cannot
//     build, such as the IR key press of a remote. An update therefore sends
//     back the document it read with only the changed fields replaced. Building
//     a fresh body from a typed struct would drop everything unmodelled, and a
//     tap-to-run scene silently becomes a different scene.

// ActionExecutor names how an action is carried out.
const (
	// ExecutorDevice writes a data point of a device.
	ExecutorDevice = "device_issue"
	// ExecutorIR presses a key of an infrared or RF remote. These are read and
	// preserved, never built: their key ids belong to a remote's own layout.
	ExecutorIR = "ir_issue_vii"
)

// SceneAction is one action of a scene.
type SceneAction struct {
	Executor string `json:"action_executor"`
	EntityID string `json:"entity_id"`
	// Property is kept loose: the shape depends on the executor, and a field
	// this code does not know must survive a round trip.
	Property map[string]any `json:"executor_property,omitempty"`
}

// FunctionCode and FunctionValue read the device action's data point.
func (a SceneAction) FunctionCode() string {
	if a.Property == nil {
		return ""
	}
	code, _ := a.Property["function_code"].(string)
	return code
}

// FunctionValue returns the value that will be written.
func (a SceneAction) FunctionValue() any {
	if a.Property == nil {
		return nil
	}
	return a.Property["function_value"]
}

// SceneDocument is a scene as the cloud stores it, including the parts this
// code does not model. Update sends it back whole.
type SceneDocument map[string]any

// SceneDetail is the typed view of a document, for the editor.
type SceneDetail struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Status      string        `json:"status"`
	RunningMode string        `json:"running_mode"`
	SpaceID     string        `json:"space_id"`
	Type        string        `json:"type"`
	Actions     []SceneAction `json:"actions"`
	// Conditions is kept raw: a scene has none, an automation has some, and this
	// editor is for scenes.
	Conditions []json.RawMessage `json:"conditions"`
	// Document is what was read, for a round-trip update.
	Document SceneDocument `json:"-"`
}

// GetScene reads one scene in full.
//
// The cloud answers 40003007 for some rules — "contains data that is currently
// not supported for querying". Such a scene cannot be edited safely, because
// there is nothing to send back unchanged, so the error is returned as is and
// the caller must refuse to edit it.
func (c *Client) GetScene(ctx context.Context, id string) (SceneDetail, error) {
	var doc SceneDocument
	path := "/v2.0/cloud/scene/rule/" + url.PathEscape(id)
	if err := c.call(ctx, http.MethodGet, path, nil, nil, &doc); err != nil {
		return SceneDetail{}, err
	}
	detail := SceneDetail{Document: doc}
	raw, err := json.Marshal(doc)
	if err != nil {
		return SceneDetail{}, fmt.Errorf("tuya: scene %s: %w", id, err)
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		return SceneDetail{}, fmt.Errorf("tuya: scene %s: %w", id, err)
	}
	return detail, nil
}

// CreateScene creates a scene and returns its id when the cloud reports one.
//
// The body is the document built by the caller: space_id, name, type "scene",
// conditions [] and the actions. The cloud validates it field by field, so a
// mistake comes back as a 40003001 naming the field.
func (c *Client) CreateScene(ctx context.Context, doc SceneDocument) (string, error) {
	var res struct {
		ID string `json:"id"`
	}
	if err := c.call(ctx, http.MethodPost, "/v2.0/cloud/scene/rule", nil, doc, &res); err != nil {
		return "", err
	}
	return res.ID, nil
}

// UpdateScene writes a whole scene document back.
//
// The caller must pass the document it read, with only the intended fields
// changed. That is the whole point of this signature: a partial body would let
// the cloud fill the rest with defaults, and the scene would lose its
// conditions, its running mode or its unmodelled actions.
func (c *Client) UpdateScene(ctx context.Context, id string, doc SceneDocument) error {
	path := "/v2.0/cloud/scene/rule/" + url.PathEscape(id)
	return c.call(ctx, http.MethodPut, path, nil, doc, nil)
}

// SetSceneStatus enables or disables a scene through the same whole-document
// update: the cloud has no separate status route this project may call.
func (c *Client) SetSceneStatus(ctx context.Context, id string, enabled bool) error {
	detail, err := c.GetScene(ctx, id)
	if err != nil {
		return err
	}
	status := "disable"
	if enabled {
		status = "enable"
	}
	detail.Document["status"] = status
	return c.UpdateScene(ctx, id, detail.Document)
}
