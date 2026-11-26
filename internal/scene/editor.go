package scene

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// Editing scenes: create a new one, change the name and the device actions of an
// existing one.
//
// Three rules hold this together.
//
// First, everything is validated against the device's thing model before a
// request leaves: a scene that writes a function the device does not have, or a
// value outside its range, would be accepted by the cloud and then fail at night
// when nobody is watching.
//
// Second, an update sends the document that was read, with only the name and the
// device actions replaced. The detail endpoint returns fields this code does not
// model — conditions, running_mode, and IR key presses — and rebuilding a body
// from a typed struct would silently drop them.
//
// Third, a scene whose detail cannot be read (the cloud answers 40003007) is not
// editable at all. There is nothing to send back unchanged, so any write would
// be guesswork about what the scene contained.

// Writer is the slice of the Tuya client the editor needs.
type Writer interface {
	Scenes(ctx context.Context, spaceID string) ([]tuya.Scene, error)
	GetScene(ctx context.Context, id string) (tuya.SceneDetail, error)
	CreateScene(ctx context.Context, doc tuya.SceneDocument) (string, error)
	UpdateScene(ctx context.Context, id string, doc tuya.SceneDocument) error
	SpaceID(ctx context.Context) (string, error)
}

// MaxNameLen bounds a scene name.
const MaxNameLen = 60

// MaxActions bounds a scene. A scene is a short sequence; forty actions is a
// sign that something else is wanted.
const MaxActions = 20

// ErrInvalid marks a draft the editor refuses.
var ErrInvalid = fmt.Errorf("scene: invalid draft")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// ErrNotEditable marks a scene that cannot be read back, and therefore cannot be
// written safely.
var ErrNotEditable = fmt.Errorf("scene: this scene cannot be read for editing")

// DraftAction is one action the editor builds: a data point of a device.
type DraftAction struct {
	Device string `json:"device"`
	Code   string `json:"code"`
	Value  any    `json:"value"`
}

// Draft is what the page submits.
type Draft struct {
	Name    string        `json:"name"`
	Actions []DraftAction `json:"actions"`
}

// Editor creates and updates scenes.
type Editor struct {
	client   Writer
	spaceID  string
	snapshot func() home.Snapshot
	// onChange runs after a successful write, so the provider's cached scene
	// list is dropped and the new scene shows up on the next refresh instead of
	// after the cache expires.
	onChange func()
	log      *slog.Logger
}

// NewEditor builds an editor over a client and the live home.
func NewEditor(client Writer, spaceID string, snapshot func() home.Snapshot, logger *slog.Logger) *Editor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Editor{client: client, spaceID: spaceID, snapshot: snapshot, log: logger}
}

// OnChange registers the hook run after a successful write.
func (e *Editor) OnChange(fn func()) { e.onChange = fn }

// changed runs the hook, if one is set.
func (e *Editor) changed() {
	if e.onChange != nil {
		e.onChange()
	}
}

func (e *Editor) space(ctx context.Context) (string, error) {
	if e.spaceID != "" {
		return e.spaceID, nil
	}
	id, err := e.client.SpaceID(ctx)
	if err != nil {
		return "", fmt.Errorf("scene: resolve space: %w", err)
	}
	if id == "" {
		return "", fmt.Errorf("scene: no space id found; set COOLHOME_SPACE_ID")
	}
	return id, nil
}

// validate checks the draft against the live home: every action must name a real
// device and a writable data point, and the value must be one the device accepts.
func (e *Editor) validate(draft Draft) ([]tuya.SceneAction, error) {
	name := strings.TrimSpace(draft.Name)
	if name == "" {
		return nil, invalid("у сцены должно быть название")
	}
	if utf8.RuneCountInString(name) > MaxNameLen {
		return nil, invalid("название длиннее %d символов", MaxNameLen)
	}
	// No "at least one action" here: a scene whose actions are all IR key presses
	// has none the editor builds, and renaming it must still be possible. Whether
	// the resulting scene has any action at all is checked where the whole list
	// is known — see Create and Update.
	if len(draft.Actions) > MaxActions {
		return nil, invalid("действий больше %d", MaxActions)
	}

	snap := e.snapshot()
	byID := make(map[string]home.DeviceView, len(snap.Devices))
	for _, d := range snap.Devices {
		byID[d.ID] = d
	}

	out := make([]tuya.SceneAction, 0, len(draft.Actions))
	for i, a := range draft.Actions {
		device, ok := byID[a.Device]
		if !ok {
			return nil, invalid("действие %d: устройство %s не найдено", i+1, a.Device)
		}
		if device.Category == Category {
			return nil, invalid("действие %d: сцена не может запускать сцену", i+1)
		}
		if device.Spec == nil {
			return nil, invalid("действие %d: у устройства %s нет описания функций — "+
				"такие устройства сценой не управляются", i+1, nameOf(device))
		}
		if a.Value == nil {
			return nil, invalid("действие %d: не задано значение", i+1)
		}
		// The same check the command path uses: the scene must not be able to
		// write something the button on the card would refuse.
		if err := home.ValidateCommand(tuya.Command{Code: a.Code, Value: a.Value}, device.Spec); err != nil {
			return nil, invalid("действие %d (%s): %v", i+1, nameOf(device), err)
		}
		out = append(out, tuya.SceneAction{
			Executor: tuya.ExecutorDevice,
			EntityID: a.Device,
			Property: map[string]any{
				"function_code":  a.Code,
				"function_value": a.Value,
			},
		})
	}
	return out, nil
}

