package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/WarlockCyber/CoolHome/internal/dashboard"
)

// dashboardResponse is what the client reads and writes.
type dashboardResponse struct {
	Widgets []dashboard.Widget `json:"widgets"`
	// Editable reports whether the server can persist a layout. A server run
	// without a file keeps the layout in memory, and the UI should say so
	// rather than pretend a change survived a restart.
	Editable bool `json:"editable"`
	// MaxWidgets is the limit the server enforces, so the UI can stop before
	// the request is rejected.
	MaxWidgets int `json:"max_widgets"`
}

func (s *server) handleDashboard(w http.ResponseWriter, _ *http.Request) {
	widgets, err := s.dash.Widgets()
	if err != nil {
		s.log.Error("read dashboard", "err", err)
		writeError(w, http.StatusInternalServerError, "не удалось прочитать раскладку: "+err.Error())
		return
	}
	if widgets == nil {
		// An empty layout is a list, not null: the client iterates it.
		widgets = []dashboard.Widget{}
	}
	writeJSON(w, http.StatusOK, dashboardResponse{
		Widgets:    widgets,
		Editable:   s.dash.Path() != "",
		MaxWidgets: dashboard.MaxWidgets,
	})
}

func (s *server) handleSaveDashboard(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req struct {
		Widgets []dashboard.Widget `json:"widgets"`
	}
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON: "+err.Error())
		return
	}
	if err := s.dash.Replace(req.Widgets); err != nil {
		if errors.Is(err, dashboard.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.log.Error("save dashboard", "err", err)
		writeError(w, http.StatusInternalServerError, "не удалось сохранить раскладку: "+err.Error())
		return
	}
	saved, err := s.dash.Widgets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dashboardResponse{
		Widgets:    saved,
		Editable:   s.dash.Path() != "",
		MaxWidgets: dashboard.MaxWidgets,
	})
}
