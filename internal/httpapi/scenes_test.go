package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/scene"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// stubScenes is a scene cloud in memory for the HTTP layer.
type stubScenes struct {
	details map[string]tuya.SceneDetail
	// unreadable holds ids the cloud answers 40003007 for.
	unreadable map[string]bool
	created    []tuya.SceneDocument
	updated    []tuya.SceneDocument
}

func (s *stubScenes) Scenes(context.Context, string) ([]tuya.Scene, error) {
	out := make([]tuya.Scene, 0, len(s.details))
	for id, d := range s.details {
		out = append(out, tuya.Scene{ID: id, Name: d.Name, Status: d.Status})
	}
	return out, nil
}

func (s *stubScenes) SpaceID(context.Context) (string, error) { return "space-1", nil }

func (s *stubScenes) GetScene(_ context.Context, id string) (tuya.SceneDetail, error) {
	if s.unreadable[id] {
		return tuya.SceneDetail{}, &tuya.APIError{Code: 40003007, Msg: "not supported for querying"}
	}
	d, ok := s.details[id]
	if !ok {
		return tuya.SceneDetail{}, &tuya.APIError{Code: 40003001, Msg: "not found"}
	}
	return d, nil
}

func (s *stubScenes) CreateScene(_ context.Context, doc tuya.SceneDocument) (string, error) {
	s.created = append(s.created, doc)
	return "made-1", nil
}

func (s *stubScenes) UpdateScene(_ context.Context, _ string, doc tuya.SceneDocument) error {
	s.updated = append(s.updated, doc)
	return nil
}

// sceneHome is a snapshot with one lamp that has a thing model.
func sceneHome() func() home.Snapshot {
	lamp := home.DeviceView{Device: tuya.Device{ID: "lamp-1", Name: "Лампа", Online: true}}
	lamp.Spec = &tuya.Spec{Functions: []tuya.Function{{Code: "switch_led", Type: "Boolean"}}}
	return func() home.Snapshot { return home.Snapshot{Devices: []home.DeviceView{lamp}} }
}

func scenesServer(t *testing.T, stub *stubScenes) *httptest.Server {
	t.Helper()
	editor := scene.NewEditor(stub, "", sceneHome(), nil)
	return newTestServer(t, Options{Demo: true, SceneEditor: editor})
}

const lampScene = `{"name":"Вечерний свет","actions":[{"device":"lamp-1","code":"switch_led","value":true}]}`

func TestScenesOffIsExplained(t *testing.T) {
	srv := newTestServer(t, Options{Demo: true})
	resp, body := getJSON(t, srv.URL+"/api/scenes")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out scenesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Editable {
		t.Error("editable = true with no editor")
	}
	if out.Scenes == nil || len(out.Scenes) != 0 {
		t.Errorf("scenes = %v, want an empty list", out.Scenes)
	}
}

