package ir

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// stub is a fake IR client. Every call is recorded so a test can assert that a
// repeated refresh did not re-ask the cloud.
type stub struct {
	blasters []string
	remotes  map[string][]tuya.IRRemote
	keys     map[string][]tuya.IRKey
	sent     []sentKey

	// subs are the blaster's own devices: learned remotes such as RF switches.
	subs map[string][]tuya.IRRemote

	blasterErr error
	remoteErr  error
	subErr     error
	keyErr     error
	sendErr    error

	blasterCalls int
	remoteCalls  int
	subCalls     int
	keyCalls     int
	sendCalls    int
}

type sentKey struct {
	blaster, remote string
	key             tuya.IRKey
}

func (s *stub) Blasters(context.Context) ([]string, error) {
	s.blasterCalls++
	return s.blasters, s.blasterErr
}

func (s *stub) IRRemotes(_ context.Context, blaster string) ([]tuya.IRRemote, error) {
	s.remoteCalls++
	if s.remoteErr != nil {
		return nil, s.remoteErr
	}
	return s.remotes[blaster], nil
}

func (s *stub) IRSubDevices(_ context.Context, blaster string) ([]tuya.IRRemote, error) {
	s.subCalls++
	if s.subErr != nil {
		return nil, s.subErr
	}
	return s.subs[blaster], nil
}

func (s *stub) IRKeys(_ context.Context, blaster, remote string) ([]tuya.IRKey, error) {
	s.keyCalls++
	if s.keyErr != nil {
		return nil, s.keyErr
	}
	return s.keys[blaster+"/"+remote], nil
}

func (s *stub) IRSendKey(_ context.Context, blaster, remote string, key tuya.IRKey) error {
	s.sendCalls++
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sent = append(s.sent, sentKey{blaster: blaster, remote: remote, key: key})
	return nil
}

func newStub() *stub {
	return &stub{
		blasters: []string{"blast-1"},
		remotes: map[string][]tuya.IRRemote{
			"blast-1": {
				{ID: "r-1", Name: "Телевизор", CategoryID: 2, Index: 449101998},
				{ID: "r-2", Name: "Кондиционер", CategoryID: 5, Index: 449101999},
			},
		},
		keys: map[string][]tuya.IRKey{
			"blast-1/r-1": {
				{Key: "PowerOn", Name: "power on", Standard: true},
				{Key: "PowerOff", Name: "power off", Standard: true},
			},
			"blast-1/r-2": {
				{Key: "PowerOn", Name: "power on", Standard: true},
			},
		},
	}
}

func newProvider(t *testing.T, s *stub, now *time.Time) *Provider {
	t.Helper()
	p := New(s, Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if now != nil {
		p.now = func() time.Time { return *now }
	}
	return p
}

func TestListDevicesExposesRemotes(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	devices, err := p.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(devices))
	}
	if devices[0].ID != "r-1" || devices[0].Name != "Телевизор" {
		t.Fatalf("first device = %+v", devices[0])
	}
	if devices[0].Category != "ir_remote" {
		t.Errorf("category = %q", devices[0].Category)
	}
}

func TestListDevicesFiltersById(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	devices, err := p.ListDevices(context.Background(), []string{"r-2"})
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devices) != 1 || devices[0].Name != "Кондиционер" {
		t.Fatalf("devices = %+v", devices)
	}
}

func TestBlastersAreDiscoveredOnce(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	for i := 0; i < 3; i++ {
		if _, err := p.ListDevices(context.Background(), nil); err != nil {
			t.Fatalf("ListDevices: %v", err)
		}
	}
	if s.blasterCalls != 1 {
		t.Errorf("blaster discovery ran %d times, want 1", s.blasterCalls)
	}
}

func TestSpecTurnsKeysIntoButtons(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	spec, err := p.Spec(context.Background(), "r-1")
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if len(spec.Functions) != 2 {
		t.Fatalf("functions = %d, want 2", len(spec.Functions))
	}
	power := spec.Functions[0]
	if power.Code != "PowerOn" || power.Name != "power on" {
		t.Errorf("first function = %+v", power)
	}
	if power.Type != tuya.FunctionTypeButton {
		t.Errorf("type = %q, want %q", power.Type, tuya.FunctionTypeButton)
	}
}

func TestKeysAreCached(t *testing.T) {
	now := time.Unix(1700000000, 0)
	s := newStub()
	p := newProvider(t, s, &now)

	if _, err := p.Spec(context.Background(), "r-1"); err != nil {
		t.Fatalf("Spec: %v", err)
	}
	first := s.keyCalls
	if _, err := p.Spec(context.Background(), "r-1"); err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if s.keyCalls != first {
		t.Errorf("key calls = %d after a second Spec, want %d", s.keyCalls, first)
	}

	now = now.Add(keysTTL + time.Second)
	if _, err := p.Spec(context.Background(), "r-1"); err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if s.keyCalls == first {
		t.Error("keys should be refetched after the TTL")
	}
}

