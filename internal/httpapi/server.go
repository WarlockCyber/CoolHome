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
	writeJSON(w, http.StatusOK, newSnapshotDTO(s.svc.Snapshot(), s.opts.Demo, s.opts.IR, s.build))
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
	writeJSON(w, http.StatusOK, map[string]any{"device": newDeviceDTO(view), "ir": s.opts.IR})
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
		writeJSON(w, http.StatusOK, map[string]any{"device": newDeviceDTO(view)})
	case errors.Is(err, home.ErrNotFound):
		writeError(w, http.StatusNotFound, "устройство не найдено")
	case isValidation(err):
		writeError(w, http.StatusBadRequest, err.Error())
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
	writeJSON(w, http.StatusOK, newSnapshotDTO(s.svc.Snapshot(), s.opts.Demo, s.opts.IR, s.build))
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
