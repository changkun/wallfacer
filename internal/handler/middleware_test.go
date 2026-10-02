package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/auth"
)

// oversizedBody creates a JSON reader with a single field whose value is a
// string of the given size, used to trigger MaxBytesReader 413 responses.
func oversizedBody(fieldName string, size int) io.Reader {
	padding := strings.Repeat("a", size)
	return strings.NewReader(`{"` + fieldName + `":"` + padding + `"}`)
}

// assertBodyTooLarge verifies that the response is a 413 with the expected
// JSON error body from httpjson.DecodeBody's MaxBytesError handling.
func assertBodyTooLarge(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "request body too large" {
		t.Fatalf("expected body-too-large error, got %#v", resp)
	}
}

// TestCreateTask_BodyTooLarge verifies that CreateTask returns 413 when the
// request body exceeds the default body size limit.
func TestCreateTask_BodyTooLarge(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/tasks", oversizedBody("prompt", 2<<20))
	r.Body = http.MaxBytesReader(w, r.Body, BodyLimitDefault)

	h.CreateTask(w, r)

	assertBodyTooLarge(t, w)
}

// TestCSRFMiddleware validates the CSRF middleware's Origin/Referer checking
// across safe methods, matching/mismatching origins, and absent headers.
func TestCSRFMiddleware(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
		wantErr string
	}{
		{name: "safe method", method: http.MethodGet, want: http.StatusNoContent},
		{name: "matching origin", method: http.MethodPost, headers: map[string]string{"Origin": "http://localhost:8080"}, want: http.StatusNoContent},
		{name: "matching referer", method: http.MethodDelete, headers: map[string]string{"Referer": "http://localhost:8080/tasks/1"}, want: http.StatusNoContent},
		{name: "missing origin and referer", method: http.MethodPatch, want: http.StatusNoContent},
		{name: "mismatched origin", method: http.MethodPost, headers: map[string]string{"Origin": "http://evil.example"}, want: http.StatusForbidden, wantErr: "forbidden: invalid origin"},
		{name: "malformed referer", method: http.MethodPut, headers: map[string]string{"Referer": ":"}, want: http.StatusForbidden, wantErr: "forbidden: invalid origin"},
	}

	mw := CSRFMiddleware("localhost:8080")
	next := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/tasks", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			next.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d body=%s", w.Code, tc.want, w.Body.String())
			}
			if tc.wantErr == "" {
				return
			}
			var resp map[string]string
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp["error"] != tc.wantErr {
				t.Fatalf("error = %q, want %q", resp["error"], tc.wantErr)
			}
		})
	}
}

// TestCSRFMiddlewareRemoteAccess verifies that a browser accessing the server
// via a hostname or IP other than "localhost" is still allowed through,
// as long as Origin matches the Host header (same-origin from the browser's
// perspective).
func TestCSRFMiddlewareRemoteAccess(t *testing.T) {
	mw := CSRFMiddleware("localhost:8080")
	next := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// Simulate a browser accessing via IP: Origin and Host both say the IP.
	req := httptest.NewRequest(http.MethodPut, "/api/workspaces", nil)
	req.Host = "192.168.1.10:8080"
	req.Header.Set("Origin", "http://192.168.1.10:8080")
	w := httptest.NewRecorder()
	next.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("remote same-origin: status = %d, want %d body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}

	// Cross-origin from a different host must still be rejected.
	req2 := httptest.NewRequest(http.MethodPut, "/api/workspaces", nil)
	req2.Host = "192.168.1.10:8080"
	req2.Header.Set("Origin", "http://evil.example")
	w2 := httptest.NewRecorder()
	next.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("cross-origin: status = %d, want %d", w2.Code, http.StatusForbidden)
	}
}

