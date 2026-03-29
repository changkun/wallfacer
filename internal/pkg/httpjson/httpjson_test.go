package httpjson

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeBody_Success(t *testing.T) {
	body := `{"name":"alice","age":30}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	type payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	v, ok := DecodeBody[payload](w, r)
	if !ok {
		t.Fatalf("DecodeBody returned false; response: %s", w.Body.String())
	}
	if v.Name != "alice" || v.Age != 30 {
		t.Fatalf("unexpected value: %+v", v)
	}
}

func TestDecodeBody_InvalidJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{bad`))
	w := httptest.NewRecorder()

	v, ok := DecodeBody[map[string]any](w, r)
	if ok {
		t.Fatal("expected DecodeBody to fail on invalid JSON")
	}
	if v != nil {
		t.Fatal("expected nil on failure")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid JSON") {
		t.Fatalf("expected error message containing 'invalid JSON', got: %s", w.Body.String())
	}
}

func TestDecodeBody_UnknownFields(t *testing.T) {
	body := `{"name":"alice","extra":"field"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	type payload struct {
		Name string `json:"name"`
	}
	_, ok := DecodeBody[payload](w, r)
	if ok {
		t.Fatal("expected DecodeBody to reject unknown fields")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDecodeBody_TrailingContent(t *testing.T) {
	body := `{"name":"alice"}{"name":"bob"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	type payload struct {
		Name string `json:"name"`
	}
	_, ok := DecodeBody[payload](w, r)
	if ok {
		t.Fatal("expected DecodeBody to reject trailing content")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDecodeBody_MaxBytesError(t *testing.T) {
	// Build a valid JSON body that exceeds the limit so that the JSON
	// decoder surfaces the MaxBytesError rather than a syntax error.
	bigBody := `{"field":"` + strings.Repeat("a", 1024) + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(bigBody))
	r.Body = http.MaxBytesReader(w, r.Body, 10)

	v, ok := DecodeBody[map[string]any](w, r)
	if ok {
		t.Fatal("expected DecodeBody to fail on oversized body")
	}
	if v != nil {
		t.Fatal("expected nil on failure")
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}
	// The response should be JSON from Write.
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got: %s", w.Body.String())
	}
	if resp["error"] != "request body too large" {
		t.Fatalf("unexpected error: %s", resp["error"])
	}
}

func TestDecodeOptionalBody_EmptyBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	w := httptest.NewRecorder()

	type payload struct {
		Name string `json:"name"`
	}
	v, ok := DecodeOptionalBody[payload](w, r)
	if !ok {
		t.Fatalf("DecodeOptionalBody returned false on empty body")
	}
	if v.Name != "" {
		t.Fatalf("expected zero-value, got: %+v", v)
	}
}

func TestDecodeOptionalBody_NilBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Body = nil
	w := httptest.NewRecorder()

	type payload struct {
		Name string
	}
	v, ok := DecodeOptionalBody[payload](w, r)
	if !ok {
		t.Fatal("DecodeOptionalBody returned false on nil body")
	}
	if v == nil {
		t.Fatal("expected non-nil zero-value pointer")
	}
}

func TestDecodeOptionalBody_NilRequest(t *testing.T) {
	w := httptest.NewRecorder()
	type payload struct {
		Name string
	}
	v, ok := DecodeOptionalBody[payload](w, nil)
	if !ok {
		t.Fatal("DecodeOptionalBody returned false on nil request")
	}
	if v == nil {
		t.Fatal("expected non-nil zero-value pointer")
	}
}

func TestDecodeOptionalBody_WithContent(t *testing.T) {
	body := `{"name":"bob"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	type payload struct {
		Name string `json:"name"`
	}
	v, ok := DecodeOptionalBody[payload](w, r)
	if !ok {
		t.Fatalf("DecodeOptionalBody returned false; response: %s", w.Body.String())
	}
	if v.Name != "bob" {
		t.Fatalf("expected bob, got: %s", v.Name)
	}
}

func TestDecodeOptionalBody_InvalidJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{bad`))
	w := httptest.NewRecorder()

	v, ok := DecodeOptionalBody[map[string]any](w, r)
	if ok {
		t.Fatal("expected DecodeOptionalBody to fail on invalid JSON")
	}
	if v != nil {
		t.Fatal("expected nil on failure")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestWrite_Success(t *testing.T) {
	w := httptest.NewRecorder()
	Write(w, http.StatusOK, map[string]string{"key": "value"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
	var v map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if v["key"] != "value" {
		t.Fatalf("expected value, got %s", v["key"])
	}
}

func TestWrite_CustomStatus(t *testing.T) {
	for _, code := range []int{http.StatusCreated, http.StatusAccepted, http.StatusNotFound} {
		w := httptest.NewRecorder()
		Write(w, code, map[string]string{})
		if w.Code != code {
			t.Errorf("expected %d, got %d", code, w.Code)
		}
	}
}

func TestWrite_SlicePayload(t *testing.T) {
	w := httptest.NewRecorder()
	Write(w, http.StatusOK, []string{"a", "b", "c"})

	var v []string
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(v) != 3 || v[0] != "a" {
		t.Fatalf("unexpected: %v", v)
	}
}

func TestWrite_EncodingError(t *testing.T) {
	// json.Encoder.Encode returns an error for channels.
	w := httptest.NewRecorder()
	Write(w, http.StatusOK, make(chan int))
	// The status is already written before encoding fails, so we just
	// verify it doesn't panic and still sets the content type.
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
}