func TestSceneCreate(t *testing.T) {
	stub := &stubScenes{details: map[string]tuya.SceneDetail{}}
	srv := scenesServer(t, stub)

	resp, body := postJSON(t, srv.URL+"/api/scenes", lampScene)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if len(stub.created) != 1 {
		t.Fatalf("sent %d documents", len(stub.created))
	}
	doc := stub.created[0]
	if doc["name"] != "Вечерний свет" || doc["type"] != "scene" || doc["space_id"] != "space-1" {
		t.Errorf("document = %v", doc)
	}
	var out struct {
		ID     string               `json:"id"`
		Scenes []scene.EditableView `json:"scenes"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "made-1" {
		t.Errorf("id = %q, want the cloud's", out.ID)
	}
}

func TestSceneCreateRejectsABadDraft(t *testing.T) {
	stub := &stubScenes{details: map[string]tuya.SceneDetail{}}
	srv := scenesServer(t, stub)
	cases := map[string]string{
		"no name":     `{"actions":[{"device":"lamp-1","code":"switch_led","value":true}]}`,
		"no actions":  `{"name":"Пустая","actions":[]}`,
		"bad device":  `{"name":"x","actions":[{"device":"nope","code":"switch_led","value":true}]}`,
		"bad code":    `{"name":"x","actions":[{"device":"lamp-1","code":"нема","value":true}]}`,
		"bad value":   `{"name":"x","actions":[{"device":"lamp-1","code":"switch_led","value":"да"}]}`,
		"broken json": `{`,
	}
	for name, body := range cases {
		resp, raw := postJSON(t, srv.URL+"/api/scenes", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body = %s", name, resp.StatusCode, raw)
		}
	}
	if len(stub.created) != 0 {
		t.Errorf("a refused draft reached the cloud: %v", stub.created)
	}
}

func TestSceneUpdate(t *testing.T) {
	stub := &stubScenes{details: map[string]tuya.SceneDetail{
		"s1": {
			ID: "s1", Name: "старое", Status: "enable",
			Actions: []tuya.SceneAction{{
				Executor: tuya.ExecutorDevice, EntityID: "lamp-1",
				Property: map[string]any{"function_code": "switch_led", "function_value": false},
			}},
			Document: tuya.SceneDocument{
				"id": "s1", "name": "старое", "type": "scene", "status": "enable",
				"space_id": "space-1", "running_mode": "cloud", "conditions": []any{},
				"actions": []any{},
			},
		},
	}}
	srv := scenesServer(t, stub)

	resp, body := putJSON(t, srv.URL+"/api/scenes/s1", lampScene)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if len(stub.updated) != 1 {
		t.Fatalf("sent %d updates", len(stub.updated))
	}
	doc := stub.updated[0]
	if doc["name"] != "Вечерний свет" {
		t.Errorf("name = %v", doc["name"])
	}
	// The fields the editor does not model are carried over.
	if doc["running_mode"] != "cloud" {
		t.Errorf("running_mode lost: %v", doc["running_mode"])
	}
}

// A scene the cloud will not read back is answered as a condition with a place
// to go, not as a failed request: the scene works, it just cannot be edited.
func TestSceneUpdateOnUnreadableScene(t *testing.T) {
	stub := &stubScenes{
		details:    map[string]tuya.SceneDetail{"closed": {ID: "closed", Name: "закрытая"}},
		unreadable: map[string]bool{"closed": true},
	}
	srv := scenesServer(t, stub)

	resp, body := putJSON(t, srv.URL+"/api/scenes/closed", lampScene)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", resp.StatusCode, body)
	}
	var out map[string]string
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out["code"] != "scene_not_editable" {
		t.Errorf("code = %q", out["code"])
	}
	if out["link"] == "" {
		t.Error("no link to the console")
	}
	if len(stub.updated) != 0 {
		t.Error("a write was sent for a scene that cannot be read")
	}
}

func TestSceneEnabled(t *testing.T) {
	stub := &stubScenes{details: map[string]tuya.SceneDetail{
		"s1": {
			ID: "s1", Name: "с", Status: "enable",
			Document: tuya.SceneDocument{
				"id": "s1", "name": "с", "type": "scene", "status": "enable",
				"space_id": "space-1", "running_mode": "cloud", "conditions": []any{},
				"actions": []any{tuya.SceneAction{Executor: tuya.ExecutorDevice, EntityID: "lamp-1"}},
			},
		},
	}}
	srv := scenesServer(t, stub)

	resp, body := putJSON(t, srv.URL+"/api/scenes/s1/enabled", `{"enabled":false}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if len(stub.updated) != 1 || stub.updated[0]["status"] != "disable" {
		t.Fatalf("update = %v", stub.updated)
	}
}

func TestSceneEnabledNeedsTheField(t *testing.T) {
	stub := &stubScenes{details: map[string]tuya.SceneDetail{}}
	srv := scenesServer(t, stub)
	resp, raw := putJSON(t, srv.URL+"/api/scenes/s1/enabled", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "enabled") {
		t.Errorf("body = %s, want the missing field named", raw)
	}
}

func TestScenesListShowsWhatEachDoes(t *testing.T) {
	stub := &stubScenes{details: map[string]tuya.SceneDetail{
		"open": {
			ID: "open", Name: "открытая", Status: "enable",
			Actions: []tuya.SceneAction{
				{Executor: tuya.ExecutorDevice, EntityID: "lamp-1",
					Property: map[string]any{"function_code": "switch_led", "function_value": true}},
				{Executor: tuya.ExecutorIR, EntityID: "blaster",
					Property: map[string]any{"function_code": "keyId", "function_value": "3"}},
			},
			Document: tuya.SceneDocument{"id": "open"},
		},
		"closed": {ID: "closed", Name: "закрытая", Status: "enable"},
	}, unreadable: map[string]bool{"closed": true}}
	srv := scenesServer(t, stub)

	_, body := getJSON(t, srv.URL+"/api/scenes")
	var out scenesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Scenes) != 2 {
		t.Fatalf("scenes = %+v", out.Scenes)
	}
	var open, closed scene.EditableView
	for _, v := range out.Scenes {
		switch v.ID {
		case "open":
			open = v
		case "closed":
			closed = v
		}
	}
	if !open.Editable || len(open.Actions) != 2 {
		t.Errorf("open = %+v", open)
	}
	if open.Actions[1].Fixed == false {
		t.Error("the IR action is not marked fixed")
	}
	if closed.Editable || closed.Reason == "" {
		t.Errorf("closed = %+v", closed)
	}
}