// TestBearerAuthMiddleware validates bearer-token auth across public routes,
// SSE query-token paths, and standard Authorization header paths.
func TestBearerAuthMiddleware(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		target  string
		headers map[string]string
		want    int
		wantErr string
	}{
		{name: "public root", method: http.MethodGet, target: "/", want: http.StatusNoContent},
		{name: "authorized api get", method: http.MethodGet, target: "/api/config", headers: map[string]string{"Authorization": "Bearer secret"}, want: http.StatusNoContent},
		{name: "missing bearer", method: http.MethodGet, target: "/api/config", want: http.StatusUnauthorized, wantErr: "unauthorized"},
		{name: "wrong bearer", method: http.MethodGet, target: "/api/config", headers: map[string]string{"Authorization": "Bearer nope"}, want: http.StatusUnauthorized, wantErr: "unauthorized"},
		{name: "bearer prefix of key", method: http.MethodGet, target: "/api/config", headers: map[string]string{"Authorization": "Bearer secre"}, want: http.StatusUnauthorized, wantErr: "unauthorized"},
		{name: "bearer key with suffix", method: http.MethodGet, target: "/api/config", headers: map[string]string{"Authorization": "Bearer secrets"}, want: http.StatusUnauthorized, wantErr: "unauthorized"},
		{name: "basic scheme with key", method: http.MethodGet, target: "/api/config", headers: map[string]string{"Authorization": "Basic secret"}, want: http.StatusUnauthorized, wantErr: "unauthorized"},
		{name: "sse wrong query token", method: http.MethodGet, target: "/api/tasks/stream?token=secre", want: http.StatusUnauthorized, wantErr: "unauthorized"},
		{name: "sse query token", method: http.MethodGet, target: "/api/tasks/stream?token=secret", want: http.StatusNoContent},
		{name: "sse wrong header only", method: http.MethodGet, target: "/api/tasks/stream", headers: map[string]string{"Authorization": "Bearer secret"}, want: http.StatusUnauthorized, wantErr: "unauthorized"},
		{name: "logs sse query token", method: http.MethodGet, target: "/api/tasks/123/logs?token=secret", want: http.StatusNoContent},
	}

	mw := BearerAuthMiddleware("secret", false)
	next := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			next.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d body=%s", w.Code, tc.want, w.Body.String())
			}
			if tc.wantErr == "" {
				return
			}
			var resp map[string]string
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp["error"] != tc.wantErr {
				t.Fatalf("error = %q, want %q", resp["error"], tc.wantErr)
			}
		})
	}
}

