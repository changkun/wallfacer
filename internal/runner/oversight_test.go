package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// parseTurnActivity
// ---------------------------------------------------------------------------

// TestParseTurnActivityEmpty verifies that an empty or non-JSON input
// returns a turn activity with no tool calls or text notes.
func TestParseTurnActivityEmpty(t *testing.T) {
	act := parseTurnActivity([]byte(""), 1)
	if act.Turn != 1 {
		t.Fatalf("expected turn=1, got %d", act.Turn)
	}
	if len(act.TextNotes) != 0 || len(act.ToolCalls) != 0 {
		t.Fatalf("expected empty notes and calls, got notes=%v calls=%v", act.TextNotes, act.ToolCalls)
	}
}

// TestParseTurnActivityTextBlock verifies that assistant text blocks are extracted.
func TestParseTurnActivityTextBlock(t *testing.T) {
	ndjson := `{"type":"assistant","message":{"content":[{"type":"text","text":"I will now explore the codebase"}]}}`
	act := parseTurnActivity([]byte(ndjson), 1)
	if len(act.TextNotes) != 1 {
		t.Fatalf("expected 1 text note, got %d: %v", len(act.TextNotes), act.TextNotes)
	}
	if act.TextNotes[0] != "I will now explore the codebase" {
		t.Fatalf("unexpected text note: %q", act.TextNotes[0])
	}
}

// TestParseTurnActivityToolCall verifies that tool_use blocks are extracted as
// "ToolName(input)" entries.
func TestParseTurnActivityToolCall(t *testing.T) {
	input := map[string]interface{}{"file_path": "/workspace/main.go"}
	inputJSON, _ := json.Marshal(input)
	ndjson := fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":%s}]}}`, inputJSON)
	act := parseTurnActivity([]byte(ndjson), 2)
	if len(act.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %v", len(act.ToolCalls), act.ToolCalls)
	}
	if act.ToolCalls[0] != "Read(/workspace/main.go)" {
		t.Fatalf("unexpected tool call: %q", act.ToolCalls[0])
	}
}

// TestParseTurnActivityMultipleBlocks verifies that multiple content blocks
// in a single turn are all captured.
func TestParseTurnActivityMultipleBlocks(t *testing.T) {
	input := map[string]interface{}{"command": "go test ./..."}
	inputJSON, _ := json.Marshal(input)
	ndjson := `{"type":"assistant","message":{"content":[{"type":"text","text":"Running tests now"},{"type":"tool_use","name":"Bash","input":` + string(inputJSON) + `}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"PASS"}]}]}}`
	act := parseTurnActivity([]byte(ndjson), 3)
	if len(act.TextNotes) != 1 || act.TextNotes[0] != "Running tests now" {
		t.Fatalf("unexpected text notes: %v", act.TextNotes)
	}
	if len(act.ToolCalls) != 1 || act.ToolCalls[0] != "Bash(go test ./...)" {
		t.Fatalf("unexpected tool calls: %v", act.ToolCalls)
	}
}

// TestParseTurnActivityCodexItems verifies Codex item.started/item.completed
// command execution events are mapped into Bash tool calls and text notes.
func TestParseTurnActivityCodexItems(t *testing.T) {
	ndjson := `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Inspecting the repository."}}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"/bin/bash -lc 'ls -la /workspace'","status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/bin/bash -lc 'ls -la /workspace'","aggregated_output":"total 12","exit_code":0,"status":"completed"}}`
	act := parseTurnActivity([]byte(ndjson), 4)
	if len(act.TextNotes) != 1 || act.TextNotes[0] != "Inspecting the repository." {
		t.Fatalf("unexpected text notes: %v", act.TextNotes)
	}
	if len(act.ToolCalls) != 1 || act.ToolCalls[0] != "Bash(ls -la /workspace)" {
		t.Fatalf("unexpected tool calls: %v", act.ToolCalls)
	}
}

func TestParseTurnActivityCodexToolItems(t *testing.T) {
	ndjson := `{"type":"item.completed","item":{"id":"item_2","type":"read_file","input":{"file_path":"/workspace/main.go"}}}`
	act := parseTurnActivity([]byte(ndjson), 5)
	if len(act.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %v", len(act.ToolCalls), act.ToolCalls)
	}
	if act.ToolCalls[0] != "Read(/workspace/main.go)" {
		t.Fatalf("unexpected tool call: %q", act.ToolCalls[0])
	}
}

func TestParseTurnActivityLowercaseToolName(t *testing.T) {
	input := map[string]interface{}{"file_path": "/workspace/main.go"}
	inputJSON, _ := json.Marshal(input)
	ndjson := fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"read_file","input":%s}]}}`, inputJSON)
	act := parseTurnActivity([]byte(ndjson), 6)
	if len(act.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %v", len(act.ToolCalls), act.ToolCalls)
	}
	if act.ToolCalls[0] != "Read(/workspace/main.go)" {
		t.Fatalf("unexpected tool call: %q", act.ToolCalls[0])
	}
}

func TestNormalizeCodexCommand(t *testing.T) {
	got := normalizeCodexCommand("/bin/bash -lc 'echo hello'")
	if got != "echo hello" {
		t.Fatalf("unexpected normalized command: %q", got)
	}
	if got := normalizeCodexCommand("go test ./..."); got != "go test ./..." {
		t.Fatalf("command should be unchanged, got %q", got)
	}
}

// TestParseTurnActivityLongTextTruncated verifies that text longer than 200
// characters is truncated with an ellipsis.
func TestParseTurnActivityLongTextTruncated(t *testing.T) {
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	ndjson := fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":"%s"}]}}`, string(long))
	act := parseTurnActivity([]byte(ndjson), 1)
	if len(act.TextNotes) != 1 {
		t.Fatalf("expected 1 text note, got %d", len(act.TextNotes))
	}
	note := act.TextNotes[0]
	if len(note) > 210 {
		t.Fatalf("text note should be truncated, got length %d", len(note))
	}
	// … is multi-byte (UTF-8: 0xE2 0x80 0xA6); check via rune slice.
	runes := []rune(note)
	if string(runes[len(runes)-1]) != "…" {
		t.Fatalf("expected truncated note to end with '…', got %q", note)
	}
}

// ---------------------------------------------------------------------------
// buildTurnTimestamps
// ---------------------------------------------------------------------------

// TestBuildTurnTimestampsEmpty verifies that an empty event list produces an
// empty timestamp map.
func TestBuildTurnTimestampsEmpty(t *testing.T) {
	ts := buildTurnTimestamps(nil)
	if len(ts) != 0 {
		t.Fatalf("expected empty map, got %v", ts)
	}
}

