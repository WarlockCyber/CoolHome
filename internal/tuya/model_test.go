package tuya

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The models below are verbatim excerpts of what the cloud answered for the
// devices on one real home, trimmed to the properties under test. Parsing tests
// use them so the mapping is checked against the real shape, not a guess.

const modelTandH = `{"modelId":"fz4og8","services":[{"actions":[],"code":"","events":[],
"name":"默认服务","properties":[
{"abilityId":20,"accessMode":"rw","code":"temp_unit_convert","name":"温标切换","typeSpec":{"range":["c","f"],"type":"enum"}},
{"abilityId":27,"accessMode":"ro","code":"temp_current","name":"温度","typeSpec":{"max":600,"min":-200,"scale":1,"step":1,"type":"value","unit":"℃"}},
{"abilityId":46,"accessMode":"ro","code":"humidity_value","name":"湿度","typeSpec":{"max":100,"min":0,"scale":0,"step":1,"type":"value","unit":"%"}},
{"abilityId":101,"accessMode":"ro","code":"battery_state","name":"电池电量状态","typeSpec":{"range":["low","middle","high"],"type":"enum"}}
]}]}`

const modelBlaster = `{"modelId":"000004t1bk","services":[{"properties":[
{"abilityId":201,"accessMode":"rw","code":"ir_send","name":"红外控制下发","typeSpec":{"maxlen":3072,"type":"string"}},
{"abilityId":202,"accessMode":"ro","code":"ir_study_code","name":"红外学习值上报","typeSpec":{"maxlen":128,"type":"raw"}}
]}]}`

// An RF switch: four learned keys, each a one-option enum, plus the IR payload.
const modelRFSwitch = `{"modelId":"000002vhjm","services":[{"properties":[
{"abilityId":1,"accessMode":"rw","code":"scene_1","name":"场景1","typeSpec":{"range":["scene"],"type":"enum"}},
{"abilityId":2,"accessMode":"rw","code":"scene_2","name":"场景2","typeSpec":{"range":["scene"],"type":"enum"}},
{"abilityId":201,"accessMode":"rw","code":"ir_send","name":"红外控制下发","typeSpec":{"maxlen":3072,"type":"string"}},
{"abilityId":202,"accessMode":"ro","code":"ir_study_code","name":"红外学习值上报","typeSpec":{"maxlen":128,"type":"raw"}}
]}]}`

// An IR air conditioner carries both real controls and the IR transport.
const modelIRAC = `{"modelId":"000004t25f","services":[{"properties":[
{"abilityId":101,"accessMode":"wr","code":"switch_power","name":"开关","typeSpec":{"type":"bool"}},
{"abilityId":102,"accessMode":"rw","code":"mode","name":"工作模式","typeSpec":{"range":["0","1","2","3","4"],"type":"enum"}},
{"abilityId":103,"accessMode":"wr","code":"temperature","name":"温度","typeSpec":{"max":40,"min":10,"scale":0,"step":1,"type":"value"}},
{"abilityId":201,"accessMode":"rw","code":"ir_send","name":"红外控制下发","typeSpec":{"maxlen":3072,"type":"string"}}
]}]}`

func TestParseThingModelReadsSensor(t *testing.T) {
	spec, err := ParseThingModel(modelTandH)
	if err != nil {
		t.Fatalf("ParseThingModel: %v", err)
	}
	if len(spec.Functions) != 1 || spec.Functions[0].Code != "temp_unit_convert" {
		t.Fatalf("functions = %+v, want only temp_unit_convert", spec.Functions)
	}
	if spec.Functions[0].Type != "Enum" {
		t.Errorf("temp_unit_convert type = %q, want Enum", spec.Functions[0].Type)
	}
	if got := spec.Functions[0].ValueSpec().Range; len(got) != 2 || got[0] != "c" || got[1] != "f" {
		t.Errorf("temp_unit_convert range = %v, want [c f]", got)
	}

	byCode := map[string]Function{}
	for _, f := range spec.Status {
		byCode[f.Code] = f
	}
	if len(spec.Status) != 3 {
		t.Fatalf("status = %+v, want three points", spec.Status)
	}
	temp, ok := byCode["temp_current"]
	if !ok {
		t.Fatal("temp_current is not a status point")
	}
	if temp.Type != "Integer" {
		t.Errorf("temp_current type = %q, want Integer", temp.Type)
	}
	vs := temp.ValueSpec()
	if vs.Scale != 1 || vs.Unit != "℃" || vs.Min == nil || *vs.Min != -200 || vs.Max == nil || *vs.Max != 600 {
		t.Errorf("temp_current domain = %+v, want scale 1, ℃, -200..600", vs)
	}
	if byCode["humidity_value"].ValueSpec().Unit != "%" {
		t.Errorf("humidity_value unit = %q, want %%", byCode["humidity_value"].ValueSpec().Unit)
	}
	if got := byCode["battery_state"].ValueSpec().Range; len(got) != 3 {
		t.Errorf("battery_state range = %v, want three options", got)
	}
}