func TestBearerAuthMiddleware_PublicUIShell(t *testing.T) {
	next := BearerAuthMiddleware("generated-local-key", false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	public := []string{
		"/", "/assets/app-123.js", "/assets/app-123.css", "/fonts/ui.woff2", "/static/overview.png", "/favicon.ico",
		"/install", "/dashboard", "/agent-graph", "/agents", "/workflows", "/flows", "/routines", "/analytics",
		"/chat", "/plan", "/whiteboard", "/artifacts", "/mission", "/map", "/settings", "/docs", "/docs/guide/start",
	}
	for _, target := range public {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			t.Run(method+target, func(t *testing.T) {
				rec := httptest.NewRecorder()
				next.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
				want := http.StatusNoContent
				if method == http.MethodPost {
					want = http.StatusUnauthorized
				}
				if rec.Code != want {
					t.Fatalf("status = %d, want %d", rec.Code, want)
				}
			})
		}
	}
	for _, target := range []string{"/api/config", "/api/tasks", "/api/docs/guide", "/artifact/private.html", "/internal/sandbox-proxy/llm/anthropic/v1/messages", "/assets/../api/config", "/static/../artifact/private.html"} {
		t.Run("protected"+target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

// TestBearerAuthMiddleware_SignInRoutes covers the redirect sign-in behind the
// server key. A browser cannot put the key on a navigation, so GET /login and
// GET /callback pass for a peer on this machine, the peer the index page hands
// the key to anyway. A peer on another host stays behind the key: a sign-in it
// completed would carry its own session past the key check. /logout and
// /logout/notify clear the machine's stored token as well as the caller's
// cookie, so they keep the key for every peer.
func TestBearerAuthMiddleware_SignInRoutes(t *testing.T) {
	const (
		loopback = "127.0.0.1:52100"
		remote   = "192.168.1.20:52100"
	)
	next := BearerAuthMiddleware("generated-local-key", false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	tests := []struct {
		name   string
		method string
		target string
		peer   string
		want   int
	}{
		{"login from this machine", http.MethodGet, "/login", loopback, http.StatusNoContent},
		{"login with an org from this machine", http.MethodGet, "/login?org_id=org-b", loopback, http.StatusNoContent},
		{"callback from this machine", http.MethodGet, "/callback?code=c&state=s", loopback, http.StatusNoContent},
		{"login from this machine over IPv6", http.MethodGet, "/login", "[::1]:52100", http.StatusNoContent},
		{"login from another host", http.MethodGet, "/login", remote, http.StatusUnauthorized},
		{"callback from another host", http.MethodGet, "/callback?code=c&state=s", remote, http.StatusUnauthorized},
		{"login by POST", http.MethodPost, "/login", loopback, http.StatusUnauthorized},
		{"login by HEAD", http.MethodHead, "/login", loopback, http.StatusUnauthorized},
		{"callback by POST", http.MethodPost, "/callback", loopback, http.StatusUnauthorized},
		{"path under login", http.MethodGet, "/login/x", loopback, http.StatusUnauthorized},
		{"unclean path to login", http.MethodGet, "/api/../login", loopback, http.StatusUnauthorized},
		{"logout from this machine", http.MethodGet, "/logout", loopback, http.StatusUnauthorized},
		{"logout notify from this machine", http.MethodGet, "/logout/notify", loopback, http.StatusUnauthorized},
		{"api from this machine", http.MethodGet, "/api/config", loopback, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			req.RemoteAddr = tc.peer
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("%s %s from %s: status = %d, want %d", tc.method, tc.target, tc.peer, rec.Code, tc.want)
			}
		})
	}
}

// TestBearerAuthMiddleware_IdentityBypass covers a request whose context
// already carries a validated principal (populated upstream by
// auth.CookieAuth or auth.OptionalAuth). In cloud mode the identity stands in
// for the static key from any peer, so cookie-only and JWT-bearer clients
// work in a deployment that also sets WALLFACER_SERVER_API_KEY for scripts.
// Outside cloud mode it does so only for a peer on this machine, which the
// index page hands the key to anyway; a peer on another host needs the key
// whatever identity it presents, on JSON routes and streaming paths alike.
func TestBearerAuthMiddleware_IdentityBypass(t *testing.T) {
	const (
		loopback = "127.0.0.1:52100"
		remote   = "192.168.1.20:52100"
	)
	tests := []struct {
		name     string
		cloud    bool
		peer     string
		target   string
		bearer   string
		identity bool
		want     int
	}{
		{"local, identity, this machine", false, loopback, "/api/config", "", true, http.StatusNoContent},
		{"local, identity, this machine over IPv6", false, "[::1]:52100", "/api/config", "", true, http.StatusNoContent},
		{"local, identity, this machine, stream", false, loopback, "/api/terminal/ws", "", true, http.StatusNoContent},
		{"local, identity, another host", false, remote, "/api/config", "", true, http.StatusUnauthorized},
		{"local, identity, another host, terminal", false, remote, "/api/terminal/ws", "", true, http.StatusUnauthorized},
		{"local, identity, another host, task logs", false, remote, "/api/tasks/123/logs", "", true, http.StatusUnauthorized},
		{"local, identity, another host, logout", false, remote, "/logout", "", true, http.StatusUnauthorized},
		{"local, identity and key, another host", false, remote, "/api/config", "secret", true, http.StatusNoContent},
		{"local, identity and query key, another host, terminal", false, remote, "/api/terminal/ws?token=secret", "", true, http.StatusNoContent},
		{"local, no identity, this machine", false, loopback, "/api/config", "", false, http.StatusUnauthorized},
		{"cloud, identity, another host", true, remote, "/api/config", "", true, http.StatusNoContent},
		{"cloud, identity, another host, stream", true, remote, "/api/terminal/ws", "", true, http.StatusNoContent},
		{"cloud, no identity, another host", true, remote, "/api/config", "", false, http.StatusUnauthorized},
		{"cloud, key, another host", true, remote, "/api/config", "secret", false, http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			next := BearerAuthMiddleware("secret", tc.cloud)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			req.RemoteAddr = tc.peer
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			if tc.identity {
				req = req.WithContext(auth.WithIdentity(req.Context(), &authkit.Identity{Sub: "user-xyz"}))
			}
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("GET %s from %s (cloud=%v, identity=%v): status = %d, want %d",
					tc.target, tc.peer, tc.cloud, tc.identity, rec.Code, tc.want)
			}
		})
	}
}

func TestSubmitFeedback_BodyTooLarge(t *testing.T) {
	h := newTestHandler(t)
	taskID := createWaitingTask(t, h, "test prompt")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/tasks/"+taskID.String()+"/feedback", oversizedBody("message", 600<<10))
	r.Body = http.MaxBytesReader(w, r.Body, BodyLimitFeedback)

	h.SubmitFeedback(w, r, taskID)

	assertBodyTooLarge(t, w)
}

func TestUpdateEnvConfig_BodyTooLarge(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/env", oversizedBody("oauth_token", int(BodyLimitDefault)+100))
	r.Body = http.MaxBytesReader(w, r.Body, BodyLimitDefault)

	h.UpdateEnvConfig(w, r)

	assertBodyTooLarge(t, w)
}

func TestDecodeJSONBody_Returns413ForMaxBytesError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", oversizedBody("x", 100))
	r.Body = http.MaxBytesReader(w, r.Body, 10)

	_, ok := httpjson.DecodeBody[struct {
		X string `json:"x"`
	}](w, r)
	if ok {
		t.Fatal("expected DecodeBody to fail")
	}

	assertBodyTooLarge(t, w)
}

// TestMaxBytesMiddleware_AllowsSmallBody verifies that small bodies pass through.
func TestMaxBytesMiddleware_AllowsSmallBody(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := MaxBytesMiddleware(1024)(next)
	body := strings.NewReader(`{"key":"value"}`)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", body)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for small body, got %d", w.Code)
	}
}
