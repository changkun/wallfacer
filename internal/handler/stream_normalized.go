package handler

import (
	"bytes"
	"encoding/json"
	"net/http"

	"latere.ai/x/wallfacer/internal/harness"
)

// normalizedEvent is the wire shape served by GET /api/tasks/{id}/logs?format=normalized.
// It is a stable projection of harness.Event so the frontend can render a
// trajectory uniformly across every harness without knowing each native
// dialect. Kind is a string token (harness.EventKind.String()) so the wire
// never depends on the enum's int ordering.
type normalizedEvent struct {
	Kind       string           `json:"kind"`
	Text       string           `json:"text,omitempty"`
	Subtype    string           `json:"subtype,omitempty"`
	Tool       *normalizedTool  `json:"tool,omitempty"`
	Usage      *normalizedUsage `json:"usage,omitempty"`
	StopReason string           `json:"stop_reason,omitempty"`
	SessionID  string           `json:"session_id,omitempty"`
}

type normalizedTool struct {
	Name   string          `json:"name,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type normalizedUsage struct {
	InputTokens         int     `json:"input_tokens,omitempty"`
	OutputTokens        int     `json:"output_tokens,omitempty"`
	CacheReadTokens     int     `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int     `json:"cache_creation_tokens,omitempty"`
	CostUSD             float64 `json:"cost_usd,omitempty"`
}

// toNormalized projects a parsed harness.Event onto the wire DTO.
func toNormalized(evt harness.Event) normalizedEvent {
	out := normalizedEvent{
		Kind:       evt.Kind.String(),
		Text:       evt.Text,
		Subtype:    evt.Subtype,
		StopReason: evt.StopReason,
		SessionID:  evt.SessionID,
	}
	if evt.Tool != nil {
		out.Tool = &normalizedTool{
			Name:   evt.Tool.Name,
			Input:  evt.Tool.Input,
			Output: evt.Tool.Output,
			Error:  evt.Tool.Error,
		}
	}
	if evt.Usage != nil {
		out.Usage = &normalizedUsage{
			InputTokens:         evt.Usage.InputTokens,
			OutputTokens:        evt.Usage.OutputTokens,
			CacheReadTokens:     evt.Usage.CacheReadTokens,
			CacheCreationTokens: evt.Usage.CacheCreationTokens,
			CostUSD:             evt.Usage.CostUSD,
		}
	}
	return out
}

// normalizingWriter wraps the log ResponseWriter and rewrites the raw,
// harness-native NDJSON stream into normalized-event NDJSON on the fly. It
// buffers across Write calls so a JSON line split across chunk boundaries is
// still parsed exactly once; finish() flushes any trailing newline-less line.
//
// It is installed at the top of StreamLogs so every downstream serve path
// (live relay, stored turns, phase filters) inherits normalization without
// change. Lines the harness does not recognise (KindUnknown) and non-JSON
// noise (stderr, keepalive newlines) are dropped — the raw view still carries
// them; the normalized view is events only.
type normalizingWriter struct {
	w   http.ResponseWriter
	h   harness.Harness
	buf []byte
}

func (nw *normalizingWriter) Header() http.Header { return nw.w.Header() }

func (nw *normalizingWriter) WriteHeader(code int) { nw.w.WriteHeader(code) }

// Flush satisfies http.Flusher so the live-stream path (which asserts
// w.(http.Flusher)) keeps flushing through the wrapper to the client.
func (nw *normalizingWriter) Flush() {
	if f, ok := nw.w.(http.Flusher); ok {
		f.Flush()
	}
}

func (nw *normalizingWriter) Write(p []byte) (int, error) {
	nw.buf = append(nw.buf, p...)
	for {
		i := bytes.IndexByte(nw.buf, '\n')
		if i < 0 {
			break
		}
		nw.emit(nw.buf[:i])
		nw.buf = nw.buf[i+1:]
	}
	// Report the caller's bytes as fully consumed; we buffer internally.
	return len(p), nil
}

func (nw *normalizingWriter) emit(line []byte) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return
	}
	evt, err := nw.h.ParseEvent(trimmed)
	if err != nil || evt.Kind == harness.KindUnknown {
		return
	}
	b, err := json.Marshal(toNormalized(evt))
	if err != nil {
		return
	}
	_, _ = nw.w.Write(b)
	_, _ = nw.w.Write([]byte("\n"))
}

// finish flushes any buffered trailing line (a final JSON object with no
// terminating newline). Call once after all writes complete.
func (nw *normalizingWriter) finish() {
	if len(bytes.TrimSpace(nw.buf)) > 0 {
		nw.emit(nw.buf)
		nw.buf = nil
	}
}
