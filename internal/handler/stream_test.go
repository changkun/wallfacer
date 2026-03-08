package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
)

// --- parseTurnNumber ---

func TestParseTurnNumber_ValidJSON(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     int
	}{
		{"simple json", "turn-0001.json", 1},
		{"zero padded", "turn-0042.json", 42},
		{"three digits", "turn-100.json", 100},
		{"stderr txt", "turn-0001.stderr.txt", 1},
		{"turn 0", "turn-0000.json", 0},
		{"large turn", "turn-9999.json", 9999},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTurnNumber(tc.filename)
			if got != tc.want {
				t.Errorf("parseTurnNumber(%q) = %d, want %d", tc.filename, got, tc.want)
			}
		})
	}
}

func TestParseTurnNumber_Invalid(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"no dot", "turn-0001"},
		{"not a turn file", "output.json"},
		{"empty string", ""},
		{"just dot", "turn-.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTurnNumber(tc.filename)
			if got != 0 {
				t.Errorf("parseTurnNumber(%q) = %d, want 0", tc.filename, got)
			}
		})
	}
}

// --- serveStoredLogs (via StreamLogs for non-running tasks) ---

func TestStreamLogs_TaskNotFound(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	// Immediately cancel — non-running task with no logs.
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusCancelled)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()

	// serveStoredLogs is called for done/cancelled tasks (no live container).
	// When there are no outputs saved, it returns "no logs saved" 404.
	h.serveStoredLogs(w, req, task.ID)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when no logs, got %d", w.Code)
	}
}

func TestServeStoredLogs_ShowsNoOutputMessage(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	// Create an empty outputs directory but no turn files.
	outputsDir := h.store.OutputsDir(task.ID)
	os.MkdirAll(outputsDir, 0755)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.serveStoredLogs(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (empty dir), got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "no output saved") {
		t.Errorf("expected 'no output saved' message, got: %s", w.Body.String())
	}
}

func TestServeStoredLogs_ServesTurnFiles(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	outputsDir := h.store.OutputsDir(task.ID)
	os.MkdirAll(outputsDir, 0755)
	os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"result": "ok"}`), 0644)
	os.WriteFile(filepath.Join(outputsDir, "turn-0002.json"), []byte(`{"result": "done"}`), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.serveStoredLogs(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"result": "ok"`) {
		t.Errorf("expected turn 1 output in response, got: %s", body)
	}
	if !strings.Contains(body, `"result": "done"`) {
		t.Errorf("expected turn 2 output in response, got: %s", body)
	}
}

func TestServeStoredLogsUpTo_FiltersHigherTurns(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	outputsDir := h.store.OutputsDir(task.ID)
	os.MkdirAll(outputsDir, 0755)
	os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"turn": 1}`), 0644)
	os.WriteFile(filepath.Join(outputsDir, "turn-0002.json"), []byte(`{"turn": 2}`), 0644)
	os.WriteFile(filepath.Join(outputsDir, "turn-0003.json"), []byte(`{"turn": 3}`), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.serveStoredLogsUpTo(w, req, task.ID, 2)

	body := w.Body.String()
	if !strings.Contains(body, `"turn": 1`) {
		t.Error("expected turn 1 in response")
	}
	if !strings.Contains(body, `"turn": 2`) {
		t.Error("expected turn 2 in response")
	}
	if strings.Contains(body, `"turn": 3`) {
		t.Error("turn 3 should be excluded (above maxTurn=2)")
	}
}

func TestServeStoredLogsFrom_FiltersLowerTurns(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	outputsDir := h.store.OutputsDir(task.ID)
	os.MkdirAll(outputsDir, 0755)
	os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"turn": 1}`), 0644)
	os.WriteFile(filepath.Join(outputsDir, "turn-0002.json"), []byte(`{"turn": 2}`), 0644)
	os.WriteFile(filepath.Join(outputsDir, "turn-0003.json"), []byte(`{"turn": 3}`), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.serveStoredLogsFrom(w, req, task.ID, 2)

	body := w.Body.String()
	if strings.Contains(body, `"turn": 1`) {
		t.Error("turn 1 should be excluded (at or below fromTurn=2)")
	}
	if strings.Contains(body, `"turn": 2`) {
		t.Error("turn 2 should be excluded (exclusive: fromTurn=2 means >2)")
	}
	if !strings.Contains(body, `"turn": 3`) {
		t.Error("expected turn 3 in response (above fromTurn=2)")
	}
}