func TestParseThingModelDropsProtocolPayloads(t *testing.T) {
	// An IR blaster owns nothing but the transport: a command blob and a
	// learned-code blob. Presenting either as a control would be nonsense.
	spec, err := ParseThingModel(modelBlaster)
	if err != nil {
		t.Fatalf("ParseThingModel: %v", err)
	}
	if len(spec.Functions) != 0 || len(spec.Status) != 0 {
		t.Fatalf("spec = %+v, want nothing presentable", spec)
	}
}

func TestParseThingModelDropsSingleOptionEnum(t *testing.T) {
	// An RF switch stores four keys as `scene_N = "scene"`: a fixed command,
	// not a choice. Four buttons all reading "scene" would be worse than the
	// honest "cloud does not expose this" note.
	spec, err := ParseThingModel(modelRFSwitch)
	if err != nil {
		t.Fatalf("ParseThingModel: %v", err)
	}
	if len(spec.Functions) != 0 || len(spec.Status) != 0 {
		t.Fatalf("spec = %+v, want nothing presentable", spec)
	}
}

func TestParseThingModelKeepsAirConditionerControls(t *testing.T) {
	spec, err := ParseThingModel(modelIRAC)
	if err != nil {
		t.Fatalf("ParseThingModel: %v", err)
	}
	byCode := map[string]Function{}
	for _, f := range spec.Functions {
		byCode[f.Code] = f
	}
	for _, code := range []string{"switch_power", "mode", "temperature"} {
		if _, ok := byCode[code]; !ok {
			t.Errorf("%s is missing from functions: %+v", code, spec.Functions)
		}
	}
	if _, ok := byCode["ir_send"]; ok {
		t.Error("ir_send must not become a control")
	}
	if byCode["switch_power"].Type != "Boolean" || byCode["temperature"].Type != "Integer" {
		t.Errorf("types = %q / %q, want Boolean / Integer",
			byCode["switch_power"].Type, byCode["temperature"].Type)
	}
	if vs := byCode["temperature"].ValueSpec(); vs.Min == nil || *vs.Min != 10 || vs.Max == nil || *vs.Max != 40 {
		t.Errorf("temperature domain = %+v, want 10..40", vs)
	}
}

func TestParseThingModelRejectsGarbage(t *testing.T) {
	if _, err := ParseThingModel("not json"); err == nil {
		t.Fatal("expected an error for a non-JSON model")
	}
}

// cloudStub answers the endpoints the fallback needs, and counts calls so a
// test can prove the legacy path was not bypassed.
type cloudStub struct {
	mu       sync.Mutex
	calls    map[string]int
	legacy   func(w http.ResponseWriter)
	model    string
	shadow   string
	device   string
	commands func(w http.ResponseWriter, r *http.Request)
	issue    func(w http.ResponseWriter, r *http.Request)
}

func (s *cloudStub) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if s.calls == nil {
			s.calls = map[string]int{}
		}
		s.calls[r.URL.Path]++
		s.mu.Unlock()
		switch r.URL.Path {
		case "/v1.0/token":
			io.WriteString(w, `{"success":true,"result":{"access_token":"token-a","expire_time":7200,"uid":"uid-1"},"t":0,"tid":"t1"}`)
		case "/v1.0/devices/dev-1/specifications":
			if s.legacy != nil {
				s.legacy(w)
				return
			}
			io.WriteString(w, `{"success":false,"code":"2009","msg":"not support this device","t":0,"tid":"tid-1"}`)
		case "/v1.0/devices/dev-1":
			io.WriteString(w, s.device)
		case "/v1.0/devices/dev-1/commands":
			if s.commands != nil {
				s.commands(w, r)
				return
			}
			io.WriteString(w, `{"success":false,"code":"2008","msg":"command or value not support","t":0,"tid":"tid-11"}`)
		case "/v2.0/cloud/thing/dev-1/shadow/properties/issue":
			if s.issue != nil {
				s.issue(w, r)
				return
			}
			io.WriteString(w, `{"success":true,"result":{},"t":0,"tid":"tid-12"}`)
		case "/v2.0/cloud/thing/dev-1/model":
			if s.model == "" {
				io.WriteString(w, `{"success":false,"code":"1108","msg":"uri path invalid","t":0,"tid":"tid-2"}`)
				return
			}
			io.WriteString(w, `{"success":true,"result":{"model":`+quote(s.model)+`},"t":0,"tid":"tid-3"}`)
		case "/v2.0/cloud/thing/dev-1/shadow/properties":
			if s.shadow == "" {
				io.WriteString(w, `{"success":false,"code":"1108","msg":"uri path invalid","t":0,"tid":"tid-4"}`)
				return
			}
			io.WriteString(w, `{"success":true,"result":`+s.shadow+`,"t":0,"tid":"tid-5"}`)
		default:
			http.NotFound(w, r)
		}
	})
}

