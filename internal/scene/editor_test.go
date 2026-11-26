package scene

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// stubClient is a scene cloud in memory. It records what was sent so a test can
// check the document, and it can refuse to return a scene's content, which is
// what 40003007 looks like from here.
type stubClient struct {
	scenes  []tuya.Scene
	details map[string]tuya.SceneDetail
	// unreadable holds the ids the cloud answers 40003007 for.
	unreadable map[string]bool

	created []tuya.SceneDocument
	updated []tuya.SceneDocument
	updates []string
}

func (s *stubClient) Scenes(context.Context, string) ([]tuya.Scene, error) {
	return s.scenes, nil
}

func (s *stubClient) SpaceID(context.Context) (string, error) { return "space-1", nil }

func (s *stubClient) GetScene(_ context.Context, id string) (tuya.SceneDetail, error) {
	if s.unreadable[id] {
		return tuya.SceneDetail{}, &tuya.APIError{
			Code: 40003007,
			Msg:  "This rule contains data that is currently not supported for querying",
		}
	}
	detail, ok := s.details[id]
	if !ok {
		return tuya.SceneDetail{}, &tuya.APIError{Code: 40003001, Msg: "rule not found"}
	}
	return detail, nil
}

func (s *stubClient) CreateScene(_ context.Context, doc tuya.SceneDocument) (string, error) {
	s.created = append(s.created, doc)
	return "new-scene-id", nil
}

func (s *stubClient) UpdateScene(_ context.Context, id string, doc tuya.SceneDocument) error {
	s.updates = append(s.updates, id)
	s.updated = append(s.updated, doc)
	return nil
}

// aHome is a snapshot with a lamp and a plug, both with a thing model.
func aHome() func() home.Snapshot {
	lamp := home.DeviceView{Device: tuya.Device{ID: "lamp-1", Name: "Лампа", Online: true}}
	lamp.Spec = &tuya.Spec{
		Functions: []tuya.Function{
			{Code: "switch_led", Type: "Boolean"},
			{Code: "bright_value", Type: "Integer", Values: json.RawMessage(`{"min":10,"max":1000}`)},
			{Code: "work_mode", Type: "Enum", Values: json.RawMessage(`{"range":["white","colour"]}`)},
		},
	}
	plug := home.DeviceView{Device: tuya.Device{ID: "plug-1", Name: "Розетка", Online: true}}
	plug.Spec = &tuya.Spec{Functions: []tuya.Function{{Code: "switch_1", Type: "Boolean"}}}

	// A device the cloud will never describe: an RF switch behind a blaster.
	dumb := home.DeviceView{Device: tuya.Device{ID: "rf-1", Name: "Пульт", Online: true}}
	dumb.SpecUnsupported = true

	return func() home.Snapshot {
		return home.Snapshot{Devices: []home.DeviceView{lamp, plug, dumb}}
	}
}

func newEditor(t *testing.T, stub *stubClient) *Editor {
	t.Helper()
	return NewEditor(stub, "", aHome(), nil)
}

