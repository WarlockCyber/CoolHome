package httpapi

import (
	"net/http"
	"strings"
)

// Methods and headers the API actually serves to a cross-origin caller.
const (
	corsAllowMethods = "GET, POST, OPTIONS"
	corsAllowHeaders = "Authorization, X-Auth-Token, Content-Type"
	// corsMaxAge caches a successful preflight for ten minutes so a polling
	// client does not pay for it before every request.
	corsMaxAge = "600"
)

// corsPolicy is an explicit allowlist of origins. A mini-app bundle is public
// and same-origin policy is the only thing stopping an arbitrary page from
// calling the API with a token it has seen, so a wildcard must be opted into
// and is never combined with credentials.
type corsPolicy struct {
	origins  map[string]struct{}
	wildcard bool
}

// newCORSPolicy builds a policy from configured origins, or returns nil when
// CORS is off. Entries are normalized (lower-case, no trailing slash) so a
// config typo in case does not silently block a client.
func newCORSPolicy(allowed []string) *corsPolicy {
	if len(allowed) == 0 {
		return nil
	}
	p := &corsPolicy{origins: make(map[string]struct{}, len(allowed))}
	for _, origin := range allowed {
		origin = strings.TrimSpace(origin)
		switch {
		case origin == "":
			continue
		case origin == "*":
			p.wildcard = true
		default:
			p.origins[normalizeOrigin(origin)] = struct{}{}
		}
	}
	if len(p.origins) == 0 && !p.wildcard {
		return nil
	}
	return p
}

func normalizeOrigin(origin string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(origin), "/"))
}

func (p *corsPolicy) allows(origin string) bool {
	if p.wildcard {
		return true
	}
	_, ok := p.origins[normalizeOrigin(origin)]
	return ok
}

// cors answers preflights and labels cross-origin responses. It runs outside
// auth because a preflight carries no Authorization header and must succeed
// before the browser will send the real request.
func (s *server) cors(next http.Handler) http.Handler {
	p := s.corsPolicy
	if p == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
		if !p.allows(origin) {
			if preflight {
				writeError(w, http.StatusForbidden, "origin not allowed: "+origin)
				return
			}
			// Not a permitted origin: serve the request without CORS headers so
			// the browser, not the server, decides to hide the response.
			next.ServeHTTP(w, r)
			return
		}

		h := w.Header()
		if p.wildcard {
			h.Set("Access-Control-Allow-Origin", "*")
		} else {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
		}
		if !preflight {
			next.ServeHTTP(w, r)
			return
		}

		h.Add("Vary", "Access-Control-Request-Method")
		h.Add("Vary", "Access-Control-Request-Headers")
		h.Set("Access-Control-Allow-Methods", corsAllowMethods)
		h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
		h.Set("Access-Control-Max-Age", corsMaxAge)
		w.WriteHeader(http.StatusNoContent)
	})
}