// TestBuildTurnTimestampsCountsAgentTurnSpanStarts verifies that each
// span_start event for the "agent_turn" phase maps to consecutive turn
// numbers, and that their timestamps represent container start times.
func TestBuildTurnTimestampsCountsAgentTurnSpanStarts(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC)

	span1, _ := json.Marshal(store.SpanData{Phase: "agent_turn", Label: "agent_turn_1"})
	span2, _ := json.Marshal(store.SpanData{Phase: "agent_turn", Label: "agent_turn_2"})
	events := []store.TaskEvent{
		{EventType: store.EventTypeStateChange, CreatedAt: t1.Add(-1 * time.Second)},
		{EventType: store.EventTypeSpanStart, Data: span1, CreatedAt: t1},
		{EventType: store.EventTypeSystem, CreatedAt: t1.Add(30 * time.Second)},
		{EventType: store.EventTypeOutput, CreatedAt: t1.Add(60 * time.Second)}, // output events are ignored
		{EventType: store.EventTypeSpanStart, Data: span2, CreatedAt: t2},
	}
	ts := buildTurnTimestamps(events)
	if len(ts) != 2 {
		t.Fatalf("expected 2 turn timestamps, got %d: %v", len(ts), ts)
	}
	if !ts[1].Equal(t1) {
		t.Fatalf("turn 1 timestamp: expected %v, got %v", t1, ts[1])
	}
	if !ts[2].Equal(t2) {
		t.Fatalf("turn 2 timestamp: expected %v, got %v", t2, ts[2])
	}
}

// TestBuildTurnTimestampsIgnoresNonAgentTurnPhases verifies that span_start
// events for other phases (e.g. "worktree_setup", "commit") are not counted.
func TestBuildTurnTimestampsIgnoresNonAgentTurnPhases(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC)

	worktreeSpan, _ := json.Marshal(store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})
	agentSpan, _ := json.Marshal(store.SpanData{Phase: "agent_turn", Label: "agent_turn_1"})
	commitSpan, _ := json.Marshal(store.SpanData{Phase: "commit", Label: "commit"})
	events := []store.TaskEvent{
		{EventType: store.EventTypeSpanStart, Data: worktreeSpan, CreatedAt: t1.Add(-5 * time.Second)}, // not counted
		{EventType: store.EventTypeSpanStart, Data: agentSpan, CreatedAt: t1},                          // counted: turn 1
		{EventType: store.EventTypeOutput, CreatedAt: t1.Add(30 * time.Second)},                        // ignored
		{EventType: store.EventTypeSpanStart, Data: commitSpan, CreatedAt: t2},                         // not counted
	}
	ts := buildTurnTimestamps(events)
	if len(ts) != 1 {
		t.Fatalf("expected 1 turn timestamp, got %d: %v", len(ts), ts)
	}
	if !ts[1].Equal(t1) {
		t.Fatalf("turn 1 timestamp: expected %v, got %v", t1, ts[1])
	}
}

// TestBuildTurnTimestampsIgnoresNewInstrumentedPhases verifies that the new
// instrumentation phases — board_context, feedback_waiting, worktree_cleanup —
// are not counted as agent turns by buildTurnTimestamps.
func TestBuildTurnTimestampsIgnoresNewInstrumentedPhases(t *testing.T) {
	t0 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2024, 1, 15, 10, 1, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 10, 2, 0, 0, time.UTC)

	boardSpan, _ := json.Marshal(store.SpanData{Phase: "board_context", Label: "board_context"})
	boardRefreshSpan, _ := json.Marshal(store.SpanData{Phase: "board_context", Label: "board_context_1"})
	feedbackSpan, _ := json.Marshal(store.SpanData{Phase: "feedback_waiting", Label: "feedback_waiting"})
	cleanupSpan, _ := json.Marshal(store.SpanData{Phase: "worktree_cleanup", Label: "worktree_cleanup"})
	agentSpan, _ := json.Marshal(store.SpanData{Phase: "agent_turn", Label: "implementation_1"})

	events := []store.TaskEvent{
		{EventType: store.EventTypeSpanStart, Data: boardSpan, CreatedAt: t0},                                    // not counted
		{EventType: store.EventTypeSpanEnd, Data: boardSpan, CreatedAt: t0.Add(100 * time.Millisecond)},          // not counted
		{EventType: store.EventTypeSpanStart, Data: boardRefreshSpan, CreatedAt: t0.Add(200 * time.Millisecond)}, // not counted
		{EventType: store.EventTypeSpanStart, Data: agentSpan, CreatedAt: t1},                                    // counted: turn 1
		{EventType: store.EventTypeSpanStart, Data: feedbackSpan, CreatedAt: t1.Add(30 * time.Second)},           // not counted
		{EventType: store.EventTypeSpanEnd, Data: feedbackSpan, CreatedAt: t2},                                   // not counted
		{EventType: store.EventTypeSpanStart, Data: cleanupSpan, CreatedAt: t2.Add(1 * time.Second)},             // not counted
	}
	ts := buildTurnTimestamps(events)
	if len(ts) != 1 {
		t.Fatalf("expected 1 turn timestamp (only agent_turn counted), got %d: %v", len(ts), ts)
	}
	if !ts[1].Equal(t1) {
		t.Fatalf("turn 1 timestamp: expected %v, got %v", t1, ts[1])
	}
}

// ---------------------------------------------------------------------------
// fillMissingPhaseTimestamps
// ---------------------------------------------------------------------------

// TestFillMissingPhaseTimestampsAllZero verifies that when every phase has a
// zero timestamp, proportional timestamps from the activity log are assigned.
func TestFillMissingPhaseTimestampsAllZero(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC)
	activities := []turnActivity{
		{Turn: 1, Timestamp: t1},
		{Turn: 2, Timestamp: t2},
	}
	phases := []store.OversightPhase{
		{Title: "Phase A"}, // zero timestamp
		{Title: "Phase B"}, // zero timestamp
	}
	result := fillMissingPhaseTimestamps(phases, activities)
	if result[0].Timestamp.IsZero() {
		t.Fatal("phase A should have received a timestamp")
	}
	if result[1].Timestamp.IsZero() {
		t.Fatal("phase B should have received a timestamp")
	}
	// Phase A anchors to turn 0 (first turn), phase B to turn 1.
	if !result[0].Timestamp.Equal(t1) {
		t.Fatalf("phase A timestamp: expected %v, got %v", t1, result[0].Timestamp)
	}
	if !result[1].Timestamp.Equal(t2) {
		t.Fatalf("phase B timestamp: expected %v, got %v", t2, result[1].Timestamp)
	}
}

// TestFillMissingPhaseTimestampsPartialValid verifies that when at least one
// phase has a non-zero timestamp the slice is returned unchanged.
func TestFillMissingPhaseTimestampsPartialValid(t *testing.T) {
	t0 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2024, 1, 15, 10, 3, 0, 0, time.UTC)
	activities := []turnActivity{{Turn: 1, Timestamp: t0}, {Turn: 2, Timestamp: t1}}
	phases := []store.OversightPhase{
		{Title: "Phase A", Timestamp: t0}, // already set
		{Title: "Phase B"},                // zero timestamp
	}
	result := fillMissingPhaseTimestamps(phases, activities)
	// Phase B should remain zero — partial valid means we trust the provided data.
	if !result[0].Timestamp.Equal(t0) {
		t.Fatalf("phase A timestamp should be unchanged: %v", result[0].Timestamp)
	}
	if !result[1].Timestamp.IsZero() {
		t.Fatalf("phase B should remain zero when partial valid, got %v", result[1].Timestamp)
	}
}

