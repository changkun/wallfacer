package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	task, err := h.store.CreateTask(context.Background(), "test", 15, false, "", "")
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
