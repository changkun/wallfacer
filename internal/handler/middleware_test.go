package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/store"
)

func oversizedBody(fieldName string, size int) io.Reader {
	padding := strings.Repeat("a", size)
	return strings.NewReader(`{"` + fieldName + `":"` + padding + `"}`)
}

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

func TestCreateTask_BodyTooLarge(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/tasks", oversizedBody("prompt", 2<<20))
	r.Body = http.MaxBytesReader(w, r.Body, BodyLimitDefault)

	h.CreateTask(w, r)

	assertBodyTooLarge(t, w)
}

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

func TestRefineApply_BodyTooLarge(t *testing.T) {
	h := newTestHandler(t)
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/apply", oversizedBody("prompt", int(BodyLimitDefault)+100))
	r.Body = http.MaxBytesReader(w, r.Body, BodyLimitDefault)

	h.RefineApply(w, r, task.ID)

	assertBodyTooLarge(t, w)
}

func TestDecodeJSONBody_Returns413ForMaxBytesError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", oversizedBody("x", 100))
	r.Body = http.MaxBytesReader(w, r.Body, 10)

	var v struct {
		X string `json:"x"`
	}
	if ok := decodeJSONBody(w, r, &v); ok {
		t.Fatal("expected decodeJSONBody to fail")
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