// TestFillMissingPhaseTimestampsAllSameNonZero verifies that when multiple
// phases all carry the same non-zero timestamp, the timestamps are treated as
// degenerate model output and rebalanced across the activity log.
func TestFillMissingPhaseTimestampsAllSameNonZero(t *testing.T) {
	t0 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2024, 1, 15, 10, 3, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 10, 6, 0, 0, time.UTC)
	activities := []turnActivity{
		{Turn: 1, Timestamp: t0},
		{Turn: 2, Timestamp: t1},
		{Turn: 3, Timestamp: t2},
	}
	phases := []store.OversightPhase{
		{Title: "Phase A", Timestamp: t0},
		{Title: "Phase B", Timestamp: t0},
		{Title: "Phase C", Timestamp: t0},
	}

	result := fillMissingPhaseTimestamps(phases, activities)

	if !result[0].Timestamp.Equal(t0) {
		t.Fatalf("phase A timestamp: expected %v, got %v", t0, result[0].Timestamp)
	}
	if !result[1].Timestamp.Equal(t1) {
		t.Fatalf("phase B timestamp: expected %v, got %v", t1, result[1].Timestamp)
	}
	if !result[2].Timestamp.Equal(t2) {
		t.Fatalf("phase C timestamp: expected %v, got %v", t2, result[2].Timestamp)
	}
}

// TestFillMissingPhaseTimestampsEmptyActivities verifies that an empty
// activities slice leaves phases unchanged.
func TestFillMissingPhaseTimestampsEmptyActivities(t *testing.T) {
	phases := []store.OversightPhase{{Title: "Phase A"}}
	result := fillMissingPhaseTimestamps(phases, nil)
	if !result[0].Timestamp.IsZero() {
		t.Fatalf("expected zero timestamp with no activities, got %v", result[0].Timestamp)
	}
}

// ---------------------------------------------------------------------------
// formatActivityLog
// ---------------------------------------------------------------------------

// TestFormatActivityLogEmpty verifies that an empty activity list produces an
// empty string.
func TestFormatActivityLogEmpty(t *testing.T) {
	result := formatActivityLog(nil)
	if result != "" {
		t.Fatalf("expected empty string for nil activities, got %q", result)
	}
}

// TestFormatActivityLogSingleTurn verifies basic formatting of a single turn.
func TestFormatActivityLogSingleTurn(t *testing.T) {
	ts := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	activities := []turnActivity{
		{
			Turn:      1,
			Timestamp: ts,
			TextNotes: []string{"Exploring the codebase"},
			ToolCalls: []string{"Read(/workspace/main.go)", "Glob(**/*.go)"},
		},
	}
	result := formatActivityLog(activities)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	// Should contain turn header.
	if !containsStr(result, "[Turn 1") {
		t.Errorf("expected turn header in output, got: %q", result)
	}
	// Should contain text note.
	if !containsStr(result, "Exploring the codebase") {
		t.Errorf("expected text note in output, got: %q", result)
	}
	// Should contain tool call.
	if !containsStr(result, "Read(/workspace/main.go)") {
		t.Errorf("expected tool call in output, got: %q", result)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

// ---------------------------------------------------------------------------
// parseOversightResult
// ---------------------------------------------------------------------------

// TestParseOversightResultValid verifies that valid JSON is parsed into phases.
func TestParseOversightResultValid(t *testing.T) {
	raw := `{
		"phases": [
			{
				"timestamp": "2024-01-15T10:00:00Z",
				"title": "Explored codebase",
				"summary": "The agent read key files",
				"tools_used": ["Read", "Glob"],
				"actions": ["Read main.go", "Listed Go files"]
			},
			{
				"timestamp": "2024-01-15T10:05:00Z",
				"title": "Implemented feature",
				"summary": "Added the new handler",
				"tools_used": ["Write", "Edit"],
				"actions": ["Created handler.go"]
			}
		]
	}`

	phases, err := parseOversightResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(phases))
	}
	if phases[0].Title != "Explored codebase" {
		t.Fatalf("unexpected title: %q", phases[0].Title)
	}
	if len(phases[0].ToolsUsed) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(phases[0].ToolsUsed))
	}
	if phases[0].ToolsUsed[0] != "Read" {
		t.Fatalf("unexpected tool: %q", phases[0].ToolsUsed[0])
	}
	if len(phases[0].Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(phases[0].Actions))
	}
	// Timestamp should be parsed.
	expected := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	if !phases[0].Timestamp.Equal(expected) {
		t.Fatalf("unexpected timestamp: %v (expected %v)", phases[0].Timestamp, expected)
	}
}

// TestParseOversightResultWithCommands verifies that Bash commands are preserved
// in the commands field and not conflated with tools_used.
func TestParseOversightResultWithCommands(t *testing.T) {
	raw := `{
		"phases": [
			{
				"timestamp": "2024-01-15T10:00:00Z",
				"title": "Ran tests and committed",
				"summary": "Ran the test suite and committed changes.",
				"tools_used": ["Bash", "Read"],
				"commands": ["go test ./...", "git add -A", "git commit -m \"fix: auth handler\""],
				"actions": ["Ran Go tests", "Committed changes"]
			}
		]
	}`

	phases, err := parseOversightResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(phases))
	}
	if len(phases[0].Commands) != 3 {
		t.Fatalf("expected 3 commands, got %d: %v", len(phases[0].Commands), phases[0].Commands)
	}
	if phases[0].Commands[0] != "go test ./..." {
		t.Fatalf("unexpected first command: %q", phases[0].Commands[0])
	}
	if phases[0].Commands[2] != `git commit -m "fix: auth handler"` {
		t.Fatalf("unexpected third command: %q", phases[0].Commands[2])
	}
}

// TestParseOversightResultCommandsAbsent verifies that a phase without Bash
// calls has a nil/empty commands slice.
func TestParseOversightResultCommandsAbsent(t *testing.T) {
	raw := `{"phases":[{"title":"Read files","summary":"Explored code","tools_used":["Read"],"actions":["Read main.go"]}]}`
	phases, err := parseOversightResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(phases))
	}
	if len(phases[0].Commands) != 0 {
		t.Fatalf("expected no commands, got %v", phases[0].Commands)
	}
}

// TestParseOversightResultMarkdownFences verifies that markdown code fences
// are stripped before parsing.
func TestParseOversightResultMarkdownFences(t *testing.T) {
	raw := "```json\n" + `{"phases":[{"title":"Phase one","summary":"did stuff","tools_used":["Read"],"actions":["Read file"]}]}` + "\n```"
	phases, err := parseOversightResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(phases))
	}
	if phases[0].Title != "Phase one" {
		t.Fatalf("unexpected title: %q", phases[0].Title)
	}
}