func TestCreateBuildsTheDocumentTheCloudExpects(t *testing.T) {
	stub := &stubClient{}
	e := newEditor(t, stub)

	id, err := e.Create(context.Background(), Draft{
		Name: "  Вечерний свет  ",
		Actions: []DraftAction{
			{Device: "lamp-1", Code: "switch_led", Value: true},
			{Device: "lamp-1", Code: "work_mode", Value: "colour"},
			{Device: "plug-1", Code: "switch_1", Value: true},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id != "new-scene-id" {
		t.Errorf("id = %q", id)
	}
	if len(stub.created) != 1 {
		t.Fatalf("sent %d documents", len(stub.created))
	}
	doc := stub.created[0]
	// The cloud validates these in order: space_id, name, type, actions.
	if doc["space_id"] != "space-1" || doc["type"] != "scene" {
		t.Errorf("space_id/type = %v/%v", doc["space_id"], doc["type"])
	}
	if doc["name"] != "Вечерний свет" {
		t.Errorf("name = %q, want it trimmed", doc["name"])
	}
	if _, ok := doc["conditions"]; !ok {
		t.Error("conditions missing: the cloud would reject the body")
	}
	actions, ok := doc["actions"].([]any)
	if !ok || len(actions) != 3 {
		t.Fatalf("actions = %#v", doc["actions"])
	}
	first := actions[0].(tuya.SceneAction)
	if first.Executor != tuya.ExecutorDevice || first.EntityID != "lamp-1" {
		t.Errorf("executor/entity = %q/%q", first.Executor, first.EntityID)
	}
	if first.FunctionCode() != "switch_led" || first.FunctionValue() != true {
		t.Errorf("property = %v", first.Property)
	}
}

func TestCreateRefusesDrafts(t *testing.T) {
	cases := map[string]Draft{
		"no name":        {Actions: []DraftAction{{Device: "lamp-1", Code: "switch_led", Value: true}}},
		"blank name":     {Name: "   ", Actions: []DraftAction{{Device: "lamp-1", Code: "switch_led", Value: true}}},
		"long name":      {Name: strings.Repeat("я", MaxNameLen+1), Actions: []DraftAction{{Device: "lamp-1", Code: "switch_led", Value: true}}},
		"no actions":     {Name: "Пустая"},
		"unknown device": {Name: "x", Actions: []DraftAction{{Device: "nope", Code: "switch_1", Value: true}}},
		"unknown code":   {Name: "x", Actions: []DraftAction{{Device: "lamp-1", Code: "нет", Value: true}}},
		"wrong type":     {Name: "x", Actions: []DraftAction{{Device: "lamp-1", Code: "switch_led", Value: "true"}}},
		"out of range":   {Name: "x", Actions: []DraftAction{{Device: "lamp-1", Code: "bright_value", Value: float64(5000)}}},
		"bad enum":       {Name: "x", Actions: []DraftAction{{Device: "lamp-1", Code: "work_mode", Value: "rainbow"}}},
		"no value":       {Name: "x", Actions: []DraftAction{{Device: "lamp-1", Code: "switch_led"}}},
		// A device whose thing model the cloud never describes cannot be driven
		// from here; the scene would be accepted and then fail silently.
		"no thing model": {Name: "x", Actions: []DraftAction{{Device: "rf-1", Code: "scene_1", Value: "scene"}}},
	}
	for name, draft := range cases {
		if _, err := newEditor(t, &stubClient{}).Create(context.Background(), draft); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

// A scene cannot contain a scene: the cloud would accept it and the nesting
// would do something nobody asked for.
func TestCreateRefusesSceneInScene(t *testing.T) {
	snap := aHome()
	base := snap()
	base.Devices = append(base.Devices, home.DeviceView{
		Device: tuya.Device{ID: "other-scene", Name: "Сцена-сцена"},
	})
	// Marked by category, which is how the provider labels a scene.
	base.Devices[len(base.Devices)-1].Category = Category
	e := NewEditor(&stubClient{}, "", func() home.Snapshot { return base }, nil)

	_, err := e.Create(context.Background(), Draft{
		Name:    "Вложенная",
		Actions: []DraftAction{{Device: "other-scene", Code: "trigger", Value: true}},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

// The update sends back what it read, changing only the name and the device
// actions. Anything the editor does not model — an IR key press, a running mode,
// a condition — must survive, or a tap-to-run scene quietly becomes another one.
func TestUpdatePreservesWhatItDoesNotModel(t *testing.T) {
	irAction := tuya.SceneAction{
		Executor: tuya.ExecutorIR,
		EntityID: "blaster-1",
		Property: map[string]any{"function_code": "keyId", "function_value": "12"},
	}
	deviceAction := tuya.SceneAction{
		Executor: tuya.ExecutorDevice,
		EntityID: "lamp-1",
		Property: map[string]any{"function_code": "switch_led", "function_value": false},
	}
	doc := tuya.SceneDocument{
		"id": "scene-1", "name": "старое имя", "status": "enable", "type": "scene",
		"space_id": "space-1", "running_mode": "cloud",
		"conditions": []any{},
		"actions":    []any{irAction, deviceAction},
	}
	stub := &stubClient{
		scenes: []tuya.Scene{{ID: "scene-1", Name: "старое имя", Status: "enable"}},
		details: map[string]tuya.SceneDetail{
			"scene-1": {
				ID: "scene-1", Name: "старое имя", Status: "enable", RunningMode: "cloud",
				Actions: []tuya.SceneAction{irAction, deviceAction}, Document: doc,
			},
		},
	}
	e := newEditor(t, stub)

	err := e.Update(context.Background(), "scene-1", Draft{
		Name:    "новое имя",
		Actions: []DraftAction{{Device: "plug-1", Code: "switch_1", Value: true}},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	sent := stub.updated[0]
	if sent["name"] != "новое имя" {
		t.Errorf("name = %v", sent["name"])
	}
	// Unmodelled fields survive.
	if sent["running_mode"] != "cloud" {
		t.Errorf("running_mode = %v, want it carried over", sent["running_mode"])
	}
	if _, ok := sent["conditions"]; !ok {
		t.Error("conditions were dropped")
	}
	if _, ok := sent["space_id"]; !ok {
		t.Error("space_id was dropped")
	}
	// The id belongs to the path, not the body.
	if _, ok := sent["id"]; ok {
		t.Error("id was sent in the body")
	}
	// The IR action keeps its place; the device action was replaced.
	actions := sent["actions"].([]any)
	if len(actions) != 2 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[0].(tuya.SceneAction).Executor != tuya.ExecutorIR {
		t.Errorf("the IR action moved or was lost: %#v", actions[0])
	}
	replaced := actions[1].(tuya.SceneAction)
	if replaced.EntityID != "plug-1" || replaced.FunctionCode() != "switch_1" {
		t.Errorf("device action = %#v", replaced)
	}
}

// A scene whose content the cloud will not return cannot be written safely: any
// body would be a guess about what it contained.
func TestUpdateRefusesAnUnreadableScene(t *testing.T) {
	stub := &stubClient{
		scenes:     []tuya.Scene{{ID: "closed", Name: "закрытая"}},
		unreadable: map[string]bool{"closed": true},
	}
	e := newEditor(t, stub)
	err := e.Update(context.Background(), "closed", Draft{
		Name:    "x",
		Actions: []DraftAction{{Device: "lamp-1", Code: "switch_led", Value: true}},
	})
	if !errors.Is(err, ErrNotEditable) {
		t.Fatalf("err = %v, want ErrNotEditable", err)
	}
	if len(stub.updated) != 0 {
		t.Fatal("a write was sent for a scene whose content could not be read")
	}
	// The message says where to go instead of naming a code.
	if !strings.Contains(err.Error(), "Tuya") {
		t.Errorf("message = %q, want it to point at the app", err)
	}
}

// Validation happens before the read, so a bad draft never reaches the cloud.
func TestUpdateValidatesBeforeReading(t *testing.T) {
	stub := &stubClient{
		scenes:  []tuya.Scene{{ID: "s1", Name: "s"}},
		details: map[string]tuya.SceneDetail{"s1": {ID: "s1"}},
	}
	e := newEditor(t, stub)
	err := e.Update(context.Background(), "s1", Draft{Name: ""})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if len(stub.updates) != 0 {
		t.Error("a write was attempted for an invalid draft")
	}
}

// Order is part of a scene's meaning, so the merger keeps the non-device actions
// where they were while filling the device slots in order.
func TestMergeActionsKeepsOrder(t *testing.T) {
	ir := func(key string) tuya.SceneAction {
		return tuya.SceneAction{Executor: tuya.ExecutorIR, EntityID: "blaster",
			Property: map[string]any{"function_code": "keyId", "function_value": key}}
	}
	dev := func(entity string) tuya.SceneAction {
		return tuya.SceneAction{Executor: tuya.ExecutorDevice, EntityID: entity,
			Property: map[string]any{"function_code": "switch_1", "function_value": true}}
	}

	existing := []tuya.SceneAction{dev("a"), ir("1"), dev("b")}
	edited := []tuya.SceneAction{dev("x"), dev("y")}
	got := mergeActions(existing, edited)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].(tuya.SceneAction).EntityID != "x" {
		t.Errorf("slot 0 = %v", got[0])
	}
	if got[1].(tuya.SceneAction).Executor != tuya.ExecutorIR {
		t.Errorf("the IR action did not keep its place: %v", got[1])
	}
	if got[2].(tuya.SceneAction).EntityID != "y" {
		t.Errorf("slot 2 = %v", got[2])
	}

	// Fewer device actions than slots: the surplus slot disappears.
	got = mergeActions(existing, []tuya.SceneAction{dev("only")})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	// More edited than slots: they go at the end.
	got = mergeActions([]tuya.SceneAction{dev("a")}, []tuya.SceneAction{dev("x"), dev("y")})
	if len(got) != 2 || got[1].(tuya.SceneAction).EntityID != "y" {
		t.Errorf("extra action was lost: %v", got)
	}
}

// Describe reports an unreadable scene as present but not editable, with a
// reason: hiding it would make the page disagree with the app.
func TestDescribeExplainsUnreadableScenes(t *testing.T) {
	stub := &stubClient{
		scenes: []tuya.Scene{
			{ID: "open", Name: "открытая", Status: "enable"},
			{ID: "closed", Name: "закрытая", Status: "enable"},
		},
		details: map[string]tuya.SceneDetail{
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
		},
		unreadable: map[string]bool{"closed": true},
	}
	views, err := newEditor(t, stub).Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("views = %d", len(views))
	}
	open, closed := views[0], views[1]
	if !open.Editable || len(open.Actions) != 2 {
		t.Fatalf("open = %+v", open)
	}
	if open.Actions[0].Device != "lamp-1" || open.Actions[0].Code != "switch_led" {
		t.Errorf("device action = %+v", open.Actions[0])
	}
	// An IR action is shown as fixed, not as a device the editor could change.
	if !open.Actions[1].Fixed || open.Actions[1].Label == "" {
		t.Errorf("IR action = %+v", open.Actions[1])
	}
	if closed.Editable || closed.Reason == "" {
		t.Errorf("closed = %+v, want not editable with a reason", closed)
	}
	if !strings.Contains(closed.Reason, "Tuya") {
		t.Errorf("reason = %q, want it to point somewhere", closed.Reason)
	}
}

func TestSetStatusWritesTheWholeDocument(t *testing.T) {
	doc := tuya.SceneDocument{
		"id": "s1", "name": "с", "status": "enable", "type": "scene",
		"space_id": "space-1", "running_mode": "cloud", "conditions": []any{},
		"actions": []any{tuya.SceneAction{Executor: tuya.ExecutorDevice, EntityID: "lamp-1"}},
	}
	stub := &stubClient{
		scenes:  []tuya.Scene{{ID: "s1", Name: "с", Status: "enable"}},
		details: map[string]tuya.SceneDetail{"s1": {ID: "s1", Document: doc}},
	}
	if err := newEditor(t, stub).SetStatus(context.Background(), "s1", false); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	sent := stub.updated[0]
	if sent["status"] != "disable" {
		t.Errorf("status = %v", sent["status"])
	}
	if _, ok := sent["actions"]; !ok {
		t.Error("the actions were dropped: disabling would have emptied the scene")
	}
	if _, ok := sent["id"]; ok {
		t.Error("id was sent in the body")
	}
}