func (s *cloudStub) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[path]
}

// quote escapes a JSON document for embedding as a JSON string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n', '\r', '\t':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func TestSpecFallsBackToThingModel(t *testing.T) {
	stub := &cloudStub{model: modelTandH}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	spec, err := c.Spec(context.Background(), "dev-1")
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if len(spec.Status) != 3 {
		t.Fatalf("status = %+v, want three points from the thing model", spec.Status)
	}
	if stub.count("/v1.0/devices/dev-1/specifications") != 1 {
		t.Error("the legacy endpoint must be tried first")
	}
	if stub.count("/v2.0/cloud/thing/dev-1/model") != 1 {
		t.Error("the thing model must be fetched once")
	}
}

func TestSpecKeepsUnsupportedWhenModelHasNothing(t *testing.T) {
	stub := &cloudStub{model: modelBlaster}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	_, err := c.Spec(context.Background(), "dev-1")
	if err == nil {
		t.Fatal("expected the unsupported error to survive an empty model")
	}
	if !IsUnsupportedDevice(err) {
		t.Fatalf("error = %v, want the original 2009", err)
	}
}

func TestSpecDoesNotUseTheModelForOtherFailures(t *testing.T) {
	stub := &cloudStub{model: modelTandH, legacy: func(w http.ResponseWriter) {
		io.WriteString(w, `{"success":false,"code":"1004","msg":"sign invalid","t":0,"tid":"tid-6"}`)
	}}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	_, err := c.Spec(context.Background(), "dev-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 1004 {
		t.Fatalf("error = %v, want the 1004 to be reported as is", err)
	}
	if stub.count("/v2.0/cloud/thing/dev-1/model") != 0 {
		t.Error("a signing error is not a reason to try the thing model")
	}
}

func TestSpecReturnsLegacyWhenItWorks(t *testing.T) {
	stub := &cloudStub{model: modelTandH, legacy: func(w http.ResponseWriter) {
		io.WriteString(w, `{"success":true,"result":{"category":"cz","functions":[{"code":"switch_1","type":"Boolean","values":"{}"}]},"t":0,"tid":"tid-7"}`)
	}}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	spec, err := c.Spec(context.Background(), "dev-1")
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if len(spec.Functions) != 1 || spec.Functions[0].Code != "switch_1" {
		t.Fatalf("spec = %+v, want the legacy answer", spec)
	}
	if stub.count("/v2.0/cloud/thing/dev-1/model") != 0 {
		t.Error("the thing model must not be fetched when the legacy spec works")
	}
}