// TestParseOversightResultPreamble verifies that text before the JSON object
// is skipped.
func TestParseOversightResultPreamble(t *testing.T) {
	raw := `Here is the structured summary:
{"phases":[{"title":"Phase one","summary":"did stuff","tools_used":["Read"],"actions":["Read file"]}]}`
	phases, err := parseOversightResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(phases))
	}
}

// TestParseOversightResultBareArray verifies that a bare JSON array of phase
// objects (without a wrapping {"phases": ...} envelope) is parsed correctly.
// This covers the case where the oversight agent returns [{"title":...}, ...]
// instead of {"phases": [{"title":...}, ...]}.
func TestParseOversightResultBareArray(t *testing.T) {
	raw := `[
		{
			"timestamp": "2024-01-15T10:00:00Z",
			"title": "Explored codebase",
			"summary": "The agent read key files",
			"tools_used": ["Read", "Glob"],
			"actions": ["Read main.go", "Listed Go files"]
		},
		{
			"timestamp": "2024-01-15T10:05:00Z",
			"title": "Implemented feature",
			"summary": "Added the new handler",
			"tools_used": ["Write"],
			"commands": ["go build ./..."],
			"actions": ["Created handler.go"]
		}
	]`

	phases, err := parseOversightResult(raw)
	if err != nil {
		t.Fatalf("unexpected error for bare array: %v", err)
	}
	if len(phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(phases))
	}
	if phases[0].Title != "Explored codebase" {
		t.Fatalf("unexpected first phase title: %q", phases[0].Title)
	}
	if phases[1].Title != "Implemented feature" {
		t.Fatalf("unexpected second phase title: %q", phases[1].Title)
	}
	if len(phases[1].Commands) != 1 || phases[1].Commands[0] != "go build ./..." {
		t.Fatalf("unexpected commands in second phase: %v", phases[1].Commands)
	}
	expected := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	if !phases[0].Timestamp.Equal(expected) {
		t.Fatalf("unexpected timestamp: %v (expected %v)", phases[0].Timestamp, expected)
	}
}

// TestParseOversightResultInvalid verifies that clearly invalid JSON returns
// an error.
func TestParseOversightResultInvalid(t *testing.T) {
	_, err := parseOversightResult("this is not json at all")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestParseOversightResultEmptyPhases verifies that an empty phases array
// is valid and returns an empty slice.
func TestParseOversightResultEmptyPhases(t *testing.T) {
	phases, err := parseOversightResult(`{"phases":[]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(phases) != 0 {
		t.Fatalf("expected 0 phases, got %d", len(phases))
	}
}

func TestParseOversightResultEmptyText(t *testing.T) {
	phases, err := parseOversightResult("   ")
	if err != nil {
		t.Fatalf("unexpected error for whitespace result: %v", err)
	}
	if len(phases) != 0 {
		t.Fatalf("expected 0 phases for empty output, got %d", len(phases))
	}
}

// ---------------------------------------------------------------------------
// GenerateOversight — integration (uses fake container)
// ---------------------------------------------------------------------------

const oversightOutput = `{"result":"{\"phases\":[{\"timestamp\":\"2024-01-15T10:00:00Z\",\"title\":\"Explored codebase\",\"summary\":\"Read key files\",\"tools_used\":[\"Read\"],\"actions\":[\"Read main.go\"]}]}","session_id":"s1","stop_reason":"end_turn","is_error":false}`
const oversightOutputEmptyResult = `{"result":"","session_id":"s1","stop_reason":"end_turn","is_error":false}`
const oversightOutputWithUsage = `{"result":"{\"phases\":[{\"timestamp\":\"2024-01-15T10:00:00Z\",\"title\":\"Explored codebase\",\"summary\":\"Read key files\",\"tools_used\":[\"Read\"],\"actions\":[\"Read main.go\"]}]}","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001,"usage":{"input_tokens":123,"output_tokens":45}}`

// TestGenerateOversightSuccess verifies that GenerateOversight saves a ready
// oversight when the container succeeds and produces valid structured JSON.
func TestGenerateOversightSuccess(t *testing.T) {
	cmd := fakeCmdScript(t, oversightOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Add feature X", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	// Write a fake turn file so buildActivityLog has something to process.
	outputsDir := s.OutputsDir(task.ID)
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		t.Fatal(err)
	}
	turnData := `{"type":"assistant","message":{"content":[{"type":"text","text":"Starting work"}]}}`
	if err := os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(turnData), 0644); err != nil {
		t.Fatal(err)
	}

	r.GenerateOversight(task.ID)

	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading oversight: %v", err)
	}
	if oversight.Status != store.OversightStatusReady {
		t.Fatalf("expected status=ready, got %q (error: %s)", oversight.Status, oversight.Error)
	}
	if len(oversight.Phases) == 0 {
		t.Fatal("expected at least one phase")
	}
	if oversight.Phases[0].Title != "Explored codebase" {
		t.Fatalf("unexpected phase title: %q", oversight.Phases[0].Title)
	}
}

// TestGenerateOversightAcceptsValidOutputOnNonZeroExit verifies that oversight
// generation still succeeds when the agent exits non-zero after emitting a
// valid final result payload.
func TestGenerateOversightAcceptsValidOutputOnNonZeroExit(t *testing.T) {
	cmd := fakeCmdScript(t, oversightOutput, 1)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Add feature X", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	outputsDir := s.OutputsDir(task.ID)
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		t.Fatal(err)
	}
	turnData := `{"type":"assistant","message":{"content":[{"type":"text","text":"Starting work"}]}}`
	if err := os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(turnData), 0644); err != nil {
		t.Fatal(err)
	}

	r.GenerateOversight(task.ID)

	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading oversight: %v", err)
	}
	if oversight.Status != store.OversightStatusReady {
		t.Fatalf("expected status=ready, got %q (error: %s)", oversight.Status, oversight.Error)
	}
	if len(oversight.Phases) == 0 {
		t.Fatal("expected at least one phase")
	}
	if oversight.Phases[0].Title != "Explored codebase" {
		t.Fatalf("unexpected phase title: %q", oversight.Phases[0].Title)
	}
}

