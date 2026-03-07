package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
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

// TestBuildTurnTimestampsCountsOutputEvents verifies that each output event
// maps to consecutive turn numbers.
func TestBuildTurnTimestampsCountsOutputEvents(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC)
	events := []store.TaskEvent{
		{EventType: store.EventTypeStateChange, CreatedAt: t1.Add(-1 * time.Second)},
		{EventType: store.EventTypeOutput, CreatedAt: t1},
		{EventType: store.EventTypeSystem, CreatedAt: t1.Add(30 * time.Second)},
		{EventType: store.EventTypeOutput, CreatedAt: t2},
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

// ---------------------------------------------------------------------------
// GenerateOversight — integration (uses fake container)
// ---------------------------------------------------------------------------

const oversightOutput = `{"result":"{\"phases\":[{\"timestamp\":\"2024-01-15T10:00:00Z\",\"title\":\"Explored codebase\",\"summary\":\"Read key files\",\"tools_used\":[\"Read\"],\"actions\":[\"Read main.go\"]}]}","session_id":"s1","stop_reason":"end_turn","is_error":false}`

// TestGenerateOversightSuccess verifies that GenerateOversight saves a ready
// oversight when the container succeeds and produces valid structured JSON.
func TestGenerateOversightSuccess(t *testing.T) {
	cmd := fakeCmdScript(t, oversightOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Add feature X", 5, false, "")
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

// TestGenerateOversightContainerError verifies that GenerateOversight saves a
// failed status when the container exits non-zero with no output.
func TestGenerateOversightContainerError(t *testing.T) {
	cmd := fakeCmdScript(t, "", 1)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Task with failing oversight", 5, false, "")
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

// TestGenerateOversightNoTurns verifies that GenerateOversight saves a failed
// status when there are no turn files to summarize.
func TestGenerateOversightNoTurns(t *testing.T) {
	cmd := fakeCmdScript(t, oversightOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Task with no turns", 5, false, "")
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
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	task, err := s.CreateTask(context.Background(), "test", 5, false, "")
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
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	task, err := s.CreateTask(context.Background(), "test", 5, false, "")
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
}