func TestCommandPressesTheNamedKey(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	err := p.Command(context.Background(), "r-1", []tuya.Command{{Code: "PowerOff", Value: true}})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if len(s.sent) != 1 {
		t.Fatalf("sent %d keys, want 1", len(s.sent))
	}
	got := s.sent[0]
	if got.blaster != "blast-1" || got.remote != "r-1" {
		t.Errorf("routed to %s/%s, want blast-1/r-1: the path uses remote_id", got.blaster, got.remote)
	}
	if got.key.Key != "PowerOff" {
		t.Errorf("key = %+v", got.key)
	}
}

func TestCommandRejectsAnUnknownKey(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	err := p.Command(context.Background(), "r-1", []tuya.Command{{Code: "nonexistent", Value: true}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if s.sendCalls != 0 {
		t.Errorf("sent %d keys, want 0", s.sendCalls)
	}
}

func TestCommandPropagatesACloudRefusal(t *testing.T) {
	s := newStub()
	s.sendErr = &tuya.APIError{Code: 1106, Msg: "permission deny"}
	p := newProvider(t, s, nil)

	err := p.Command(context.Background(), "r-1", []tuya.Command{{Code: "PowerOn", Value: true}})
	if err == nil {
		t.Fatal("expected an error")
	}
	var api *tuya.APIError
	if !errors.As(err, &api) || api.Code != 1106 {
		t.Fatalf("error = %v, want the API error wrapped", err)
	}
}

// An unsubscribed IR service must not fail the whole home; the provider goes
// quiet after saying so once.
func TestUnsubscribedServiceDisablesTheProvider(t *testing.T) {
	s := newStub()
	s.remoteErr = &tuya.APIError{Code: 28841101, Msg: "No permissions. This API is not subscribed."}
	p := newProvider(t, s, nil)

	devices, err := p.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices must not fail: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("devices = %+v, want none", devices)
	}

	calls := s.remoteCalls
	if _, err := p.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("second ListDevices: %v", err)
	}
	if s.remoteCalls != calls {
		t.Errorf("remote calls = %d, want %d: a disabled provider must stop asking", s.remoteCalls, calls)
	}
}

func TestNotFoundWrapsTheHomeSentinel(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	_, err := p.Device(context.Background(), "missing")
	if !errors.Is(err, home.ErrNotFound) {
		t.Fatalf("error = %v, want home.ErrNotFound", err)
	}
	if _, err := p.Device(context.Background(), "not-an-ir-id"); !errors.Is(err, home.ErrNotFound) {
		t.Errorf("error = %v, want home.ErrNotFound", err)
	}
}

func TestDeviceReturnsARemote(t *testing.T) {
	s := newStub()
	p := newProvider(t, s, nil)

	d, err := p.Device(context.Background(), "r-2")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if d.Name != "Кондиционер" || d.ID != "r-2" {
		t.Fatalf("device = %+v", d)
	}
}

// Addressing a remote by its index answers 1106, so the key must always be the
// remote_id even when an index is present.
func TestRemoteKeyIsAlwaysTheID(t *testing.T) {
	if got := remoteKey(tuya.IRRemote{Index: 449101998, ID: "bfa33762"}); got != "bfa33762" {
		t.Errorf("remoteKey = %q, want the remote_id", got)
	}
	if got := remoteKey(tuya.IRRemote{ID: "x"}); got != "x" {
		t.Errorf("remoteKey = %q, want x", got)
	}
}

