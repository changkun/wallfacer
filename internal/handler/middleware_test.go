package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/auth"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
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
		{name: "sse query token", method: http.MethodGet, target: "/api/tasks/stream?token=secret", want: http.StatusNoContent},
		{name: "sse wrong header only", method: http.MethodGet, target: "/api/tasks/stream", headers: map[string]string{"Authorization": "Bearer secret"}, want: http.StatusUnauthorized, wantErr: "unauthorized"},
		{name: "logs sse query token", method: http.MethodGet, target: "/api/tasks/123/logs?token=secret", want: http.StatusNoContent},
	}

	mw := BearerAuthMiddleware("secret")
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

// TestBearerAuthMiddleware_ClaimsBypass confirms that a request whose
// context already carries a validated principal (populated upstream by
// auth.OptionalAuth in cloud mode) skips the static-key check. Keeps
// cookie-only and JWT-bearer clients working in a deployment that also
// sets WALLFACER_SERVER_API_KEY for scripts.
func TestBearerAuthMiddleware_ClaimsBypass(t *testing.T) {
	var served bool
	mw := BearerAuthMiddleware("secret")
	next := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served = true
		w.WriteHeader(http.StatusNoContent)
	}))

	// No Authorization header, but claims are already in context — should pass.
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{Sub: "user-xyz"}))
	w := httptest.NewRecorder()
	next.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (claims bypass)", w.Code)
	}
	if !served {
		t.Fatal("next handler not invoked")
	}
}

func TestUpdateInstructions_BodyTooLarge(t *testing.T) {
	h, _ := newTestHandlerWithInstructions(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/instructions", oversizedBody("content", 6<<20))
	r.Body = http.MaxBytesReader(w, r.Body, BodyLimitInstructions)

	h.UpdateInstructions(w, r)

	assertBodyTooLarge(t, w)
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
