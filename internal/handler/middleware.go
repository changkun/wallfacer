package handler

import (
	"net/http"
	"net/url"
	"strings"

	"changkun.de/x/wallfacer/internal/constants"
)

// Convenience aliases so callers can write handler.BodyLimitDefault etc.
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
func CSRFMiddleware(serverHostPort string) func(http.Handler) http.Handler {
	allowedHost := strings.TrimSpace(serverHostPort)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden: invalid origin"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BearerAuthMiddleware enforces bearer-token authentication on non-SSE routes.
func BearerAuthMiddleware(apiKey string) func(http.Handler) http.Handler {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	isSSEPath := func(path string) bool {
		if path == "/api/tasks/stream" || path == "/api/git/stream" {
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
			if isSSEPath(r.URL.Path) {
				if r.URL.Query().Get("token") != key {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			auth := strings.TrimSpace(r.Header.Get("Authorization"))
			if auth != "Bearer "+key {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
