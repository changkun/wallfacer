package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/harness"
)

// captureRW is a minimal http.ResponseWriter that records everything written,
// and satisfies http.Flusher so normalizingWriter's Flush passthrough works.
type captureRW struct {
	hdr     http.Header
	body    strings.Builder
	flushed int
}

func (c *captureRW) Header() http.Header {
	if c.hdr == nil {
		c.hdr = http.Header{}
	}
	return c.hdr
}
func (c *captureRW) Write(p []byte) (int, error) { return c.body.WriteString(string(p)) }
func (c *captureRW) WriteHeader(int)             {}
func (c *captureRW) Flush()                      { c.flushed++ }

func loadFixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "harness", "testdata", rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return b
}

// runNormalized feeds raw bytes through a normalizingWriter for the given
// harness (optionally one byte at a time to exercise chunk splitting) and
// returns the decoded normalized events.
func runNormalized(t *testing.T, id harness.ID, raw []byte, byteByByte bool) []normalizedEvent {
	t.Helper()
	h, ok := harness.Lookup(id)
	if !ok {
		t.Fatalf("harness %s not registered", id)
	}
	capRW := &captureRW{}
	nw := &normalizingWriter{w: capRW, h: h}
	if byteByByte {
		for i := range raw {
			_, _ = nw.Write(raw[i : i+1])
		}
	} else {
		_, _ = nw.Write(raw)
	}
	nw.finish()

	var out []normalizedEvent
	for _, line := range strings.Split(strings.TrimSpace(capRW.body.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e normalizedEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("normalized line is not valid JSON: %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

func kinds(evts []normalizedEvent) []string {
	ks := make([]string, len(evts))
	for i, e := range evts {
		ks[i] = e.Kind
	}
	return ks
}

func TestNormalizingWriter_OpenCodeToolCalls(t *testing.T) {
	evts := runNormalized(t, harness.OpenCode, loadFixture(t, "opencode/tool-calls.jsonl"), false)

	// step_start→system_init, two tool_use(completed/error)→tool_end,
	// text→assistant, reasoning→thinking, step_finish→dropped, result→result.
	want := []string{"system_init", "tool_end", "tool_end", "assistant", "thinking", "result"}
	if got := kinds(evts); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("kinds = %v, want %v", got, want)
	}

	// The thinking row carries the reasoning prose.
	var thinking *normalizedEvent
	var toolErr *normalizedEvent
	var result *normalizedEvent
	for i := range evts {
		switch evts[i].Kind {
		case "thinking":
			thinking = &evts[i]
		case "tool_end":
			if evts[i].Tool != nil && evts[i].Tool.Error != "" {
				toolErr = &evts[i]
			}
		case "result":
			result = &evts[i]
		}
	}
	if thinking == nil || thinking.Text != "thinking about the directory listing" {
		t.Errorf("thinking event = %+v", thinking)
	}
	if toolErr == nil || !strings.Contains(toolErr.Tool.Error, "No such file") {
		t.Errorf("expected a tool_end carrying the error body, got %+v", toolErr)
	}
	if result == nil || result.Usage == nil || result.Usage.InputTokens != 540 {
		t.Errorf("result usage not surfaced: %+v", result)
	}
}

func TestNormalizingWriter_ChunkBoundaries(t *testing.T) {
	raw := loadFixture(t, "opencode/tool-calls.jsonl")
	whole := runNormalized(t, harness.OpenCode, raw, false)
	split := runNormalized(t, harness.OpenCode, raw, true)
	if strings.Join(kinds(whole), ",") != strings.Join(kinds(split), ",") {
		t.Errorf("byte-by-byte feed produced different events:\n whole=%v\n split=%v", kinds(whole), kinds(split))
	}
	if len(whole) != len(split) {
		t.Fatalf("event count differs: whole=%d split=%d", len(whole), len(split))
	}
}

func TestNormalizingWriter_DropsNoiseAndUnknown(t *testing.T) {
	// stderr text, a blank line, and a JSON line the harness does not recognise
	// must all be dropped from the normalized stream.
	raw := []byte("a plain stderr line\n\n{\"type\":\"totally_unknown_event\"}\n")
	evts := runNormalized(t, harness.OpenCode, raw, false)
	if len(evts) != 0 {
		t.Errorf("expected no normalized events from noise, got %v", kinds(evts))
	}
}

func TestNormalizingWriter_CursorAndPi(t *testing.T) {
	cursor := runNormalized(t, harness.Cursor, loadFixture(t, "cursor/headless-run.ndjson"), false)
	if len(cursor) == 0 {
		t.Fatal("cursor produced no normalized events")
	}
	var sawCursorTool, sawCursorResult bool
	for _, e := range cursor {
		if strings.HasPrefix(e.Kind, "tool_") {
			sawCursorTool = true
		}
		if e.Kind == "result" {
			sawCursorResult = true
		}
	}
	if !sawCursorTool || !sawCursorResult {
		t.Errorf("cursor: tool=%v result=%v; kinds=%v", sawCursorTool, sawCursorResult, kinds(cursor))
	}

	codex := runNormalized(t, harness.Codex, loadFixture(t, "codex/headless-run.ndjson"), false)
	var sawCodexTool, sawCodexThinking, sawCodexAssistant, sawCodexToolErr bool
	for _, e := range codex {
		switch e.Kind {
		case "tool_start", "tool_end":
			sawCodexTool = true
			if e.Tool != nil && e.Tool.Error != "" {
				sawCodexToolErr = true
			}
		case "thinking":
			sawCodexThinking = true
		case "assistant":
			if e.Text != "" {
				sawCodexAssistant = true
			}
		}
	}
	if !sawCodexTool || !sawCodexThinking || !sawCodexAssistant || !sawCodexToolErr {
		t.Errorf("codex: tool=%v thinking=%v assistant=%v toolErr=%v; kinds=%v",
			sawCodexTool, sawCodexThinking, sawCodexAssistant, sawCodexToolErr, kinds(codex))
	}

	pi := runNormalized(t, harness.Pi, loadFixture(t, "pi/headless-run.ndjson"), false)
	var sawPiTool, sawPiAssistant bool
	for _, e := range pi {
		if strings.HasPrefix(e.Kind, "tool_") {
			sawPiTool = true
		}
		if e.Kind == "assistant" && e.Text != "" {
			sawPiAssistant = true
		}
	}
	if !sawPiTool || !sawPiAssistant {
		t.Errorf("pi: tool=%v assistant=%v; kinds=%v", sawPiTool, sawPiAssistant, kinds(pi))
	}
}