func TestConfiguredBlastersSkipDiscovery(t *testing.T) {
	s := newStub()
	p := New(s, Options{Blasters: []string{"blast-1"}}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := p.ListDevices(context.Background(), nil); err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if s.blasterCalls != 0 {
		t.Errorf("blaster discovery ran %d times, want 0 when pinned", s.blasterCalls)
	}
}

// An RF switch is a learned remote: it never appears in the /remotes list, only
// as a sub-device of the blaster. Missing it was the whole reason these devices
// showed "no functions".
func TestLearnedRemoteIsServedFromSubDevices(t *testing.T) {
	online := true
	s := newStub()
	s.subs = map[string][]tuya.IRRemote{
		"blast-1": {
			{ID: "rf-1", Name: "Второй этаж", Category: "rf_switch1", Online: &online},
		},
	}
	s.keys["blast-1/rf-1"] = []tuya.IRKey{{Key: "one_on", Name: "one ON"}}
	p := newProvider(t, s, nil)

	devices, err := p.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	var found *tuya.Device
	for i := range devices {
		if devices[i].ID == "rf-1" {
			found = &devices[i]
		}
	}
	if found == nil {
		t.Fatalf("the RF switch is missing from %+v", devices)
	}
	if found.Name != "Второй этаж" || !found.Online {
		t.Errorf("device = %+v", found)
	}

	spec, err := p.Spec(context.Background(), "rf-1")
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if len(spec.Functions) != 1 || spec.Functions[0].Code != "one_on" {
		t.Fatalf("functions = %+v, want the one_on button", spec.Functions)
	}

	if err := p.Command(context.Background(), "rf-1", []tuya.Command{{Code: "one_on", Value: true}}); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if len(s.sent) != 1 || s.sent[0].key.Key != "one_on" || s.sent[0].remote != "rf-1" {
		t.Fatalf("sent = %+v", s.sent)
	}
}

// A device offered by both listings must not appear twice.
func TestRemoteListedTwiceIsDeduped(t *testing.T) {
	s := newStub()
	s.subs = map[string][]tuya.IRRemote{
		"blast-1": {{ID: "r-1", Name: "Телевизор"}},
	}
	p := newProvider(t, s, nil)

	devices, err := p.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2 (r-1 and r-2, no duplicates): %+v", len(devices), devices)
	}
}

func TestOfflineLearnedRemoteIsReportedOffline(t *testing.T) {
	off := false
	s := newStub()
	s.subs = map[string][]tuya.IRRemote{
		"blast-1": {{ID: "rf-1", Name: "Гирлянда", Category: "rf_switch1", Online: &off}},
	}
	p := newProvider(t, s, nil)

	d, err := p.Device(context.Background(), "rf-1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if d.Online {
		t.Error("an offline sub-device must be reported offline")
	}
}

// A library key is a button; a learned key is passive. Conflating them is what
// put a control on screen that the cloud silently refused to transmit.
func TestLibraryKeyIsButtonAndLearnedKeyIsPassive(t *testing.T) {
	s := newStub()
	s.keys["blast-1/r-1"] = []tuya.IRKey{
		{Key: "PowerOn", Name: "power on", Standard: true},
		{Key: "one_on", Name: "one ON", Standard: false},
	}
	p := newProvider(t, s, nil)

	spec, err := p.Spec(context.Background(), "r-1")
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	byCode := map[string]string{}
	for _, f := range spec.Functions {
		byCode[f.Code] = f.Type
	}
	if byCode["PowerOn"] != tuya.FunctionTypeButton {
		t.Errorf("a library key must be a Button, got %q", byCode["PowerOn"])
	}
	if byCode["one_on"] != tuya.FunctionTypePassive {
		t.Errorf("a learned key must be Passive, got %q", byCode["one_on"])
	}
}

// With scenes on, a learned RF switch must not be listed: its keys are Passive
// and a same-named scene is the working alternative. Showing both offered the
// same light twice, once broken.
func TestLearnedSwitchesAreHiddenWhenScenesCover(t *testing.T) {
	online := true
	s := newStub()
	s.subs = map[string][]tuya.IRRemote{
		"blast-1": {
			{ID: "rf-1", Name: "Главный", Category: "rf_switch1", Online: &online},
			{ID: "ac-1", Name: "Кондер", Category: "infrared_ac", Online: &online},
		},
	}
	p := New(s, Options{SkipLearned: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	devices, err := p.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	for _, d := range devices {
		if d.Category == "ir_remote" && d.ID == "rf-1" {
			t.Error("a learned RF switch must be hidden when scenes are on")
		}
	}
	// The library remote stays: it has real keys and no scene covers it.
	var kept bool
	for _, d := range devices {
		if d.ID == "ac-1" {
			kept = true
		}
	}
	if !kept {
		t.Error("a library remote must still be served")
	}
}

// Without scenes the learned switch stays visible — hidden is a choice, not a
// silent removal of the device.
func TestLearnedSwitchesStayVisibleWithoutScenes(t *testing.T) {
	online := true
	s := newStub()
	s.subs = map[string][]tuya.IRRemote{
		"blast-1": {{ID: "rf-1", Name: "Главный", Category: "rf_switch1", Online: &online}},
	}
	p := newProvider(t, s, nil)

	devices, err := p.ListDevices(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	var found bool
	for _, d := range devices {
		if d.ID == "rf-1" {
			found = true
		}
	}
	if !found {
		t.Error("without scenes the RF switch must remain listed")
	}
}
