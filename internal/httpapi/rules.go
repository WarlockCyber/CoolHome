package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/rules"
)

// rulesResponse is the whole rule screen: the set, whether it can be stored, and
// the runner's state so the page can say "правила не проверялись" rather than
// imply they are working.
type rulesResponse struct {
	Groups []rules.Group `json:"groups"`
	Rules  []rules.Rule  `json:"rules"`
	// Editable reports whether the server can persist the set. A server run
	// without a file keeps it in memory, and the page should say so rather than
	// pretend a change survived a restart.
	Editable bool `json:"editable"`
	// Running is false when the engine is off (COOLHOME_RULES unset), in which
	// case the rules are editable but never evaluated.
	Running  bool          `json:"running"`
	Primed   bool          `json:"primed"`
	LastRun  *time.Time    `json:"last_run,omitempty"`
	LastErr  string        `json:"last_error,omitempty"`
	Journal  []rules.Entry `json:"journal"`
	Timezone string        `json:"timezone"`
}

func (s *server) rulesAvailable() bool {
	return s.opts.Rules != nil && s.opts.Rules.Store() != nil
}

func (s *server) newRulesResponse(set rules.Set) rulesResponse {
	if set.Groups == nil {
		set.Groups = []rules.Group{}
	}
	if set.Rules == nil {
		set.Rules = []rules.Rule{}
	}
	out := rulesResponse{
		Groups:   set.Groups,
		Rules:    set.Rules,
		Editable: s.rulesAvailable() && s.opts.Rules.Store().Path() != "",
		Journal:  []rules.Entry{},
	}
	if s.opts.RulesLocation != nil {
		out.Timezone = s.opts.RulesLocation.String()
	}
	if s.opts.Rules != nil {
		out.Running = true
		primed, lastRun, lastErr := s.opts.Rules.Status()
		out.Primed = primed
		out.LastErr = lastErr
		if !lastRun.IsZero() {
			t := lastRun
			out.LastRun = &t
		}
		if journal := s.opts.Rules.Journal(); len(journal) > 0 {
			out.Journal = journal
		}
	}
	return out
}

func (s *server) handleRules(w http.ResponseWriter, _ *http.Request) {
	if !s.rulesAvailable() {
		// The feature is off. An empty set with running:false is a truer answer
		// than a 404: the page can still explain how to turn it on.
		writeJSON(w, http.StatusOK, s.newRulesResponse(rules.Set{}))
		return
	}
	set, err := s.opts.Rules.Store().Get()
	if err != nil {
		s.log.Error("read rules", "err", err)
		writeError(w, http.StatusInternalServerError, "не удалось прочитать правила: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.newRulesResponse(set))
}

func (s *server) handleSaveRules(w http.ResponseWriter, r *http.Request) {
	if !s.rulesAvailable() {
		writeError(w, http.StatusConflict, "правила выключены: задайте COOLHOME_RULES=true или COOLHOME_RULES_FILE")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var req struct {
		Groups []rules.Group `json:"groups"`
		Rules  []rules.Rule  `json:"rules"`
	}
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON: "+err.Error())
		return
	}
	saved, err := s.opts.Rules.Store().Save(rules.Set{Groups: req.Groups, Rules: req.Rules})
	if err != nil {
		if errors.Is(err, rules.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.log.Error("save rules", "err", err)
		writeError(w, http.StatusInternalServerError, "не удалось сохранить правила: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.newRulesResponse(saved))
}

// handleRuleEnabled switches one rule. It is separate from saving the whole set
// because it is the one action a person takes in a hurry: the automation is in
// the way, and it has to stop now, from a phone.
func (s *server) handleRuleEnabled(w http.ResponseWriter, r *http.Request) {
	s.handleSwitch(w, r, func(set *rules.Set, enabled bool) (found bool, err error) {
		id := r.PathValue("id")
		for i := range set.Rules {
			if set.Rules[i].ID == id {
				set.Rules[i].Enabled = enabled
				return true, nil
			}
		}
		return false, nil
	})
}

// handleGroupEnabled switches a whole group without touching the individual
// switches, so turning it back on restores exactly the set that was running.
func (s *server) handleGroupEnabled(w http.ResponseWriter, r *http.Request) {
	s.handleSwitch(w, r, func(set *rules.Set, enabled bool) (found bool, err error) {
		name := r.PathValue("name")
		for i := range set.Groups {
			if set.Groups[i].Name == name {
				set.Groups[i].Enabled = enabled
				return true, nil
			}
		}
		return false, nil
	})
}

func (s *server) handleSwitch(w http.ResponseWriter, r *http.Request, apply func(*rules.Set, bool) (bool, error)) {
	if !s.rulesAvailable() {
		writeError(w, http.StatusConflict, "правила выключены: задайте COOLHOME_RULES=true или COOLHOME_RULES_FILE")
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
	found := false
	saved, err := s.opts.Rules.Store().Update(func(set *rules.Set) error {
		var err error
		found, err = apply(set, *req.Enabled)
		return err
	})
	if err != nil {
		if errors.Is(err, rules.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.log.Error("switch rules", "err", err)
		writeError(w, http.StatusInternalServerError, "не удалось переключить: "+err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "не найдено")
		return
	}
	writeJSON(w, http.StatusOK, s.newRulesResponse(saved))
}

// handleRulesCheck runs one evaluation now. Waiting for the next tick to find out
// whether a rule works is a bad way to write one.
func (s *server) handleRulesCheck(w http.ResponseWriter, r *http.Request) {
	if s.opts.Rules == nil {
		writeError(w, http.StatusConflict, "правила выключены: задайте COOLHOME_RULES=true или COOLHOME_RULES_FILE")
		return
	}
	// Dry: nothing is sent, and the edge is not consumed, so the real tick still
	// sees it. The answer is "what would fire", not "what did".
	decisions := s.opts.Rules.DryStep(r.Context())
	now := time.Now()
	if s.opts.RulesLocation != nil {
		now = time.Now().In(s.opts.RulesLocation)
	}
	preview := make([]rules.Entry, 0, len(decisions))
	for _, d := range decisions {
		entry := rules.Entry{Time: now, RuleID: d.RuleID, Name: d.RuleName, Group: d.Group}
		for _, a := range d.Actions {
			if a.Kind == rules.ActionScene {
				entry.Actions = append(entry.Actions, rules.JournalAction{Kind: rules.ActionScene, Scene: a.Scene})
				continue
			}
			entry.Actions = append(entry.Actions, rules.JournalAction{
				Kind: rules.ActionCommand, Device: a.Device, Code: a.Code, Value: a.Value,
			})
		}
		preview = append(preview, entry)
	}
	set, err := s.opts.Rules.Store().Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := s.newRulesResponse(set)
	out.Journal = preview
	writeJSON(w, http.StatusOK, out)
}
