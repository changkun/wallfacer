package handler

import (
	"net/http"
	"net/url"
	"strings"

	"changkun.de/x/wallfacer/internal/auth"
	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// Convenience aliases so callers can write handler.BodyLimitDefault etc.
// These re-export constants used by MaxBytesMiddleware to enforce per-route
// request body size limits.
const (
	BodyLimitDefault      = constants.BodyLimitDefault
	BodyLimitInstructions = constants.BodyLimitInstructions
	BodyLimitFeedback     = constants.BodyLimitFeedback
)

// MaxBytesMiddleware limits the size of the request body for downstream handlers.
func MaxBytesMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFMiddleware validates the Origin/Referer header against the expected host.
// Safe methods (GET, HEAD, OPTIONS) are always allowed. State-changing methods
// require the Origin or Referer header to match the server's host:port. When
// neither header is present the request is allowed through — this covers API
// clients and tools that don't send browser-style origin headers.
func CSRFMiddleware(serverHostPort string) func(http.Handler) http.Handler {
	allowedHost := strings.TrimSpace(serverHostPort)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Safe methods never need CSRF protection.
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if allowedHost == "" {
				next.ServeHTTP(w, r)
				return
			}
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			referer := strings.TrimSpace(r.Header.Get("Referer"))
			// Allow requests without Origin/Referer (non-browser clients).
			if origin == "" && referer == "" {
				next.ServeHTTP(w, r)
				return
			}
			raw := origin
			if raw == "" {
				raw = referer
			}
			parsed, err := url.Parse(raw)
			if err != nil || parsed.Host == "" || parsed.Host != allowedHost {
				httpjson.Write(w, http.StatusForbidden, map[string]string{"error": "forbidden: invalid origin"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BearerAuthMiddleware enforces bearer-token authentication on non-SSE routes.
// SSE and WebSocket paths use a ?token= query parameter instead of the
// Authorization header because EventSource and WebSocket APIs do not support
// custom request headers. The root path (GET /) is always public so the
// browser can load the UI shell.
func BearerAuthMiddleware(apiKey string) func(http.Handler) http.Handler {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		// No API key configured — authentication is disabled.
		return func(next http.Handler) http.Handler { return next }
	}
	isSSEPath := func(path string) bool {
		if path == "/api/tasks/stream" || path == "/api/git/stream" || path == "/api/terminal/ws" ||
			path == "/api/explorer/stream" || path == "/api/specs/stream" {
			return true
		}
		return strings.HasPrefix(path, "/api/tasks/") && strings.HasSuffix(path, "/logs")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/" {
				next.ServeHTTP(w, r)
				return
			}
			// A request already authenticated by the upstream JWT
			// middleware (cloud mode) bypasses the static-key check.
			// Keeps cookie-only and JWT-bearer clients working even
			// when WALLFACER_SERVER_API_KEY is set for script access.
			if _, ok := auth.PrincipalFromContext(r.Context()); ok {
				next.ServeHTTP(w, r)
				return
			}
			if isSSEPath(r.URL.Path) {
				if r.URL.Query().Get("token") != key {
					httpjson.Write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			got := strings.TrimSpace(r.Header.Get("Authorization"))
			if got != "Bearer "+key {
				httpjson.Write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
