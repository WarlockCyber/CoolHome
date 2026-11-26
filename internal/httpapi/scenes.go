package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/WarlockCyber/CoolHome/internal/scene"
)

// scenesResponse is the scene tab for the editor: every scene with what it does,
// and whether it can be changed from here.
type scenesResponse struct {
	Scenes []scene.EditableView `json:"scenes"`
	// Editable is false when the scene service is not configured at all, which
	// is a different thing from "this scene cannot be read".
	Editable bool `json:"editable"`
}

func (s *server) scenesEditable() bool {
	return s.opts.SceneEditor != nil
}

func (s *server) handleScenes(w http.ResponseWriter, r *http.Request) {
	if !s.scenesEditable() {
		writeJSON(w, http.StatusOK, scenesResponse{Scenes: []scene.EditableView{}, Editable: false})
		return
	}
	views, err := s.opts.SceneEditor.Describe(r.Context())
	if err != nil {
		s.log.Error("describe scenes", "err", err)
		writeError(w, http.StatusBadGateway, "не удалось прочитать сцены: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, scenesResponse{Scenes: views, Editable: true})
}

// sceneDraftBody is what the editor submits.
type sceneDraftBody struct {
	Name    string `json:"name"`
	Actions []struct {
		Device string `json:"device"`
		Code   string `json:"code"`
		Value  any    `json:"value"`
	} `json:"actions"`
}

func (b sceneDraftBody) draft() scene.Draft {
	out := scene.Draft{Name: b.Name, Actions: make([]scene.DraftAction, 0, len(b.Actions))}
	for _, a := range b.Actions {
		out.Actions = append(out.Actions, scene.DraftAction{Device: a.Device, Code: a.Code, Value: a.Value})
	}
	return out
}

func (s *server) handleCreateScene(w http.ResponseWriter, r *http.Request) {
	if !s.scenesEditable() {
		writeError(w, http.StatusConflict, "сцены выключены: задайте COOLHOME_SCENES=true")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var body sceneDraftBody
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON: "+err.Error())
		return
	}
	id, err := s.opts.SceneEditor.Create(r.Context(), body.draft())
	if err != nil {
		s.writeSceneError(w, err)
		return
	}
	// The new scene is served back so the page can switch to it at once.
	s.writeScenes(w, r, map[string]string{"id": id})
}

func (s *server) handleUpdateScene(w http.ResponseWriter, r *http.Request) {
	if !s.scenesEditable() {
		writeError(w, http.StatusConflict, "сцены выключены: задайте COOLHOME_SCENES=true")
		return
	}
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var body sceneDraftBody
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON: "+err.Error())
		return
	}
	if err := s.opts.SceneEditor.Update(r.Context(), id, body.draft()); err != nil {
		s.writeSceneError(w, err)
		return
	}
	s.writeScenes(w, r, nil)
}

// handleSceneEnabled enables or disables a scene. The cloud has no separate
// status route, so this is the whole-document update with one field changed.
func (s *server) handleSceneEnabled(w http.ResponseWriter, r *http.Request) {
	if !s.scenesEditable() {
		writeError(w, http.StatusConflict, "сцены выключены: задайте COOLHOME_SCENES=true")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON: "+err.Error())
		return
	}
	if req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "нужно поле enabled")
		return
	}
	if err := s.opts.SceneEditor.SetStatus(r.Context(), r.PathValue("id"), *req.Enabled); err != nil {
		s.writeSceneError(w, err)
		return
	}
	s.writeScenes(w, r, nil)
}

// writeScenes answers with the refreshed list. A write is followed by a read
// because the page shows the list: an answer without it would leave the page to
// guess what the cloud now holds.
func (s *server) writeScenes(w http.ResponseWriter, r *http.Request, extra map[string]string) {
	views, err := s.opts.SceneEditor.Describe(r.Context())
	if err != nil {
		// The write itself succeeded; the list could not be read. Saying so is
		// better than reporting the whole operation as failed.
		writeJSON(w, http.StatusOK, map[string]any{
			"scenes": []scene.EditableView{}, "editable": true,
			"warning": "сцена сохранена, но список прочитать не удалось: " + err.Error(),
		})
		return
	}
	out := map[string]any{"scenes": views, "editable": true}
	for k, v := range extra {
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
}

// writeSceneError maps the editor's refusals to status codes: a bad draft is the
// caller's mistake (400), a scene that cannot be read is a conflict with the
// cloud's own state (409), and everything else is the cloud's failure (502).
func (s *server) writeSceneError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, scene.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, scene.ErrNotEditable):
		// A condition, not a bad request: the scene exists and works, the cloud
		// just will not let it be read back. The page shows the reason as a
		// message with the place to go.
		writeProblem(w, http.StatusConflict, problem{
			Code:      "scene_not_editable",
			Title:     "Эту сцену нельзя изменить отсюда",
			Message:   err.Error(),
			Link:      s.consoleURL(),
			LinkLabel: "Открыть консоль Tuya",
		})
	default:
		s.log.Warn("scene write failed", "err", err)
		writeError(w, http.StatusBadGateway, "облако отклонило запрос: "+err.Error())
	}
}
