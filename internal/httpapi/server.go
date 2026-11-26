package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/dashboard"
	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/rules"
	"github.com/WarlockCyber/CoolHome/internal/scene"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
	"github.com/WarlockCyber/CoolHome/web"
)

// Options are the HTTP layer settings.
type Options struct {
	// AuthToken protects /api/* when non-empty. The static shell stays public
	// and the UI prompts for the token.
	AuthToken string
	Demo      bool
	Logger    *slog.Logger
	// AllowedOrigins enables CORS for the listed origins, e.g. a Tuya mini-app
	// running in a webview. Empty (the default) leaves CORS off entirely.
	AllowedOrigins []string
	// IR tells the UI whether infrared remotes are being served, so a blaster
	// card can point at them instead of telling the user to enable something
	// that is already on.
	IR bool
	// Dashboard stores which controls the remote shows. Nil means the feature
	// is disabled and the UI falls back to showing everything.
	Dashboard *dashboard.Store
	// ConsoleURL is where a message points when the cloud refuses for a reason
	// only the Tuya console can fix. Empty falls back to defaultConsoleURL.
	ConsoleURL string
	// Rules is the automation runner. Nil means the feature is off: the page
	// shows the rule screen with running:false rather than a 404, so it can say
	// how to turn it on.
	Rules *rules.Runner
	// RulesLocation is the zone schedules are read in, so a check and the runner
	// agree about what "22:00" means. Nil means time.Local.
	RulesLocation *time.Location
	// SceneEditor creates and edits scenes. Nil means the scene service is not
	// configured: the page then shows scenes read-only.
	SceneEditor *scene.Editor
}

type server struct {
	svc        *home.Service
	opts       Options
	log        *slog.Logger
	tmpl       *template.Template
	corsPolicy *corsPolicy
	dash       *dashboard.Store
	// build identifies the embedded bundle; a page that sees it change reloads
	// itself, so a wall tablet never runs yesterday's interface.
	build string
}

// New builds the HTTP handler.
func New(svc *home.Service, opts Options) (http.Handler, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	tmpl, err := template.ParseFS(web.FS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("httpapi: parse templates: %w", err)
	}
	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		return nil, fmt.Errorf("httpapi: static assets: %w", err)
	}
	assets, err := newStaticAssets(staticFS)
	if err != nil {
		return nil, fmt.Errorf("httpapi: static assets: %w", err)
	}
	build, err := buildFingerprint(web.FS)
	if err != nil {
		return nil, fmt.Errorf("httpapi: build fingerprint: %w", err)
	}

	s := &server{
		svc:        svc,
		opts:       opts,
		log:        opts.Logger,
		tmpl:       tmpl,
		corsPolicy: newCORSPolicy(opts.AllowedOrigins),
		build:      build,
	}
	s.dash = opts.Dashboard
	if s.dash == nil {
		// An in-memory store keeps the handlers uniform; Editable reports false
		// so the UI knows a change will not survive a restart.
		s.dash = dashboard.New("")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	// Registered one path at a time: the set comes from the embedded tree, so an
	// unknown name is a 404 rather than a directory listing, and every asset
	// carries the ETag its bytes earned.
	for _, name := range assets.names() {
		mux.Handle("GET /static/"+name, assets.handler(name))
	}
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("GET /api/devices/{id}", s.handleDevice)
	mux.HandleFunc("POST /api/devices/{id}/commands", s.handleCommands)
	mux.HandleFunc("POST /api/refresh", s.handleRefresh)
	mux.HandleFunc("GET /api/dashboard", s.handleDashboard)
	mux.HandleFunc("PUT /api/dashboard", s.handleSaveDashboard)
	mux.HandleFunc("GET /api/rules", s.handleRules)
	mux.HandleFunc("PUT /api/rules", s.handleSaveRules)
	mux.HandleFunc("POST /api/rules/check", s.handleRulesCheck)
	mux.HandleFunc("GET /api/scenes", s.handleScenes)
	mux.HandleFunc("POST /api/scenes", s.handleCreateScene)
	mux.HandleFunc("PUT /api/scenes/{id}", s.handleUpdateScene)
	mux.HandleFunc("PUT /api/scenes/{id}/enabled", s.handleSceneEnabled)
	mux.HandleFunc("POST /api/rules/{id}/enabled", s.handleRuleEnabled)
	// Editing one rule sends one rule: the whole-set PUT is kept for scripts, but
	// the page must not use it, or two screens overwrite each other.
	mux.HandleFunc("PUT /api/rules/{id}", s.handleRuleUpsert)
	mux.HandleFunc("DELETE /api/rules/{id}", s.handleRuleDelete)
	mux.HandleFunc("POST /api/rules/groups/{name}/enabled", s.handleGroupEnabled)

	// cors sits inside secureHeaders so preflight answers carry the same
	// hardening headers, and outside auth so preflights pass without a token.
	return s.recoverer(s.logRequests(s.secureHeaders(s.cors(s.auth(mux))))), nil
}

func (s *server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The shell names the scripts to load, so a stale copy keeps an old asset
	// list even after the binary changed. It is a few kilobytes; do not store it.
	w.Header().Set("Cache-Control", "no-store")
	data := struct {
		Demo  bool
		Build string
	}{Demo: s.opts.Demo, Build: s.build}
	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		s.log.Error("render index", "err", err)
	}
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	snap := s.svc.Snapshot()
	status := http.StatusOK
	if snap.Error != "" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"ok":        snap.Error == "",
		"devices":   len(snap.Devices),
		"last_sync": snap.LastSync,
		"error":     snap.Error,
	})
}

