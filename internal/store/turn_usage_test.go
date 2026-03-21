package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAppendAndGetTurnUsages(t *testing.T) {
	s := newTestStore(t)

	// Create a task so the task directory exists.
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "test prompt", Timeout: 0, Kind: TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	recs := []TurnUsageRecord{
		{Turn: 1, Timestamp: now, InputTokens: 100, OutputTokens: 50, CostUSD: 0.001, StopReason: "end_turn", SubAgent: "implementation"},
		{Turn: 2, Timestamp: now.Add(time.Minute), InputTokens: 200, OutputTokens: 80, CostUSD: 0.002, StopReason: "max_tokens", SubAgent: "implementation"},
		{Turn: 3, Timestamp: now.Add(2 * time.Minute), InputTokens: 150, OutputTokens: 60, CacheReadInputTokens: 30, CostUSD: 0.0015, SubAgent: "implementation"},
		{Turn: 4, Timestamp: now.Add(3 * time.Minute), InputTokens: 120, OutputTokens: 40, CostUSD: 0.0012, SubAgent: "test"},
		{Turn: 5, Timestamp: now.Add(4 * time.Minute), InputTokens: 90, OutputTokens: 30, CostUSD: 0.0009, StopReason: "end_turn", SubAgent: "implementation"},
	}

	for _, rec := range recs {
		if err := s.AppendTurnUsage(task.ID, rec); err != nil {
			t.Fatalf("AppendTurnUsage(turn=%d): %v", rec.Turn, err)
		}
	}

	got, err := s.GetTurnUsages(task.ID)
	if err != nil {
		t.Fatalf("GetTurnUsages: %v", err)
	}
	if len(got) != len(recs) {
		t.Fatalf("expected %d records, got %d", len(recs), len(got))
	}
	for i, rec := range recs {
		if got[i].Turn != rec.Turn {
			t.Errorf("record %d: Turn=%d want %d", i, got[i].Turn, rec.Turn)
		}
		if got[i].InputTokens != rec.InputTokens {
			t.Errorf("record %d: InputTokens=%d want %d", i, got[i].InputTokens, rec.InputTokens)
		}
		if got[i].OutputTokens != rec.OutputTokens {
			t.Errorf("record %d: OutputTokens=%d want %d", i, got[i].OutputTokens, rec.OutputTokens)
		}
		if got[i].SubAgent != rec.SubAgent {
			t.Errorf("record %d: SubAgent=%q want %q", i, got[i].SubAgent, rec.SubAgent)
		}
		if got[i].CostUSD != rec.CostUSD {
			t.Errorf("record %d: CostUSD=%v want %v", i, got[i].CostUSD, rec.CostUSD)
		}
	}
}

func TestGetTurnUsages_NoFile(t *testing.T) {
	s := newTestStore(t)

	// Create a task so the task directory exists, but never call AppendTurnUsage.
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "test prompt", Timeout: 0, Kind: TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetTurnUsages(task.ID)
	if err != nil {
		t.Fatalf("GetTurnUsages on missing file: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 records, got %d", len(got))
	}
}

func TestGetTurnUsages_NonExistentTask(t *testing.T) {
	s := newTestStore(t)
	randomID := uuid.New()

	got, err := s.GetTurnUsages(randomID)
	if err != nil {
		t.Fatalf("GetTurnUsages on non-existent task: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 records, got %d", len(got))
	}
}

func TestTurnUsageFileIsValidJSONL(t *testing.T) {
	s := newTestStore(t)

	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "jsonl test", Timeout: 0, Kind: TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	for i := 1; i <= 5; i++ {
		if err := s.AppendTurnUsage(task.ID, TurnUsageRecord{
			Turn:      i,
			Timestamp: time.Now().UTC(),
			CostUSD:   float64(i) * 0.001,
			SubAgent:  "implementation",
		}); err != nil {
			t.Fatalf("AppendTurnUsage(turn=%d): %v", i, err)
		}
	}

	// Read raw file and verify each line is independently parseable JSON.
	path := s.turnUsagePath(task.ID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		var rec TurnUsageRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Errorf("line %d is not valid JSON: %v (line: %s)", lineNum, err, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if lineNum != 5 {
		t.Errorf("expected 5 lines, got %d", lineNum)
	}
}

// TestAppendTurnUsage_NonExistentDir verifies that AppendTurnUsage returns an error
// when the task directory does not exist (covering the os.OpenFile error path).
func TestAppendTurnUsage_NonExistentDir(t *testing.T) {
	s := newTestStore(t)
	// Use a random UUID that has no corresponding directory in the store.
	randomID := uuid.New()
	err := s.AppendTurnUsage(randomID, TurnUsageRecord{Turn: 1, CostUSD: 0.001})
	if err == nil {
		t.Error("expected an error when task directory does not exist, got nil")
	}
}

// TestGetTurnUsages_CorruptedLineSkipped verifies that a corrupted JSONL line is
// silently skipped and valid records are still returned.
func TestGetTurnUsages_CorruptedLineSkipped(t *testing.T) {
	s := newTestStore(t)

	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "test prompt", Timeout: 0, Kind: TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Write one valid record first.
	if err := s.AppendTurnUsage(task.ID, TurnUsageRecord{Turn: 1, CostUSD: 0.001}); err != nil {
		t.Fatalf("AppendTurnUsage: %v", err)
	}

	// Manually append corrupted JSONL data.
	path := s.turnUsagePath(task.ID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	_, _ = f.WriteString("not-valid-json\n")
	_ = f.Close()

	// Write another valid record.
	if err := s.AppendTurnUsage(task.ID, TurnUsageRecord{Turn: 2, CostUSD: 0.002}); err != nil {
		t.Fatalf("AppendTurnUsage: %v", err)
	}

	got, err := s.GetTurnUsages(task.ID)
	if err != nil {
		t.Fatalf("GetTurnUsages: %v", err)
	}
	// Only the two valid records should be returned.
	if len(got) != 2 {
		t.Errorf("expected 2 records (corrupted line skipped), got %d", len(got))
	}
}
