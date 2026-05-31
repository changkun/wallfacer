package sse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// nonFlushingRecorder embeds httptest.ResponseRecorder and hides the
// Flush method via a wrapper that intentionally does NOT implement
// http.Flusher, so we can exercise the streaming-unsupported branch.
type nonFlushingRecorder struct {
	rec *httptest.ResponseRecorder
}

func (n *nonFlushingRecorder) Header() http.Header        { return n.rec.Header() }
func (n *nonFlushingRecorder) Write(b []byte) (int, error) { return n.rec.Write(b) }
func (n *nonFlushingRecorder) WriteHeader(code int)       { n.rec.WriteHeader(code) }

func TestNewWriter_SetsHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewWriter(rec)
	if s == nil {
		t.Fatal("NewWriter returned nil; expected a Writer")
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if got := rec.Header().Get("Connection"); got != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", got)
	}
}

func TestNewWriter_NoFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	n := &nonFlushingRecorder{rec: rec}
	if s := NewWriter(n); s != nil {
		t.Fatal("expected NewWriter to return nil when streaming unsupported")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestEvent_Format(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewWriter(rec)
	if err := s.Event("update", []byte(`{"x":1}`)); err != nil {
		t.Fatalf("Event: %v", err)
	}
	got := rec.Body.String()
	want := "event: update\ndata: {\"x\":1}\n\n"
	if got != want {
		t.Errorf("frame = %q, want %q", got, want)
	}
}

func TestEventID_Format(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewWriter(rec)
	if err := s.EventID("42", "delta", []byte(`{}`)); err != nil {
		t.Fatalf("EventID: %v", err)
	}
	got := rec.Body.String()
	want := "id: 42\nevent: delta\ndata: {}\n\n"
	if got != want {
		t.Errorf("frame = %q, want %q", got, want)
	}
}

func TestJSON_EncodesAndWrites(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewWriter(rec)
	if err := s.JSON("snapshot", map[string]int{"n": 7}); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	got := rec.Body.String()
	if !strings.HasPrefix(got, "event: snapshot\ndata: ") {
		t.Errorf("frame prefix wrong: %q", got)
	}
	if !strings.Contains(got, `"n":7`) {
		t.Errorf("frame payload missing n:7: %q", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Errorf("frame must end with blank line: %q", got)
	}
}

func TestHeartbeat_Format(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewWriter(rec)
	if err := s.Heartbeat(); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	got := rec.Body.String()
	want := "event: heartbeat\ndata: {}\n\n"
	if got != want {
		t.Errorf("heartbeat = %q, want %q", got, want)
	}
}

func TestEvent_FlushedEachCall(t *testing.T) {
	rec := httptest.NewRecorder()
	s := NewWriter(rec)
	_ = s.Event("a", []byte("1"))
	if !rec.Flushed {
		t.Error("expected ResponseRecorder.Flushed = true after Event")
	}
}
