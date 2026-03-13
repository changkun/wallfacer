package store

import (
	"strings"
	"testing"
	"time"
)

func TestSaveAndGetOversight(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTask(ctx, "test prompt", 30, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	oversight := TaskOversight{
		Status:      OversightStatusReady,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		Phases: []OversightPhase{
			{
				Title:     "Phase 1: Analysis",
				Summary:   "Analyzed the codebase",
				ToolsUsed: []string{"read_file", "search"},
			},
			{
				Title:   "Phase 2: Implementation",
				Summary: "Implemented the feature",
				Actions: []string{"created widget.go"},
			},
		},
	}

	if err := s.SaveOversight(task.ID, oversight); err != nil {
		t.Fatalf("SaveOversight: %v", err)
	}

	got, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("GetOversight: %v", err)
	}
	if got == nil {
		t.Fatal("GetOversight returned nil")
	}
	if got.Status != OversightStatusReady {
		t.Errorf("Status = %q, want %q", got.Status, OversightStatusReady)
	}
	if len(got.Phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(got.Phases))
	}
	if got.Phases[0].Title != "Phase 1: Analysis" {
		t.Errorf("Phase[0].Title = %q, want %q", got.Phases[0].Title, "Phase 1: Analysis")
	}
	if got.Phases[1].Summary != "Implemented the feature" {
		t.Errorf("Phase[1].Summary = %q, want %q", got.Phases[1].Summary, "Implemented the feature")
	}
}

func TestGetOversight_NotExist_ReturnsPending(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTask(ctx, "test prompt", 30, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("GetOversight on missing file: %v", err)
	}
	if got == nil {
		t.Fatal("GetOversight should return non-nil pending when file missing")
	}
	if got.Status != OversightStatusPending {
		t.Errorf("Status = %q, want %q", got.Status, OversightStatusPending)
	}
}

func TestSaveAndGetTestOversight(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTask(ctx, "test prompt", 30, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	oversight := TaskOversight{
		Status:      OversightStatusReady,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		Phases: []OversightPhase{
			{Title: "Test Phase", Summary: "Ran test suite"},
		},
	}

	if err := s.SaveTestOversight(task.ID, oversight); err != nil {
		t.Fatalf("SaveTestOversight: %v", err)
	}

	got, err := s.GetTestOversight(task.ID)
	if err != nil {
		t.Fatalf("GetTestOversight: %v", err)
	}
	if got == nil {
		t.Fatal("GetTestOversight returned nil")
	}
	if got.Status != OversightStatusReady {
		t.Errorf("Status = %q, want %q", got.Status, OversightStatusReady)
	}
	if len(got.Phases) != 1 || got.Phases[0].Title != "Test Phase" {
		t.Errorf("unexpected phases: %+v", got.Phases)
	}
}

func TestGetTestOversight_NotExist_ReturnsPending(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTask(ctx, "test prompt", 30, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetTestOversight(task.ID)
	if err != nil {
		t.Fatalf("GetTestOversight on missing file: %v", err)
	}
	if got == nil {
		t.Fatal("GetTestOversight should return non-nil pending when file missing")
	}
	if got.Status != OversightStatusPending {
		t.Errorf("Status = %q, want %q", got.Status, OversightStatusPending)
	}
}

func TestLoadOversightText_NotExist_ReturnsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTask(ctx, "test prompt", 30, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	text, err := s.LoadOversightText(task.ID)
	if err != nil {
		t.Fatalf("LoadOversightText on missing file: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty string when no oversight file, got %q", text)
	}
}

func TestLoadOversightText_WithPhases(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTask(ctx, "test prompt", 30, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	oversight := TaskOversight{
		Status: OversightStatusReady,
		Phases: []OversightPhase{
			{Title: "Alpha", Summary: "First phase"},
			{Title: "Beta", Summary: "Second phase"},
		},
	}
	if err := s.SaveOversight(task.ID, oversight); err != nil {
		t.Fatalf("SaveOversight: %v", err)
	}

	text, err := s.LoadOversightText(task.ID)
	if err != nil {
		t.Fatalf("LoadOversightText: %v", err)
	}
	if text == "" {
		t.Fatal("LoadOversightText returned empty string after saving phases")
	}
	for _, want := range []string{"Alpha", "First phase", "Beta", "Second phase"} {
		if !strings.Contains(text, want) {
			t.Errorf("LoadOversightText result does not contain %q: got %q", want, text)
		}
	}
}

func TestOversightText_EmptyPhases(t *testing.T) {
	o := TaskOversight{Status: OversightStatusPending}
	text := oversightText(o)
	if text != "" {
		t.Errorf("oversightText with no phases = %q, want empty", text)
	}
}

func TestOversightText_PhasesWithEmptyTitleOrSummary(t *testing.T) {
	o := TaskOversight{
		Phases: []OversightPhase{
			{Title: "OnlyTitle"},
			{Summary: "OnlySummary"},
			{Title: "Both", Summary: "Content"},
		},
	}
	text := oversightText(o)
	for _, want := range []string{"OnlyTitle", "OnlySummary", "Both", "Content"} {
		if !strings.Contains(text, want) {
			t.Errorf("oversightText missing %q: got %q", want, text)
		}
	}
}

func TestSaveOversight_UpdatesSearchIndex(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTask(ctx, "searchable test prompt", 30, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	oversight := TaskOversight{
		Status: OversightStatusReady,
		Phases: []OversightPhase{
			{Title: "UniqueOversightKeyword", Summary: "Some summary"},
		},
	}
	if err := s.SaveOversight(task.ID, oversight); err != nil {
		t.Fatalf("SaveOversight: %v", err)
	}

	text, err := s.LoadOversightText(task.ID)
	if err != nil {
		t.Fatalf("LoadOversightText: %v", err)
	}
	if !strings.Contains(text, "UniqueOversightKeyword") {
		t.Errorf("oversight text does not contain keyword: %q", text)
	}
}