func (s *server) handleDevices(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, newSnapshotDTO(s.svc.Snapshot(), s.opts.Demo, s.opts.IR, s.build, s.localNames()))
}

// localNames reads the installation's device names. A store that cannot be read
// is not a reason to fail the snapshot: the devices are still there, and the
// cloud's names are a correct fallback.
func (s *server) localNames() map[string]string {
	names, err := s.dash.Names()
	if err != nil {
		s.log.Warn("read local device names", "err", err)
		return nil
	}
	return names
}

func (s *server) handleDevice(w http.ResponseWriter, r *http.Request) {
	view, ok := s.svc.Device(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "устройство не найдено")
		return
	}
	// The flag travels with the device too: the single-device view is what a
	// client with a deep link opens first, and it needs to know whether infrared
	// remotes are being served before explaining a blaster.
	writeJSON(w, http.StatusOK, map[string]any{"device": newDeviceDTO(view, s.localNames()), "ir": s.opts.IR})
}

type commandRequest struct {
	Commands []tuya.Command `json:"commands"`
}

func (s *server) handleCommands(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var req commandRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON: "+err.Error())
		return
	}

	id := r.PathValue("id")
	view, err := s.svc.Command(r.Context(), id, req.Commands)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"device": newDeviceDTO(view, s.localNames())})
	case errors.Is(err, home.ErrNotFound):
		writeError(w, http.StatusNotFound, "устройство не найдено")
	case isValidation(err):
		writeError(w, http.StatusBadRequest, err.Error())
	case tuya.IsQuotaExhausted(err):
		// A subscription condition, not a fault: the project's plan allows a
		// fixed number of controllable devices, and this one is outside it.
		// It is a message with a place to go next, not a one-line error.
		s.log.Warn("command refused: controllable device quota exhausted", "device", id, "err", err)
		writeProblem(w, http.StatusBadGateway, problem{
			Code:      "quota_exhausted",
			Title:     "Облако Tuya не принимает команду",
			Message:   quotaMessage,
			Link:      s.consoleURL(),
			LinkLabel: "Открыть консоль Tuya",
		})
	default:
		s.log.Warn("command failed", "device", id, "err", err)
		writeError(w, http.StatusBadGateway, "облако Tuya отклонило команду: "+err.Error())
	}
}

func (s *server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Refresh(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, "не удалось обновить состояние: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, newSnapshotDTO(s.svc.Snapshot(), s.opts.Demo, s.opts.IR, s.build, s.localNames()))
}

func isValidation(err error) bool {
	var v *home.ValidationError
	return errors.As(err, &v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		slog.Default().Error("write json", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// problem is an error the page shows as a message rather than a toast: it has a
// cause the user can act on and often a place to go next.
//
// It is one JSON object with the error message under `error` as well, so a
// client that only knows the old shape — a script, the mini-app — still reads
// the reason instead of "no error field".
type problem struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
	// Title is the message's heading; Message is the explanation.
	Title     string `json:"title,omitempty"`
	Message   string `json:"message,omitempty"`
	Link      string `json:"link,omitempty"`
	LinkLabel string `json:"link_label,omitempty"`
}

func writeProblem(w http.ResponseWriter, status int, p problem) {
	p.Error = p.Message
	writeJSON(w, status, p)
}

// consoleURL is where a message sends the user to fix a project-level problem.
func (s *server) consoleURL() string {
	if s.opts.ConsoleURL != "" {
		return s.opts.ConsoleURL
	}
	return defaultConsoleURL
}

// defaultConsoleURL is the Tuya Cloud console (verified reachable). There is no
// stable public URL for the plan page itself, so the link points at the console
// where the project's plan and quota are shown; the message names the path.
const defaultConsoleURL = "https://iot.tuya.com/cloud"

// quotaMessage explains the cloud's refusal in the terms the user can act on.
// The raw text ("Your controllable device pool quota is insufficient") names
// neither the cause nor the place to fix it.
const quotaMessage = `У проекта Tuya исчерпана квота управляемых устройств: облако отвечает 60001001 и отклоняет команды для устройств за её пределами. Это не сбой устройства и не ошибка CoolHome — часть устройств осталась вне тарифа проекта. Откройте консоль Tuya, выберите Cloud → Development → ваш проект и проверьте квоту: на бесплатном и пробном тарифах число управляемых устройств ограничено. После увеличения квоты команды начнут проходить без изменений в CoolHome.`

func (s *server) auth(next http.Handler) http.Handler {
	if s.opts.AuthToken == "" {
		return next
	}
	want := []byte(s.opts.AuthToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get("X-Auth-Token")
		if h := r.Header.Get("Authorization"); h != "" {
			if after, ok := strings.CutPrefix(h, "Bearer "); ok {
				token = strings.TrimSpace(after)
			}
		}
		if subtle.ConstantTimeCompare([]byte(token), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="coolhome"`)
			writeError(w, http.StatusUnauthorized, "нужен токен доступа")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Debug("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).String(),
		)
	})
}

func (s *server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.log.Error("panic", "path", r.URL.Path, "panic", rec)
				writeError(w, http.StatusInternalServerError, "внутренняя ошибка")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