func nameOf(d home.DeviceView) string {
	if d.Name != "" {
		return d.Name
	}
	return d.ID
}

// Create builds and sends a new scene, returning its id when the cloud reports
// one.
func (e *Editor) Create(ctx context.Context, draft Draft) (string, error) {
	actions, err := e.validate(draft)
	if err != nil {
		return "", err
	}
	space, err := e.space(ctx)
	if err != nil {
		return "", err
	}
	doc := tuya.SceneDocument{
		"space_id":   space,
		"name":       strings.TrimSpace(draft.Name),
		"type":       "scene",
		"conditions": []any{},
		"actions":    actionsToDoc(actions),
	}
	if len(actions) == 0 {
		return "", invalid("в сцене должно быть хотя бы одно действие")
	}
	id, err := e.client.CreateScene(ctx, doc)
	if err != nil {
		return "", fmt.Errorf("scene: create: %w", err)
	}
	e.changed()
	e.log.Info("scene created", "id", id, "name", strings.TrimSpace(draft.Name), "actions", len(actions))
	return id, nil
}

// Update changes the name and the device actions of an existing scene.
//
// The document is re-read here rather than accepted from the page: the page's
// copy may be a poll old, and sending it back would undo a rename made in the
// app meanwhile. Everything the editor does not model — conditions, running
// mode, IR key presses — is carried over from that fresh read.
func (e *Editor) Update(ctx context.Context, id string, draft Draft) error {
	actions, err := e.validate(draft)
	if err != nil {
		return err
	}
	detail, err := e.client.GetScene(ctx, id)
	if err != nil {
		if tuya.IsSceneNotQueryable(err) {
			return fmt.Errorf("%w: облако не отдаёт содержимое этой сцены (40003007), "+
				"поэтому сохранить её отсюда нельзя — откройте приложение Tuya", ErrNotEditable)
		}
		return fmt.Errorf("scene: read before update: %w", err)
	}
	doc := detail.Document
	if doc == nil {
		return fmt.Errorf("%w: облако не вернуло документ сцены", ErrNotEditable)
	}
	merged := mergeActions(detail.Actions, actions)
	// The cloud refuses an empty action list, and so does this: a scene that does
	// nothing is not what somebody meant. Counted on the merged list, because a
	// scene whose only actions are IR key presses is legitimately non-empty with
	// nothing the editor built.
	if len(merged) == 0 {
		return invalid("в сцене должно быть хотя бы одно действие")
	}
	doc["name"] = strings.TrimSpace(draft.Name)
	doc["actions"] = merged
	// The document is the same one that was read, so these two are already
	// right; setting them makes the intent explicit and tolerates a document
	// that arrived without them.
	if _, ok := doc["type"]; !ok {
		doc["type"] = "scene"
	}
	if _, ok := doc["conditions"]; !ok {
		doc["conditions"] = []any{}
	}
	// The id in the path is the identity; sending it back inside the body has no
	// meaning for an update and could only confuse a validator.
	delete(doc, "id")

	if err := e.client.UpdateScene(ctx, id, doc); err != nil {
		return fmt.Errorf("scene: update: %w", err)
	}
	e.changed()
	e.log.Info("scene updated", "id", id, "name", draft.Name, "actions", len(merged))
	return nil
}

// mergeActions puts the edited device actions back into the list of the scene as
// it stands, keeping everything else where it was.
//
// Order matters in a scene: "turn the lamp on, then set the colour" is not the
// same as the reverse. So the edited device actions take the slots the old
// device actions occupied, and an IR key press keeps its place among them.
func mergeActions(existing []tuya.SceneAction, edited []tuya.SceneAction) []any {
	out := make([]any, 0, len(existing)+len(edited))
	next := 0
	for _, a := range existing {
		if a.Executor != tuya.ExecutorDevice {
			// Not editable here: an IR key press belongs to a remote's own layout.
			out = append(out, a)
			continue
		}
		if next < len(edited) {
			out = append(out, edited[next])
			next++
		}
		// A scene cannot have fewer slots than actions; a leftover slot is
		// dropped, which is how a removed action disappears.
	}
	for ; next < len(edited); next++ {
		out = append(out, edited[next])
	}
	return out
}

