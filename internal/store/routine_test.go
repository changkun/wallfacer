package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mkRoutine creates a routine card in the given store with sane defaults
// for tests. It returns the store-resident task so callers can further
// mutate via store writers.
func mkRoutine(t *testing.T, s *Store) *Task {
	t.Helper()
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{
		Prompt:                 "daily triage",
		Timeout:                30,
		Kind:                   TaskKindRoutine,
		RoutineIntervalSeconds: 3600,
		RoutineEnabled:         true,
		RoutineSpawnKind:       TaskKindTask,
	})
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}
	return task
}

func TestCreateTaskWithOptions_RoutineKind_PersistsFields(t *testing.T) {
	s := newTestStore(t)
	task := mkRoutine(t, s)

	if task.Kind != TaskKindRoutine {
		t.Fatalf("kind = %q, want routine", task.Kind)
	}
	if task.RoutineIntervalSeconds != 3600 {
		t.Fatalf("interval = %d, want 3600", task.RoutineIntervalSeconds)
	}
	if !task.RoutineEnabled {
		t.Fatalf("enabled = false, want true")
	}

	// Round-trip through the store to confirm on-disk persistence.
	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RoutineIntervalSeconds != 3600 || !got.RoutineEnabled {
		t.Fatalf("round-trip lost routine fields: %+v", got)
	}
}

func TestCreateTaskWithOptions_NonRoutineIgnoresRoutineFields(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{
		Prompt:                 "normal task",
		Timeout:                10,
		RoutineIntervalSeconds: 999,
		RoutineEnabled:         true,
		RoutineSpawnKind:       TaskKindIdeaAgent,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.RoutineIntervalSeconds != 0 || task.RoutineEnabled || task.RoutineSpawnKind != "" {
		t.Fatalf("non-routine task leaked routine fields: %+v", task)
	}
}

func TestUpdateRoutineSchedule_PersistsAndClamps(t *testing.T) {
	s := newTestStore(t)
	task := mkRoutine(t, s)

	if err := s.UpdateRoutineSchedule(bg(), task.ID, 900); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.RoutineIntervalSeconds != 900 {
		t.Fatalf("interval = %d, want 900", got.RoutineIntervalSeconds)
	}

	// Negative clamps to 0 (pause).
	if err := s.UpdateRoutineSchedule(bg(), task.ID, -1); err != nil {
		t.Fatalf("update negative: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if got.RoutineIntervalSeconds != 0 {
		t.Fatalf("interval = %d after negative, want 0", got.RoutineIntervalSeconds)
	}
}

func TestUpdateRoutineEnabled_TogglesAndClearsNextRun(t *testing.T) {
	s := newTestStore(t)
	task := mkRoutine(t, s)

	future := time.Now().Add(time.Hour)
	_ = s.UpdateRoutineNextRun(bg(), task.ID, &future)

	if err := s.UpdateRoutineEnabled(bg(), task.ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.RoutineEnabled {
		t.Fatalf("expected disabled")
	}
	if got.RoutineNextRun != nil {
		t.Fatalf("expected RoutineNextRun cleared on disable, got %v", got.RoutineNextRun)
	}

	if err := s.UpdateRoutineEnabled(bg(), task.ID, true); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if !got.RoutineEnabled {
		t.Fatalf("expected enabled")
	}
}

func TestUpdateRoutineNextRun_SetAndClear(t *testing.T) {
	s := newTestStore(t)
	task := mkRoutine(t, s)

	future := time.Now().Add(5 * time.Minute)
	if err := s.UpdateRoutineNextRun(bg(), task.ID, &future); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.RoutineNextRun == nil || !got.RoutineNextRun.Equal(future) {
		t.Fatalf("NextRun = %v, want %v", got.RoutineNextRun, future)
	}

	if err := s.UpdateRoutineNextRun(bg(), task.ID, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if got.RoutineNextRun != nil {
		t.Fatalf("NextRun not cleared: %v", got.RoutineNextRun)
	}
}

func TestUpdateRoutineLastFiredAt_SetAndClear(t *testing.T) {
	s := newTestStore(t)
	task := mkRoutine(t, s)

	now := time.Now()
	if err := s.UpdateRoutineLastFiredAt(bg(), task.ID, &now); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.RoutineLastFiredAt == nil || !got.RoutineLastFiredAt.Equal(now) {
		t.Fatalf("LastFiredAt = %v, want %v", got.RoutineLastFiredAt, now)
	}

	if err := s.UpdateRoutineLastFiredAt(bg(), task.ID, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if got.RoutineLastFiredAt != nil {
		t.Fatalf("LastFiredAt not cleared: %v", got.RoutineLastFiredAt)
	}
}

func TestUpdateRoutineSpawnKind_Persists(t *testing.T) {
	s := newTestStore(t)
	task := mkRoutine(t, s)

	if err := s.UpdateRoutineSpawnKind(bg(), task.ID, TaskKindIdeaAgent); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.RoutineSpawnKind != TaskKindIdeaAgent {
		t.Fatalf("SpawnKind = %q, want idea-agent", got.RoutineSpawnKind)
	}
}

func TestUpdateRoutineWriters_RejectNonRoutine(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "normal", Timeout: 10})

	if err := s.UpdateRoutineSchedule(bg(), task.ID, 3600); err == nil {
		t.Fatalf("expected error updating schedule on non-routine task")
	}
	if err := s.UpdateRoutineEnabled(bg(), task.ID, true); err == nil {
		t.Fatalf("expected error enabling non-routine task")
	}
	now := time.Now()
	if err := s.UpdateRoutineNextRun(bg(), task.ID, &now); err == nil {
		t.Fatalf("expected error setting NextRun on non-routine task")
	}
	if err := s.UpdateRoutineLastFiredAt(bg(), task.ID, &now); err == nil {
		t.Fatalf("expected error setting LastFiredAt on non-routine task")
	}
	if err := s.UpdateRoutineSpawnKind(bg(), task.ID, TaskKindIdeaAgent); err == nil {
		t.Fatalf("expected error setting spawn kind on non-routine task")
	}
}

func TestUpdateRoutineWriters_UnknownID(t *testing.T) {
	s := newTestStore(t)
	unknown := uuid.New()

	if err := s.UpdateRoutineSchedule(bg(), unknown, 60); err == nil {
		t.Fatalf("expected error for unknown id")
	}
}

func TestTaskHelpers_IsRoutineAndIsIdeaAgent(t *testing.T) {
	regular := &Task{Kind: TaskKindTask}
	if regular.IsRoutine() || regular.IsIdeaAgent() {
		t.Fatalf("regular task reported as special kind")
	}
	idea := &Task{Kind: TaskKindIdeaAgent}
	if idea.IsRoutine() || !idea.IsIdeaAgent() {
		t.Fatalf("idea-agent classification wrong")
	}
	routine := &Task{Kind: TaskKindRoutine}
	if !routine.IsRoutine() || routine.IsIdeaAgent() {
		t.Fatalf("routine classification wrong")
	}
}

func TestRoutineFields_OmitEmptyForNonRoutineTask(t *testing.T) {
	// A plain task must not serialize any routine_* field, so existing
	// task.json files on disk remain byte-identical after upgrade.
	task := &Task{ID: uuid.New(), Kind: TaskKindTask, Status: TaskStatusBacklog}
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	encoded := string(raw)
	for _, needle := range []string{"routine_interval", "routine_enabled", "routine_next_run", "routine_last_fired", "routine_spawn_kind"} {
		if strings.Contains(encoded, needle) {
			t.Fatalf("non-routine JSON leaked %q: %s", needle, encoded)
		}
	}
}