func TestDeviceFillsStatusFromShadow(t *testing.T) {
	stub := &cloudStub{
		device: `{"success":true,"result":{"id":"dev-1","name":"T & H Sensor","status":[]},"t":0,"tid":"tid-8"}`,
		shadow: `{"properties":[
			{"code":"temp_current","type":"value","value":110},
			{"code":"humidity_value","type":"value","value":52},
			{"code":"ir_study_code","type":"raw","value":null}]}`,
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	d, err := c.Device(context.Background(), "dev-1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if len(d.Status) != 2 {
		t.Fatalf("status = %+v, want two reported values", d.Status)
	}
	// Numbers arrive as float64 from JSON; the UI compares them as numbers.
	if d.Status[0].Code != "temp_current" || d.Status[0].Value != float64(110) {
		t.Errorf("first status = %+v, want temp_current=110", d.Status[0])
	}
	if d.Status[1].Code != "humidity_value" {
		t.Errorf("second status = %+v, want humidity_value", d.Status[1])
	}
}

func TestDeviceKeepsLegacyStatusWithoutShadow(t *testing.T) {
	stub := &cloudStub{
		device: `{"success":true,"result":{"id":"dev-1","name":"Lamp","status":[{"code":"switch_led","value":true}]},"t":0,"tid":"tid-9"}`,
		shadow: `{"properties":[{"code":"switch_led","value":false}]}`,
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	d, err := c.Device(context.Background(), "dev-1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if len(d.Status) != 1 || d.Status[0].Value != true {
		t.Fatalf("status = %+v, want the legacy answer", d.Status)
	}
	if stub.count("/v2.0/cloud/thing/dev-1/shadow/properties") != 0 {
		t.Error("the shadow must not be asked when the device reports status")
	}
}

func TestDeviceIgnoresShadowFailure(t *testing.T) {
	// A device with no status and no shadow is not an error: it is a device
	// with nothing to report, and the card says so.
	stub := &cloudStub{
		device: `{"success":true,"result":{"id":"dev-1","name":"Blaster","status":[]},"t":0,"tid":"tid-10"}`,
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	d, err := c.Device(context.Background(), "dev-1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if len(d.Status) != 0 {
		t.Fatalf("status = %+v, want none", d.Status)
	}
	if d.Name != "Blaster" {
		t.Errorf("name = %q, want the device kept", d.Name)
	}
}

func TestSpecReportsTheRealCauseWhenTheModelFails(t *testing.T) {
	// IoT Core not subscribed is a deployment step, not a property of the
	// device. Reported as 2009 it would be cached for a day and shown as
	// "this device has no data points".
	stub := &cloudStub{model: "", legacy: func(w http.ResponseWriter) {
		io.WriteString(w, `{"success":false,"code":"2009","msg":"not support this device","t":0,"tid":"tid-13"}`)
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2.0/cloud/thing/dev-1/model" {
			io.WriteString(w, `{"success":false,"code":"28841101","msg":"No permissions. This API is not subscribed.","t":0,"tid":"tid-14"}`)
			return
		}
		stub.handler(t).ServeHTTP(w, r)
	}))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	_, err := c.Spec(context.Background(), "dev-1")
	if !IsServiceNotSubscribed(err) {
		t.Fatalf("error = %v, want the unsubscribed service to be reported", err)
	}
	if IsUnsupportedDevice(err) {
		t.Error("a missing subscription must never be classified as an unsupported device")
	}
}

func TestCommandUsesThingModelForAFallbackDevice(t *testing.T) {
	var gotBody map[string]map[string]any
	stub := &cloudStub{
		model: modelTandH,
		issue: func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			verifySign(t, r, body, "token-a")
			json.Unmarshal(body, &gotBody)
			io.WriteString(w, `{"success":true,"result":{},"t":0,"tid":"tid-15"}`)
		},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	// Spec is what marks the device as living on the thing model.
	if _, err := c.Spec(context.Background(), "dev-1"); err != nil {
		t.Fatalf("Spec: %v", err)
	}
	err := c.Command(context.Background(), "dev-1", []Command{{Code: "temp_unit_convert", Value: "f"}})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if got := gotBody["properties"]["temp_unit_convert"]; got != "f" {
		t.Fatalf("properties = %+v, want temp_unit_convert=f", gotBody)
	}
	if stub.count("/v2.0/cloud/thing/dev-1/shadow/properties/issue") != 1 {
		t.Error("the IoT Core write must be issued exactly once")
	}
}

func TestCommandKeepsLegacySurfaceForLegacyDevices(t *testing.T) {
	// A 2008 on a device the thing model does not describe means a wrong value,
	// not a different surface: retrying it through a path that does not
	// validate would let a bad command reach the device.
	var (
		mu        sync.Mutex
		legacyHit bool
	)
	stub := &cloudStub{
		legacy: func(w http.ResponseWriter) {
			io.WriteString(w, `{"success":true,"result":{"category":"cz","functions":[{"code":"switch_1","type":"Boolean"}]},"t":0,"tid":"tid-16"}`)
		},
		commands: func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			legacyHit = true
			mu.Unlock()
			io.WriteString(w, `{"success":false,"code":"2008","msg":"command or value not support","t":0,"tid":"tid-17"}`)
		},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	if _, err := c.Spec(context.Background(), "dev-1"); err != nil {
		t.Fatalf("Spec: %v", err)
	}
	err := c.Command(context.Background(), "dev-1", []Command{{Code: "switch_1", Value: true}})
	if err == nil {
		t.Fatal("expected the legacy refusal to be reported")
	}
	if stub.count("/v2.0/cloud/thing/dev-1/shadow/properties/issue") != 0 {
		t.Error("a legacy device must not be retried on the IoT Core write path")
	}
	mu.Lock()
	defer mu.Unlock()
	if !legacyHit {
		t.Error("the legacy command endpoint must be tried first")
	}
}

func TestCommandWritesThroughLegacyWhenItWorks(t *testing.T) {
	var body map[string][]Command
	stub := &cloudStub{
		commands: func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			json.Unmarshal(raw, &body)
			io.WriteString(w, `{"success":true,"result":true,"t":0,"tid":"tid-18"}`)
		},
	}
	srv := httptest.NewServer(stub.handler(t))
	defer srv.Close()

	now := time.Unix(1700000000, 0)
	c := newTestClient(t, srv, &now)
	if err := c.Command(context.Background(), "dev-1", []Command{{Code: "switch_1", Value: true}}); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if len(body["commands"]) != 1 || body["commands"][0].Code != "switch_1" {
		t.Fatalf("body = %+v, want one switch_1 command", body)
	}
	if stub.count("/v2.0/cloud/thing/dev-1/shadow/properties/issue") != 0 {
		t.Error("IoT Core must not be used when the legacy endpoint works")
	}
}