func TestServeStoredLogs_SkipsEmptyFiles(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	outputsDir := h.store.OutputsDir(task.ID)
	os.MkdirAll(outputsDir, 0755)
	// Empty file — should be skipped.
	os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(""), 0644)
	os.WriteFile(filepath.Join(outputsDir, "turn-0002.json"), []byte("  \n  "), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.serveStoredLogs(w, req, task.ID)

	if !strings.Contains(w.Body.String(), "no output saved") {
		t.Errorf("expected 'no output saved' message for empty files, got: %s", w.Body.String())
	}
}

func TestServeStoredLogs_SkipsNonTurnFiles(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	outputsDir := h.store.OutputsDir(task.ID)
	os.MkdirAll(outputsDir, 0755)
	// Non-turn file — should be skipped.
	os.WriteFile(filepath.Join(outputsDir, "metadata.json"), []byte(`{"meta": true}`), 0644)
	os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"turn": 1}`), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.serveStoredLogs(w, req, task.ID)

	body := w.Body.String()
	if strings.Contains(body, `"meta": true`) {
		t.Error("metadata.json should not appear in logs output")
	}
	if !strings.Contains(body, `"turn": 1`) {
		t.Error("expected turn-0001.json content in output")
	}
}

// TestStreamTasks_InitialSnapshot verifies that StreamTasks sends a "snapshot" SSE event
// containing the full task list on first connect.
func TestStreamTasks_InitialSnapshot(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "my task", 15, false, "", "")

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()

	// The snapshot is written before the select loop, so a short pause is sufficient.
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done // ensure goroutine exits before reading body

	body := w.Body.String()
	if !strings.Contains(body, "event: snapshot") {
		t.Errorf("expected 'event: snapshot' in response, got:\n%s", body)
	}
	if !strings.Contains(body, task.ID.String()) {
		t.Errorf("expected task ID %s in snapshot, got:\n%s", task.ID, body)
	}
}

// TestStreamTasks_DeltaOnUpdate verifies that a task mutation after connect emits a
// single "task-updated" SSE event — not a full list snapshot.
func TestStreamTasks_DeltaOnUpdate(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "delta test", 15, false, "", "")
	// Create a second task so the full list has >1 entry; the delta must carry only 1.
	h.store.CreateTask(ctx, "other task", 15, false, "", "")

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()

	// Wait for the snapshot to be written, then trigger a mutation.
	time.Sleep(20 * time.Millisecond)
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, "event: task-updated") {
		t.Errorf("expected 'event: task-updated' in response, got:\n%s", body)
	}
	if !strings.Contains(body, task.ID.String()) {
		t.Errorf("expected mutated task ID %s in delta, got:\n%s", task.ID, body)
	}
	// The delta payload must be a single JSON object, not an array.
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if line == "event: task-updated" && i+1 < len(lines) {
			data := strings.TrimPrefix(lines[i+1], "data: ")
			var obj map[string]any
			if err := json.Unmarshal([]byte(data), &obj); err != nil {
				t.Errorf("task-updated payload is not a JSON object: %v\ndata: %s", err, data)
			}
			break
		}
	}
}

// TestStreamTasks_DeleteEmitsTaskDeleted verifies that deleting a task emits
// a "task-deleted" event carrying the task ID.
func TestStreamTasks_DeleteEmitsTaskDeleted(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "to delete", 15, false, "", "")

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()

	time.Sleep(20 * time.Millisecond)
	h.store.DeleteTask(ctx, task.ID)
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, "event: task-deleted") {
		t.Errorf("expected 'event: task-deleted' in response, got:\n%s", body)
	}
	if !strings.Contains(body, task.ID.String()) {
		t.Errorf("expected task ID %s in task-deleted event, got:\n%s", task.ID, body)
	}
}

// flushRecorder wraps httptest.ResponseRecorder and implements http.Flusher.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}
