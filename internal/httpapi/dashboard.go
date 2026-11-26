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
	// Panel names the devices whose cards stand in the right-hand column, in
	// the order they were placed.
	Panel []string `json:"panel"`
	// Editable reports whether the server can persist a layout. A server run
	// without a file keeps the layout in memory, and the UI should say so
	// rather than pretend a change survived a restart.
	Editable bool `json:"editable"`
	// MaxWidgets is the limit the server enforces, so the UI can stop before
	// the request is rejected.
	MaxWidgets int `json:"max_widgets"`
	// MaxPanel is the same bound for the side panel.
	MaxPanel int `json:"max_panel"`
	// Names are the local device names, keyed by device id. They rename the
	// device on this installation only; the cloud keeps its own.
	Names map[string]string `json:"names"`
	// Descriptions are the local notes, keyed by device id: what a scene does,
	// in words the cloud does not carry.
	Descriptions map[string]string `json:"descriptions"`
	// MaxDescLen is the bound the server enforces on one description.
	MaxDescLen int `json:"max_desc_len"`
	// Hidden lists the devices this installation does not show: scenes made in
	// the Tuya app cannot be deleted through the API, so hiding is how an
	// obsolete one leaves the list.
	Hidden []string `json:"hidden"`
	// Groups are the tabs of the main screen, and Favorites is the cross-cutting
	// list shown first. Both are local, like names and descriptions.
	Groups          []dashboard.Group `json:"groups"`
	Favorites       []string          `json:"favorites"`
	MaxGroups       int               `json:"max_groups"`
	MaxGroupNameLen int               `json:"max_group_name_len"`
	// MaxNameLen is the bound the server enforces on one name.
	MaxNameLen int `json:"max_name_len"`
}

// newDashboardResponse fills the response from a stored layout. Empty lists are
// sent as [] rather than null: the client maps over them.
func (s *server) newDashboardResponse(layout dashboard.Layout) dashboardResponse {
	if layout.Widgets == nil {
		layout.Widgets = []dashboard.Widget{}
	}
	if layout.Panel == nil {
		layout.Panel = []string{}
	}
	// A map, not a list: the client looks a device up by id, and an empty map
	// keeps "no names" expressible without a null check.
	if layout.Names == nil {
		layout.Names = map[string]string{}
	}
	if layout.Descriptions == nil {
		layout.Descriptions = map[string]string{}
	}
	if layout.Hidden == nil {
		layout.Hidden = []string{}
	}
	if layout.Groups == nil {
		layout.Groups = []dashboard.Group{}
	}
	if layout.Favorites == nil {
		layout.Favorites = []string{}
	}
	return dashboardResponse{
		Widgets:         layout.Widgets,
		Panel:           layout.Panel,
		Names:           layout.Names,
		Descriptions:    layout.Descriptions,
		Hidden:          layout.Hidden,
		Groups:          layout.Groups,
		Favorites:       layout.Favorites,
		MaxGroups:       dashboard.MaxGroups,
		MaxGroupNameLen: dashboard.MaxGroupNameLen,
		MaxDescLen:      dashboard.MaxDescLen,
		Editable:        s.dash.Path() != "",
		MaxWidgets:      dashboard.MaxWidgets,
		MaxPanel:        dashboard.MaxPanel,
		MaxNameLen:      dashboard.MaxNameLen,
	}
}

func (s *server) handleDashboard(w http.ResponseWriter, _ *http.Request) {
	layout, err := s.dash.Current()
	if err != nil {
		s.log.Error("read dashboard", "err", err)
		writeError(w, http.StatusInternalServerError, "не удалось прочитать раскладку: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.newDashboardResponse(layout))
}

func (s *server) handleSaveDashboard(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	// Both lists are pointers so "the key is absent" and "the list is empty" stay
	// different answers, and an absent key means "leave it alone".
	//
	// This matters because the two lists are edited from different places: the
	// remote from the grid, the panel from the column beside it. A phone adding a
	// card to the panel and a tablet reordering the remote are two writers to one
	// document, and neither may silently undo the other. The page therefore sends
	// only the list it changed, and clearing is still expressible: send [].
	var req struct {
		Widgets      *[]dashboard.Widget `json:"widgets"`
		Panel        *[]string           `json:"panel"`
		Names        *map[string]string  `json:"names"`
		Descriptions *map[string]string  `json:"descriptions"`
		Hidden       *[]string           `json:"hidden"`
		Groups       *[]dashboard.Group  `json:"groups"`
		Favorites    *[]string           `json:"favorites"`
	}
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON: "+err.Error())
		return
	}
	if req.Widgets == nil && req.Panel == nil && req.Names == nil &&
		req.Descriptions == nil && req.Hidden == nil && req.Groups == nil && req.Favorites == nil {
		writeError(w, http.StatusBadRequest,
			"нечего сохранять: нужен widgets, panel, names, descriptions, hidden, groups или favorites")
		return
	}
	// Update, not Current-then-Save: the two lists are edited from different
	// devices, and a read-modify-write in the handler would drop whichever
	// change landed in between.
	saved, err := s.dash.Update(func(layout *dashboard.Layout) error {
		if req.Widgets != nil {
			layout.Widgets = *req.Widgets
		}
		if req.Panel != nil {
			layout.Panel = *req.Panel
		}
		if req.Groups != nil {
			// The whole list: creating, renaming, moving a device and deleting a
			// group are all one edit of the tab strip, and a merge could not
			// express "this device left that group".
			layout.Groups = *req.Groups
		}
		if req.Favorites != nil {
			layout.Favorites = *req.Favorites
		}
		if req.Hidden != nil {
			// The whole list, like names: an empty list shows everything again.
			layout.Hidden = *req.Hidden
		}
		if req.Descriptions != nil {
			// The whole map, like names: an empty value removes the note.
			layout.Descriptions = *req.Descriptions
		}
		if req.Names != nil {
			// The whole map, not a merge: removing a name is how the cloud's name
			// comes back, and a merge could never express a deletion. The page
			// sends the complete map it holds.
			layout.Names = *req.Names
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, dashboard.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.log.Error("save dashboard", "err", err)
		writeError(w, http.StatusInternalServerError, "не удалось сохранить раскладку: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.newDashboardResponse(saved))
}