func actionsToDoc(actions []tuya.SceneAction) []any {
	out := make([]any, len(actions))
	for i, a := range actions {
		out[i] = a
	}
	return out
}

// EditableView is what the page needs to draw the editor for one scene.
type EditableView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// Editable is false when the cloud will not return the scene's contents.
	Editable bool `json:"editable"`
	// Reason explains why not, in the person's terms.
	Reason string `json:"reason,omitempty"`
	// Actions lists what the scene does. Items the editor cannot change carry a
	// label instead of a device and code.
	Actions []ViewAction `json:"actions"`
}

// ViewAction is one action for display.
type ViewAction struct {
	Device string `json:"device,omitempty"`
	Code   string `json:"code,omitempty"`
	Value  any    `json:"value,omitempty"`
	// Fixed is set for an action the editor does not build, such as an IR key.
	Fixed bool `json:"fixed,omitempty"`
	// Label says what a fixed action does.
	Label string `json:"label,omitempty"`
}

// Describe returns the editable view of every scene, reading each one's detail.
// A scene that cannot be read is reported as not editable rather than omitted:
// the page must show that it exists and say why it cannot be changed.
func (e *Editor) Describe(ctx context.Context) ([]EditableView, error) {
	space, err := e.space(ctx)
	if err != nil {
		return nil, err
	}
	list, err := e.client.Scenes(ctx, space)
	if err != nil {
		return nil, fmt.Errorf("scene: list: %w", err)
	}
	out := make([]EditableView, 0, len(list))
	for _, s := range list {
		// Actions is an empty list, never null: the page iterates it, and a scene
		// with no actions at all (or one that could not be read) must not make it
		// check for null at every use.
		view := EditableView{ID: s.ID, Name: s.Name, Status: s.Status, Actions: []ViewAction{}}
		detail, err := e.client.GetScene(ctx, s.ID)
		switch {
		case err == nil:
			view.Editable = true
			for _, a := range detail.Actions {
				if a.Executor == tuya.ExecutorDevice {
					view.Actions = append(view.Actions, ViewAction{
						Device: a.EntityID, Code: a.FunctionCode(), Value: a.FunctionValue(),
					})
					continue
				}
				view.Actions = append(view.Actions, ViewAction{
					Fixed: true, Label: fixedLabel(a),
				})
			}
		case tuya.IsSceneNotQueryable(err):
			view.Reason = "Облако не отдаёт содержимое этой сцены (40003007) — её можно " +
				"запускать, но не редактировать отсюда. Откройте приложение Tuya."
		default:
			view.Reason = "Не удалось прочитать сцену: " + err.Error()
		}
		out = append(out, view)
	}
	return out, nil
}

// fixedLabel names an action the editor does not build.
func fixedLabel(a tuya.SceneAction) string {
	switch a.Executor {
	case tuya.ExecutorIR:
		return "ИК-кнопка пульта (не редактируется здесь)"
	case "rule_trigger":
		return "Запуск другого правила (не редактируется здесь)"
	case "device_group_issue":
		return "Группа устройств (не редактируется здесь)"
	default:
		if a.Executor == "" {
			return "Действие, которое редактор не знает"
		}
		return "Действие «" + a.Executor + "» (не редактируется здесь)"
	}
}

// SetStatus enables or disables a scene. It is the whole-document update with
// one field changed, because the cloud has no separate status route this project
// may call.
func (e *Editor) SetStatus(ctx context.Context, id string, enabled bool) error {
	detail, err := e.client.GetScene(ctx, id)
	if err != nil {
		if tuya.IsSceneNotQueryable(err) {
			return fmt.Errorf("%w: облако не отдаёт содержимое этой сцены", ErrNotEditable)
		}
		return err
	}
	if detail.Document == nil {
		return fmt.Errorf("%w: облако не вернуло документ сцены", ErrNotEditable)
	}
	status := "disable"
	if enabled {
		status = "enable"
	}
	detail.Document["status"] = status
	delete(detail.Document, "id")
	if err := e.client.UpdateScene(ctx, id, detail.Document); err != nil {
		return fmt.Errorf("scene: set status: %w", err)
	}
	e.changed()
	return nil
}

// ErrNotFound marks a scene id the editor does not know.
var ErrNotFound = errors.New("scene: not found")
