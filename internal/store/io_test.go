// Tests for io.go: SaveTurnOutput and atomic persistence helpers.
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveTurnOutput_StdoutOnly(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(dir)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	stdout := []byte(`{"hello":"world"}`)
	if err := s.SaveTurnOutput(task.ID, 1, stdout, nil); err != nil {
		t.Fatalf("SaveTurnOutput: %v", err)
	}

	outPath := filepath.Join(filepath.Join(s.DataDir(), task.ID.String(), "outputs"), "turn-0001.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read stdout file: %v", err)
	}
	if string(data) != `{"hello":"world"}` {
		t.Errorf("stdout data = %q", data)
	}

	// No stderr file when stderr is nil/empty.
	stderrPath := filepath.Join(filepath.Join(s.DataDir(), task.ID.String(), "outputs"), "turn-0001.stderr.txt")
	if _, err := os.Stat(stderrPath); !os.IsNotExist(err) {
		t.Error("stderr file should not exist when stderr is empty")
	}
}

func TestSaveTurnOutput_WithStderr(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(dir)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	if err := s.SaveTurnOutput(task.ID, 2, []byte("stdout"), []byte("error output")); err != nil {
		t.Fatalf("SaveTurnOutput: %v", err)
	}

	stderrPath := filepath.Join(filepath.Join(s.DataDir(), task.ID.String(), "outputs"), "turn-0002.stderr.txt")
	data, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("read stderr file: %v", err)
	}
	if string(data) != "error output" {
		t.Errorf("stderr data = %q, want 'error output'", data)
	}
}

func TestSaveTurnOutput_TurnNumberFormatted(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(dir)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	if err := s.SaveTurnOutput(task.ID, 42, []byte("data"), nil); err != nil {
		t.Fatalf("SaveTurnOutput: %v", err)
	}

	outPath := filepath.Join(filepath.Join(s.DataDir(), task.ID.String(), "outputs"), "turn-0042.json")
	if _, err := os.ReadFile(outPath); err != nil {
		t.Errorf("expected file turn-0042.json: %v", err)
	}
}

// buildNDJSON builds n lines of valid NDJSON each exactly lineSize bytes long
// (including the terminating newline). Each line is a JSON object: {"i":<n>,"p":"<padding>"}.
func buildNDJSON(n, lineSize int) []byte {
	var buf bytes.Buffer
	for i := range n {
		// Build a line of exactly lineSize bytes.
		// The minimum object without padding is: {"i":0,"p":""}\n = 14 bytes for i<10.
		prefix := `{"i":` + string(rune('0'+i%10)) + `,"p":"`
		suffix := "\"}\n"
		padLen := lineSize - len(prefix) - len(suffix)
		if padLen < 0 {
			padLen = 0
		}
		buf.WriteString(prefix)
		buf.WriteString(strings.Repeat("x", padLen))
		buf.WriteString(suffix)
	}
	return buf.Bytes()
}

func TestSaveTurnOutput_Truncation(t *testing.T) {
	const limit = 1024
	const turn = 7

	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// Override the default limit with a small test value.
	s.maxTurnOutputBytes = limit

	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "trunctest", Timeout: 5})

	// Build ~4 KB of valid NDJSON (8 lines of 512 bytes each).
	const lineSize = 512
	const numLines = 8
	stdout := buildNDJSON(numLines, lineSize)
	if len(stdout) != numLines*lineSize {
		t.Fatalf("expected %d bytes of NDJSON, got %d", numLines*lineSize, len(stdout))
	}

	if err := s.SaveTurnOutput(task.ID, turn, stdout, nil); err != nil {
		t.Fatalf("SaveTurnOutput: %v", err)
	}

	// --- File size assertions ---
	outPath := filepath.Join(filepath.Join(s.DataDir(), task.ID.String(), "outputs"), "turn-0007.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}

	// sentinel is about 80 bytes; allow generous overhead.
	const sentinelOverhead = 200
	if len(data) > limit+sentinelOverhead {
		t.Errorf("truncated file too large: got %d bytes, want ≤ %d", len(data), limit+sentinelOverhead)
	}

	// --- NDJSON validity: every non-empty line must parse as JSON ---
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var lastLine string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !json.Valid([]byte(line)) {
			t.Errorf("invalid NDJSON line: %q", line)
		}
		lastLine = line
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	// --- Last line must be the truncation_notice sentinel ---
	var sentinel struct {
		Type        string `json:"type"`
		Subtype     string `json:"subtype"`
		TotalBytes  int    `json:"total_bytes"`
		TruncatedAt int    `json:"truncated_at"`
	}
	if err := json.Unmarshal([]byte(lastLine), &sentinel); err != nil {
		t.Fatalf("last line is not valid JSON: %v — line: %q", err, lastLine)
	}
	if sentinel.Type != "system" {
		t.Errorf("sentinel type = %q, want %q", sentinel.Type, "system")
	}
	if sentinel.Subtype != "truncation_notice" {
		t.Errorf("sentinel subtype = %q, want %q", sentinel.Subtype, "truncation_notice")
	}
	if sentinel.TotalBytes != len(stdout) {
		t.Errorf("sentinel total_bytes = %d, want %d", sentinel.TotalBytes, len(stdout))
	}

	// --- TruncatedTurns must record the turn ---
	updated, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	found := false
	for _, tt := range updated.TruncatedTurns {
		if tt == turn {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("TruncatedTurns = %v, want to contain turn %d", updated.TruncatedTurns, turn)
	}
}

func TestSaveTurnOutput_NoTruncationUnderLimit(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(dir)
	s.maxTurnOutputBytes = 4096

	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	stdout := []byte(`{"type":"result","value":"small"}` + "\n")

	if err := s.SaveTurnOutput(task.ID, 1, stdout, nil); err != nil {
		t.Fatalf("SaveTurnOutput: %v", err)
	}

	outPath := filepath.Join(filepath.Join(s.DataDir(), task.ID.String(), "outputs"), "turn-0001.json")
	data, _ := os.ReadFile(outPath)
	if string(data) != string(stdout) {
		t.Errorf("data was unexpectedly modified: got %q, want %q", data, stdout)
	}

	task2, _ := s.GetTask(bg(), task.ID)
	if len(task2.TruncatedTurns) != 0 {
		t.Errorf("TruncatedTurns should be empty, got %v", task2.TruncatedTurns)
	}
}

func TestSaveTurnOutput_UnlimitedWhenZero(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(dir)
	s.maxTurnOutputBytes = 0 // 0 = unlimited

	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	// Build 2 KB of data.
	big := bytes.Repeat([]byte(`{"x":1}`+"\n"), 300)

	if err := s.SaveTurnOutput(task.ID, 1, big, nil); err != nil {
		t.Fatalf("SaveTurnOutput: %v", err)
	}

	outPath := filepath.Join(filepath.Join(s.DataDir(), task.ID.String(), "outputs"), "turn-0001.json")
	data, _ := os.ReadFile(outPath)
	if len(data) != len(big) {
		t.Errorf("expected %d bytes unchanged, got %d", len(big), len(data))
	}
}
