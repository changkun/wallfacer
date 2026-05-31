// Package sse writes Server-Sent Events to an http.ResponseWriter.
//
// The package handles the small but repetitive boilerplate every SSE
// handler shares: asserting the writer supports flushing, setting the
// Content-Type / Cache-Control / Connection headers, formatting event
// frames, and emitting heartbeat keepalives. Callers stay imperative
// — drive their own loop, choose what to send — and only delegate the
// wire format.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Writer writes Server-Sent Events to an http.ResponseWriter. Construct
// one with NewWriter; methods return the underlying io.Writer error so
// callers can break out of their stream loop on client disconnect.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter sets the standard SSE response headers on w and returns a
// Writer. If w does not implement http.Flusher (streaming unsupported
// by the transport, e.g. some test recorders or buffered proxies), the
// helper writes a 500 to w and returns nil so callers can `if s ==
// nil { return }` and exit cleanly.
func NewWriter(w http.ResponseWriter) *Writer {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	return &Writer{w: w, flusher: flusher}
}

// Event writes a named SSE event with the given pre-encoded data
// payload and flushes. data is written verbatim — pre-marshal JSON
// yourself, or use JSON for the common case.
func (s *Writer) Event(name string, data []byte) error {
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// EventID writes a named event with an SSE id, used by clients that
// reconnect with Last-Event-ID for delta replay. id is written
// verbatim; callers typically format an int64 sequence as decimal.
func (s *Writer) EventID(id, name string, data []byte) error {
	if _, err := fmt.Fprintf(s.w, "id: %s\nevent: %s\ndata: %s\n\n", id, name, data); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// JSON marshals v as JSON and writes it as a named event. Returns
// the marshal error or the underlying write error.
func (s *Writer) JSON(name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Event(name, data)
}

// Heartbeat writes a "heartbeat" event with empty data. Sent as a real
// event (not an SSE comment) so the browser EventSource dispatches it
// to JavaScript — the frontend uses heartbeat arrivals to detect stale
// connections and trigger a recovery fetch.
func (s *Writer) Heartbeat() error {
	if _, err := fmt.Fprint(s.w, "event: heartbeat\ndata: {}\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// Flush forces any buffered data to be sent to the client. Most call
// sites do not need this — Event, EventID, JSON, and Heartbeat all
// flush implicitly. Use Flush only when writing raw bytes via the
// underlying ResponseWriter (rare).
func (s *Writer) Flush() {
	s.flusher.Flush()
}