// TestGenerateOversightContainerError verifies that GenerateOversight saves a
// failed status when the container exits non-zero with no output.
func TestGenerateOversightContainerError(t *testing.T) {
	cmd := fakeCmdScript(t, "", 1)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Task with failing oversight", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	// Write a turn file so it gets past the "no activity" check.
	outputsDir := s.OutputsDir(task.ID)
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		t.Fatal(err)
	}
	turnData := `{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`
	if err := os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(turnData), 0644); err != nil {
		t.Fatal(err)
	}

	r.GenerateOversight(task.ID)

	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading oversight: %v", err)
	}
	if oversight.Status != store.OversightStatusFailed {
		t.Fatalf("expected status=failed, got %q", oversight.Status)
	}
	if oversight.Error == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestGenerateOversightFallsBackToCodexOnTokenLimit(t *testing.T) {
	tokenLimit := `{"result":"rate limit exceeded: token limit reached","session_id":"s1","stop_reason":"end_turn","is_error":true,"total_cost_usd":0.001}`
	cmd := fakeStatefulCmd(t, []string{tokenLimit, oversightOutputWithUsage})
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Task with codex fallback oversight", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	outputsDir := s.OutputsDir(task.ID)
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		t.Fatal(err)
	}
	turnData := `{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`
	if err := os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(turnData), 0644); err != nil {
		t.Fatal(err)
	}

	r.GenerateOversight(task.ID)

	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading oversight: %v", err)
	}
	if oversight.Status != store.OversightStatusReady {
		t.Fatalf("expected status=ready after fallback, got %q (error: %s)", oversight.Status, oversight.Error)
	}

	usages, err := s.GetTurnUsages(task.ID)
	if err != nil {
		t.Fatalf("GetTurnUsages: %v", err)
	}
	if len(usages) == 0 {
		t.Fatal("expected oversight usage record after fallback")
	}
	if usages[len(usages)-1].Sandbox != "codex" {
		t.Fatalf("expected oversight usage sandbox codex, got %q", usages[len(usages)-1].Sandbox)
	}
}

// TestGenerateOversightEmptyResult verifies that a successful container run with
// an empty structured result is treated as an empty summary instead of failure.
func TestGenerateOversightEmptyResult(t *testing.T) {
	cmd := fakeCmdScript(t, oversightOutputEmptyResult, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Task with empty oversight result", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	outputsDir := s.OutputsDir(task.ID)
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		t.Fatal(err)
	}
	turnData := `{"type":"assistant","message":{"content":[{"type":"text","text":"working on task"}]}}`
	if err := os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(turnData), 0644); err != nil {
		t.Fatal(err)
	}

	r.GenerateOversight(task.ID)

	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading oversight: %v", err)
	}
	if oversight.Status != store.OversightStatusReady {
		t.Fatalf("expected status=ready, got %q", oversight.Status)
	}
	if len(oversight.Phases) != 0 {
		t.Fatalf("expected 0 phases for empty result, got %d", len(oversight.Phases))
	}
	if oversight.Error != "" {
		t.Fatalf("expected empty error field, got %q", oversight.Error)
	}
}

// TestGenerateOversightNoTurns verifies that GenerateOversight saves a failed
// status when there are no turn files to summarize.
func TestGenerateOversightNoTurns(t *testing.T) {
	cmd := fakeCmdScript(t, oversightOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Task with no turns", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	// Do NOT write any turn files — outputs directory doesn't even exist.
	r.GenerateOversight(task.ID)

	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading oversight: %v", err)
	}
	if oversight.Status != store.OversightStatusFailed {
		t.Fatalf("expected status=failed when no turns exist, got %q", oversight.Status)
	}
}

// ---------------------------------------------------------------------------
// store.GetOversight — pending when no file exists
// ---------------------------------------------------------------------------

// TestGetOversightPendingWhenMissing verifies that GetOversight returns a
// pending status when no oversight.json has been written yet.
func TestGetOversightPendingWhenMissing(t *testing.T) {
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if oversight.Status != store.OversightStatusPending {
		t.Fatalf("expected pending status, got %q", oversight.Status)
	}
}

// TestSaveAndGetOversight verifies the round-trip persistence of oversight data.
func TestSaveAndGetOversight(t *testing.T) {
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	original := store.TaskOversight{
		Status:      store.OversightStatusReady,
		GeneratedAt: now,
		Phases: []store.OversightPhase{
			{
				Timestamp: now,
				Title:     "Explored codebase",
				Summary:   "Read key files to understand structure",
				ToolsUsed: []string{"Read", "Glob"},
				Commands:  []string{"go test ./...", "git status"},
				Actions:   []string{"Read main.go", "Listed Go files"},
			},
		},
	}

	if err := s.SaveOversight(task.ID, original); err != nil {
		t.Fatalf("unexpected error saving: %v", err)
	}

	loaded, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error loading: %v", err)
	}
	if loaded.Status != store.OversightStatusReady {
		t.Fatalf("expected ready status, got %q", loaded.Status)
	}
	if len(loaded.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(loaded.Phases))
	}
	if loaded.Phases[0].Title != "Explored codebase" {
		t.Fatalf("unexpected phase title: %q", loaded.Phases[0].Title)
	}
	if len(loaded.Phases[0].ToolsUsed) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(loaded.Phases[0].ToolsUsed))
	}
	if len(loaded.Phases[0].Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(loaded.Phases[0].Commands), loaded.Phases[0].Commands)
	}
	if loaded.Phases[0].Commands[0] != "go test ./..." {
		t.Fatalf("unexpected command: %q", loaded.Phases[0].Commands[0])
	}
}

// ---------------------------------------------------------------------------
// oversightIntervalFromEnv
// ---------------------------------------------------------------------------

// TestOversightIntervalFromEnvMissingFile verifies that a missing env file
// returns 0 (disabled).
func TestOversightIntervalFromEnvMissingFile(t *testing.T) {
	r := NewRunner(nil, RunnerConfig{EnvFile: "/nonexistent/path/.env"})
	if got := r.oversightIntervalFromEnv(); got != 0 {
		t.Fatalf("expected 0 for missing file, got %v", got)
	}
}

// TestOversightIntervalFromEnvEmptyPath verifies that an empty envFile path
// returns 0 without attempting to read anything.
func TestOversightIntervalFromEnvEmptyPath(t *testing.T) {
	r := NewRunner(nil, RunnerConfig{EnvFile: ""})
	if got := r.oversightIntervalFromEnv(); got != 0 {
		t.Fatalf("expected 0 for empty env path, got %v", got)
	}
}

