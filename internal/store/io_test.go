// Tests for io.go: SaveTurnOutput and atomic persistence helpers.
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveTurnOutput_StdoutOnly(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "p", 5)

	stdout := []byte(`{"hello":"world"}`)
	if err := s.SaveTurnOutput(task.ID, 1, stdout, nil); err != nil {
		t.Fatalf("SaveTurnOutput: %v", err)
	}

	outPath := filepath.Join(s.OutputsDir(task.ID), "turn-0001.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read stdout file: %v", err)
	}
	if string(data) != `{"hello":"world"}` {
		t.Errorf("stdout data = %q", data)
	}

	// No stderr file when stderr is nil/empty.
	stderrPath := filepath.Join(s.OutputsDir(task.ID), "turn-0001.stderr.txt")
	if _, err := os.Stat(stderrPath); !os.IsNotExist(err) {
		t.Error("stderr file should not exist when stderr is empty")
	}
}

func TestSaveTurnOutput_WithStderr(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "p", 5)

	if err := s.SaveTurnOutput(task.ID, 2, []byte("stdout"), []byte("error output")); err != nil {
		t.Fatalf("SaveTurnOutput: %v", err)
	}

	stderrPath := filepath.Join(s.OutputsDir(task.ID), "turn-0002.stderr.txt")
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
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "p", 5)

	if err := s.SaveTurnOutput(task.ID, 42, []byte("data"), nil); err != nil {
		t.Fatalf("SaveTurnOutput: %v", err)
	}

	outPath := filepath.Join(s.OutputsDir(task.ID), "turn-0042.json")
	if _, err := os.ReadFile(outPath); err != nil {
		t.Errorf("expected file turn-0042.json: %v", err)
	}
}