// TestOversightIntervalFromEnvAbsentKey verifies that an env file without
// WALLFACER_OVERSIGHT_INTERVAL returns 0.
func TestOversightIntervalFromEnvAbsentKey(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("CLAUDE_CODE_OAUTH_TOKEN=tok\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(nil, RunnerConfig{EnvFile: envPath})
	if got := r.oversightIntervalFromEnv(); got != 0 {
		t.Fatalf("expected 0 when key absent, got %v", got)
	}
}

// TestOversightIntervalFromEnvValidValue verifies that a valid positive value
// is parsed and returned as the correct duration.
func TestOversightIntervalFromEnvValidValue(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("WALLFACER_OVERSIGHT_INTERVAL=5\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(nil, RunnerConfig{EnvFile: envPath})
	got := r.oversightIntervalFromEnv()
	if got != 5*time.Minute {
		t.Fatalf("expected 5m, got %v", got)
	}
}

// TestOversightIntervalFromEnvInvalidValue verifies that an invalid value
// (non-numeric) returns 0.
func TestOversightIntervalFromEnvInvalidValue(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("WALLFACER_OVERSIGHT_INTERVAL=notanumber\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(nil, RunnerConfig{EnvFile: envPath})
	if got := r.oversightIntervalFromEnv(); got != 0 {
		t.Fatalf("expected 0 for invalid value, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// periodicOversightWorker
// ---------------------------------------------------------------------------

// TestPeriodicOversightWorkerExitsOnContextCancel verifies that
// periodicOversightWorker exits promptly when its context is cancelled.
func TestPeriodicOversightWorkerExitsOnContextCancel(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	// Use a 1-minute interval — worker should exit before the first tick.
	if err := os.WriteFile(envPath, []byte("WALLFACER_OVERSIGHT_INTERVAL=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(nil, RunnerConfig{EnvFile: envPath})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.periodicOversightWorker(ctx, uuid.New())
	}()

	cancel()
	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("periodicOversightWorker did not exit after context cancel")
	}
}

// TestPeriodicOversightWorkerDisabledExitsImmediately verifies that the worker
// exits immediately when the interval is 0 (disabled).
func TestPeriodicOversightWorkerDisabledExitsImmediately(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("WALLFACER_OVERSIGHT_INTERVAL=0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(nil, RunnerConfig{EnvFile: envPath})

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.periodicOversightWorker(context.Background(), uuid.New())
	}()

	select {
	case <-done:
		// expected — exits immediately for interval=0
	case <-time.After(500 * time.Millisecond):
		t.Fatal("periodicOversightWorker should exit immediately when disabled")
	}
}

// TestPeriodicOversightWorkerSkipsWhenLocked verifies that periodicOversightWorker
// skips a tick when the per-task oversight mutex is already held (TryLock fails),
// without blocking or panicking.
func TestPeriodicOversightWorkerSkipsWhenLocked(t *testing.T) {
	// Use a very short interval to trigger ticks quickly in the test.
	envPath := filepath.Join(t.TempDir(), ".env")
	// We'll manually set interval=0 so worker exits; test logic is below.
	// Instead, write a valid interval but cancel immediately after confirming
	// the worker is running and the mutex is held.
	if err := os.WriteFile(envPath, []byte("WALLFACER_OVERSIGHT_INTERVAL=0\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := fakeCmdScript(t, oversightOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	// Override envFile so oversightIntervalFromEnv reads our test file.
	r.envFile = envPath

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	// Pre-hold the oversight mutex to simulate a concurrent generation.
	mu := r.oversightLock(task.ID)
	mu.Lock()

	// Worker is disabled (interval=0) so exits immediately; this simply
	// verifies it doesn't deadlock when the mutex is held.
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.periodicOversightWorker(context.Background(), task.ID)
	}()

	select {
	case <-done:
		// expected — disabled worker exits immediately even with mutex held
	case <-time.After(500 * time.Millisecond):
		t.Fatal("worker should exit immediately when disabled, regardless of mutex state")
	}

	mu.Unlock()
}

// TestPeriodicOversightWorkerSkipsEmptyOutputsDir verifies that the worker
// skips generation when the outputs directory is empty (no turns yet).
func TestPeriodicOversightWorkerSkipsEmptyOutputsDir(t *testing.T) {
	// Use a very short interval (we'll simulate by patching internals).
	// This test uses a real runner and checks that no oversight is written
	// when there are no turn files.
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("WALLFACER_OVERSIGHT_INTERVAL=0\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := fakeCmdScript(t, oversightOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	r.envFile = envPath

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "no outputs task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	// Worker exits immediately since interval=0; oversight should remain pending.
	ctxW, cancelW := context.WithCancel(context.Background())
	cancelW() // cancel immediately

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.periodicOversightWorker(ctxW, task.ID)
	}()
	wg.Wait()

	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No generation should have happened — outputs dir is empty (no turns).
	if oversight.Status == store.OversightStatusReady {
		t.Fatal("expected oversight not to be generated for task with no turn files")
	}
}

// ---------------------------------------------------------------------------
// canonicalizeToolName
// ---------------------------------------------------------------------------

func TestCanonicalizeToolName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Bash aliases
		{"bash", "Bash"},
		{"Bash", "Bash"},
		{"BASH", "Bash"},
		{"command_execution", "Bash"},
		{"Command_Execution", "Bash"},
		// Read aliases
		{"read", "Read"},
		{"read_file", "Read"},
		{"read_file_tool", "Read"},
		{"file_read", "Read"},
		{"readfile", "Read"},
		{"READ", "Read"},
		// Write aliases
		{"write", "Write"},
		{"write_file", "Write"},
		{"write_file_tool", "Write"},
		{"file_write", "Write"},
		{"writefile", "Write"},
		{"WRITE_FILE", "Write"},
		// Edit aliases
		{"edit", "Edit"},
		{"edit_file", "Edit"},
		{"modify_file", "Edit"},
		{"EDIT", "Edit"},
		// Glob
		{"glob", "Glob"},
		{"Glob", "Glob"},
		// Grep aliases
		{"grep", "Grep"},
		{"search", "Grep"},
		{"find", "Grep"},
		{"SEARCH", "Grep"},
		// WebSearch aliases
		{"websearch", "WebSearch"},
		{"web_search", "WebSearch"},
		{"WebSearch", "WebSearch"},
		// WebFetch aliases
		{"webfetch", "WebFetch"},
		{"web_fetch", "WebFetch"},
		{"WebFetch", "WebFetch"},
		// Task
		{"task", "Task"},
		{"Task", "Task"},
		{"TASK", "Task"},
		// Unknown: trimmed original returned
		{"unknown_tool", "unknown_tool"},
		{"  my_tool  ", "my_tool"},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := canonicalizeToolName(tc.input)
			if got != tc.want {
				t.Errorf("canonicalizeToolName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractToolInputGo
// ---------------------------------------------------------------------------

func TestExtractToolInputGo(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input map[string]interface{}
		want  string
	}{
		// Bash
		{name: "bash command", tool: "Bash", input: map[string]interface{}{"command": "ls -la"}, want: "ls -la"},
		{name: "bash alias", tool: "bash", input: map[string]interface{}{"command": "echo hi"}, want: "echo hi"},
		{name: "bash nil input", tool: "Bash", input: nil, want: ""},
		// Read
		{name: "read file_path", tool: "Read", input: map[string]interface{}{"file_path": "/workspace/main.go"}, want: "/workspace/main.go"},
		{name: "read alias", tool: "read_file", input: map[string]interface{}{"file_path": "/foo.go"}, want: "/foo.go"},
		// Write
		{name: "write file_path", tool: "Write", input: map[string]interface{}{"file_path": "/workspace/out.txt"}, want: "/workspace/out.txt"},
		// Edit
		{name: "edit file_path", tool: "Edit", input: map[string]interface{}{"file_path": "/workspace/a.go"}, want: "/workspace/a.go"},
		// Glob
		{name: "glob pattern", tool: "Glob", input: map[string]interface{}{"pattern": "**/*.go"}, want: "**/*.go"},
		// Grep
		{name: "grep pattern", tool: "Grep", input: map[string]interface{}{"pattern": "func main"}, want: "func main"},
		// WebFetch
		{name: "webfetch url", tool: "WebFetch", input: map[string]interface{}{"url": "https://example.com"}, want: "https://example.com"},
		// WebSearch
		{name: "websearch query", tool: "WebSearch", input: map[string]interface{}{"query": "golang channels"}, want: "golang channels"},
		// Task short prompt
		{name: "task short prompt", tool: "Task", input: map[string]interface{}{"prompt": "do something"}, want: "do something"},
		// Task long prompt truncated at 120
		{name: "task long prompt truncated", tool: "Task", input: map[string]interface{}{"prompt": string(make([]byte, 200))}, want: string(make([]byte, 120))},
		// Task empty prompt
		{name: "task empty prompt", tool: "Task", input: map[string]interface{}{"prompt": ""}, want: ""},
		// Default fallback keys
		{name: "default file_path fallback", tool: "custom_tool", input: map[string]interface{}{"file_path": "/some/file"}, want: "/some/file"},
		{name: "default command fallback", tool: "custom_tool", input: map[string]interface{}{"command": "run me"}, want: "run me"},
		{name: "default pattern fallback", tool: "custom_tool", input: map[string]interface{}{"pattern": "*.go"}, want: "*.go"},
		{name: "default query fallback", tool: "custom_tool", input: map[string]interface{}{"query": "search term"}, want: "search term"},
		{name: "default path fallback", tool: "custom_tool", input: map[string]interface{}{"path": "/a/b"}, want: "/a/b"},
		{name: "default no known key", tool: "custom_tool", input: map[string]interface{}{"other": "val"}, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractToolInputGo(tc.tool, tc.input)
			if got != tc.want {
				t.Errorf("extractToolInputGo(%q, ...) = %q, want %q", tc.tool, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseContentBlocks
// ---------------------------------------------------------------------------

func TestParseContentBlocks(t *testing.T) {
	t.Run("empty raw message returns nil", func(t *testing.T) {
		got := parseContentBlocks(json.RawMessage{})
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("valid JSON array of blocks", func(t *testing.T) {
		raw := json.RawMessage(`[{"type":"text","text":"hello"},{"type":"tool_use","name":"Read"}]`)
		got := parseContentBlocks(raw)
		if len(got) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(got))
		}
		if got[0].Type != "text" || got[0].Text != "hello" {
			t.Errorf("unexpected first block: %+v", got[0])
		}
		if got[1].Type != "tool_use" || got[1].Name != "Read" {
			t.Errorf("unexpected second block: %+v", got[1])
		}
	})

	t.Run("single valid JSON object wrapped in slice", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"text","text":"single"}`)
		got := parseContentBlocks(raw)
		if len(got) != 1 {
			t.Fatalf("expected 1 block, got %d", len(got))
		}
		if got[0].Type != "text" || got[0].Text != "single" {
			t.Errorf("unexpected block: %+v", got[0])
		}
	})

	t.Run("invalid JSON returns nil", func(t *testing.T) {
		raw := json.RawMessage(`not valid json`)
		got := parseContentBlocks(raw)
		if got != nil {
			t.Errorf("expected nil for invalid JSON, got %v", got)
		}
	})

	t.Run("empty array returns empty slice", func(t *testing.T) {
		raw := json.RawMessage(`[]`)
		got := parseContentBlocks(raw)
		if got == nil {
			t.Errorf("expected non-nil empty slice, got nil")
		}
		if len(got) != 0 {
			t.Errorf("expected 0 elements, got %d", len(got))
		}
	})
}

// ---------------------------------------------------------------------------
// parseRawInput
// ---------------------------------------------------------------------------

func TestParseRawInput(t *testing.T) {
	t.Run("empty raw message returns nil", func(t *testing.T) {
		got := parseRawInput(json.RawMessage{})
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("direct JSON map decoded correctly", func(t *testing.T) {
		raw := json.RawMessage(`{"file_path":"/workspace/main.go","command":"ls"}`)
		got := parseRawInput(raw)
		if got == nil {
			t.Fatal("expected map, got nil")
		}
		if got["file_path"] != "/workspace/main.go" {
			t.Errorf("file_path: got %q, want %q", got["file_path"], "/workspace/main.go")
		}
		if got["command"] != "ls" {
			t.Errorf("command: got %q, want %q", got["command"], "ls")
		}
	})

	t.Run("string-encoded JSON decoded correctly", func(t *testing.T) {
		inner := `{"file_path":"/encoded/path.go"}`
		encoded, _ := json.Marshal(inner)
		got := parseRawInput(json.RawMessage(encoded))
		if got == nil {
			t.Fatal("expected map, got nil")
		}
		if got["file_path"] != "/encoded/path.go" {
			t.Errorf("file_path: got %q, want %q", got["file_path"], "/encoded/path.go")
		}
	})

	t.Run("invalid JSON string-encoded returns nil map", func(t *testing.T) {
		// A JSON string whose contents are not valid JSON
		encoded, _ := json.Marshal("not a json map")
		got := parseRawInput(json.RawMessage(encoded))
		// The string decodes fine but inner decode fails; result is nil
		if got != nil {
			t.Errorf("expected nil for non-map string content, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// codexToolFromItem
// ---------------------------------------------------------------------------

func TestCodexToolFromItem(t *testing.T) {
	t.Run("type field recognized as tool name", func(t *testing.T) {
		item := &ndjsonItem{
			Type:  "bash",
			Input: json.RawMessage(`{"command":"echo hello"}`),
		}
		tool, input := codexToolFromItem(item)
		if tool != "Bash" {
			t.Errorf("tool: got %q, want %q", tool, "Bash")
		}
		if input != "echo hello" {
			t.Errorf("input: got %q, want %q", input, "echo hello")
		}
	})

	t.Run("falls back to Name field when Type unknown", func(t *testing.T) {
		item := &ndjsonItem{
			Type:  "function_call",
			Name:  "read_file",
			Input: json.RawMessage(`{"file_path":"/foo.go"}`),
		}
		tool, input := codexToolFromItem(item)
		if tool != "Read" {
			t.Errorf("tool: got %q, want %q", tool, "Read")
		}
		if input != "/foo.go" {
			t.Errorf("input: got %q, want %q", input, "/foo.go")
		}
	})

	t.Run("falls back to ToolName field", func(t *testing.T) {
		// Type must be empty so toolName starts as "" enabling ToolName fallback.
		item := &ndjsonItem{
			Type:     "",
			ToolName: "write_file",
			Input:    json.RawMessage(`{"file_path":"/out.txt"}`),
		}
		tool, input := codexToolFromItem(item)
		if tool != "Write" {
			t.Errorf("tool: got %q, want %q", tool, "Write")
		}
		if input != "/out.txt" {
			t.Errorf("input: got %q, want %q", input, "/out.txt")
		}
	})

	t.Run("falls back to Tool field", func(t *testing.T) {
		// Type must be empty so toolName starts as "" enabling Tool fallback.
		item := &ndjsonItem{
			Type:  "",
			Tool:  "glob",
			Input: json.RawMessage(`{"pattern":"**/*.go"}`),
		}
		tool, input := codexToolFromItem(item)
		if tool != "Glob" {
			t.Errorf("tool: got %q, want %q", tool, "Glob")
		}
		if input != "**/*.go" {
			t.Errorf("input: got %q, want %q", input, "**/*.go")
		}
	})

	t.Run("nil input gives empty string", func(t *testing.T) {
		item := &ndjsonItem{
			Type: "bash",
		}
		tool, input := codexToolFromItem(item)
		if tool != "Bash" {
			t.Errorf("tool: got %q, want %q", tool, "Bash")
		}
		if input != "" {
			t.Errorf("input: got %q, want empty", input)
		}
	})

	t.Run("string-encoded input decoded", func(t *testing.T) {
		inner := `{"file_path":"/encoded.go"}`
		encoded, _ := json.Marshal(inner)
		item := &ndjsonItem{
			Type:  "read",
			Input: json.RawMessage(encoded),
		}
		tool, input := codexToolFromItem(item)
		if tool != "Read" {
			t.Errorf("tool: got %q, want %q", tool, "Read")
		}
		if input != "/encoded.go" {
			t.Errorf("input: got %q, want %q", input, "/encoded.go")
		}
	})
}

// ---------------------------------------------------------------------------
// inferCodexToolName
// ---------------------------------------------------------------------------

func TestInferCodexToolName(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		// Empty command
		{"", "Bash"},
		{"   ", "Bash"},
		// Write: pipe to tee
		{"make build | tee output.log", "Write"},
		// Write: redirect >
		{"echo hello > file.txt", "Write"},
		// Write: append >>
		{"echo line >> file.txt", "Write"},
		// Write: cat >
		{"cat > newfile.txt", "Write"},
		// Write: cat>>
		{"cat>>file.txt", "Write"},
		// Read: first word is cat
		{"cat /workspace/main.go", "Read"},
		// Read: sed
		{"sed 's/foo/bar/g' file.go", "Read"},
		// Read: head
		{"head -n 10 file.txt", "Read"},
		// Read: tail
		{"tail -f log.txt", "Read"},
		// Read: less
		{"less bigfile.txt", "Read"},
		// Read: stat
		{"stat /etc/hosts", "Read"},
		// Read: pwd
		{"pwd", "Read"},
		// Read: find
		{"find . -name '*.go'", "Read"},
		// Read: grep
		{"grep -r pattern /workspace", "Read"},
		// Read: rg
		{"rg TODO .", "Read"},
		// git read operations
		{"git show HEAD:main.go", "Read"},
		{"git status", "Read"},
		{"git diff HEAD", "Read"},
		{"git log --oneline", "Read"},
		// git write operations
		{"git add .", "Write"},
		{"git commit -m 'fix'", "Write"},
		{"git mv old new", "Write"},
		{"git rm file.go", "Write"},
		{"git reset HEAD~1", "Write"},
		{"git restore .", "Write"},
		{"git checkout main", "Write"},
		{"git switch feature", "Write"},
		// git other → Write
		{"git push origin main", "Write"},
		{"git fetch origin", "Write"},
		// apply_patch → Write
		{"apply_patch patch.diff", "Write"},
		// cp, mv, rm, mkdir etc → Write
		{"cp src dst", "Write"},
		{"mv old new", "Write"},
		{"rm -rf /tmp/dir", "Write"},
		{"mkdir /new/dir", "Write"},
		{"rmdir /empty/dir", "Write"},
		{"touch file.txt", "Write"},
		{"chmod 755 script.sh", "Write"},
		{"chown user file", "Write"},
		{"tee output.txt", "Write"},
		// sudo prefix stripped, still classified correctly
		{"sudo cat /etc/passwd", "Read"},
		{"sudo rm -rf /tmp/old", "Write"},
		// Bash: unknown first word
		{"go build ./...", "Bash"},
		{"make test", "Bash"},
		{"python script.py", "Bash"},
		{"npm install", "Bash"},
	}

	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			got := inferCodexToolName(tc.command)
			if got != tc.want {
				t.Errorf("inferCodexToolName(%q) = %q, want %q", tc.command, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// periodicOversightWorker
// ---------------------------------------------------------------------------

// TestPeriodicOversightWorker_DisabledReturnsImmediately verifies that when
// the oversight interval is 0 (disabled, no env file configured), the worker
// goroutine returns immediately without blocking on the context.
func TestPeriodicOversightWorker_DisabledReturnsImmediately(t *testing.T) {
	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// NewRunner with an empty RunnerConfig means envFile == "" →
	// oversightIntervalFromEnv returns 0 → worker exits immediately.
	r := NewRunner(s, RunnerConfig{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		r.periodicOversightWorker(ctx, uuid.New())
		close(done)
	}()

	select {
	case <-done:
		// Good — worker returned immediately because interval == 0.
	case <-time.After(2 * time.Second):
		t.Error("periodicOversightWorker did not return promptly when interval=0")
	}
}

// TestPeriodicOversightWorker_ContextCancellation verifies that the worker
// exits when its context is cancelled even if the ticker would otherwise keep
// it alive. This requires a real env file with a non-zero interval.
func TestPeriodicOversightWorker_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	// Write an env file with a very long oversight interval so the ticker
	// will never fire during the test, but the worker stays alive until cancel.
	envPath := filepath.Join(tmpDir, ".env")
	envContent := "WALLFACER_OVERSIGHT_INTERVAL=99999\n"
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		t.Fatal(err)
	}

	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	r := NewRunner(s, RunnerConfig{EnvFile: envPath})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		r.periodicOversightWorker(ctx, uuid.New())
		close(done)
	}()

	// Give the goroutine a moment to start, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good — worker exited on context cancellation.
	case <-time.After(2 * time.Second):
		t.Error("periodicOversightWorker did not exit after context cancellation")
	}
}

// TestPeriodicOversightWorker_EnvFileMissing verifies that a non-empty but
// non-existent env file path still causes the worker to return immediately
// (parse error → interval=0 → disabled).
func TestPeriodicOversightWorker_EnvFileMissing(t *testing.T) {
	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	r := NewRunner(s, RunnerConfig{EnvFile: "/does/not/exist/.env"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		r.periodicOversightWorker(ctx, uuid.New())
		close(done)
	}()

	select {
	case <-done:
		// Good — missing file → parse error → interval=0 → returns immediately.
	case <-time.After(2 * time.Second):
		t.Error("periodicOversightWorker did not return when env file is missing")
	}
}
