package handler

import (
	"context"
	"encoding/json"
	"fmt"
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

// --- SSE id: field and delta replay tests ---

// TestStreamTasks_SnapshotCarriesID verifies that the snapshot event includes
// an "id:" field with a numeric sequence number.
func TestStreamTasks_SnapshotCarriesID(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	h.store.CreateTask(ctx, "task1", 15, false, "", "")

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	// The SSE output must contain an "id:" line before the snapshot event.
	if !strings.Contains(body, "id:") {
		t.Errorf("expected 'id:' field in SSE output, got:\n%s", body)
	}
	if !strings.Contains(body, "event: snapshot") {
		t.Errorf("expected 'event: snapshot' in output, got:\n%s", body)
	}
}

// TestStreamTasks_DeltaCarriesID verifies that task-updated events include an "id:" field.
func TestStreamTasks_DeltaCarriesID(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "id test", 15, false, "", "")

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()

	time.Sleep(20 * time.Millisecond)
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	// Count lines starting with "id:" — one for snapshot, one for the delta.
	idCount := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "id:") {
			idCount++
		}
	}
	if idCount < 2 {
		t.Errorf("expected at least 2 'id:' fields (snapshot + delta), got %d in:\n%s", idCount, body)
	}
}

// TestStreamTasks_MonotonicIDs verifies that id: values increase monotonically
// across the snapshot and subsequent delta events.
func TestStreamTasks_MonotonicIDs(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "mono", 15, false, "", "")

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()

	time.Sleep(20 * time.Millisecond)
	// Use two valid transitions to generate two delta events after the snapshot.
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	// Extract all id: values and verify they are strictly increasing.
	var ids []int64
	for _, line := range strings.Split(w.Body.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "id:") {
			continue
		}
		valStr := strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		var val int64
		if _, err := fmt.Sscanf(valStr, "%d", &val); err != nil {
			t.Errorf("non-numeric id: %q", valStr)
			continue
		}
		ids = append(ids, val)
	}
	if len(ids) < 2 {
		t.Fatalf("expected at least 2 id: values, got %d in:\n%s", len(ids), w.Body.String())
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Errorf("id[%d]=%d is not greater than id[%d]=%d", i, ids[i], i-1, ids[i-1])
		}
	}
}

// TestStreamTasks_ReplaySuccess verifies that a client reconnecting with a valid
// last_event_id receives only the missed deltas, not a full snapshot.
func TestStreamTasks_ReplaySuccess(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "replay me", 15, false, "", "")

	// First connection: get a snapshot and record the last sequence ID.
	reqCtx1, cancel1 := context.WithCancel(context.Background())
	req1 := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx1)
	w1 := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		h.StreamTasks(w1, req1)
	}()
	time.Sleep(20 * time.Millisecond)

	// Trigger a mutation while still connected so the delta goes into the replay buffer.
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)
	time.Sleep(20 * time.Millisecond)
	cancel1()
	<-done1

	// Extract the last id: field from the first response.
	var lastID string
	for _, line := range strings.Split(w1.Body.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "id:") {
			lastID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		}
	}
	if lastID == "" {
		t.Fatal("could not find last id: field in first connection")
	}

	// Trigger another mutation while disconnected — this will be in the replay buffer.
	// in_progress → waiting is a valid transition.
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	// Second connection: reconnect with last_event_id — should get only the delta, no snapshot.
	reqCtx2, cancel2 := context.WithCancel(context.Background())
	req2 := httptest.NewRequest(http.MethodGet, "/api/tasks/stream?last_event_id="+lastID, nil).WithContext(reqCtx2)
	w2 := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		h.StreamTasks(w2, req2)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel2()
	<-done2

	body2 := w2.Body.String()
	// Should NOT receive a snapshot (because replay succeeded).
	if strings.Contains(body2, "event: snapshot") {
		t.Errorf("expected no snapshot on replay, got:\n%s", body2)
	}
	// Should receive the missed task-updated event.
	if !strings.Contains(body2, "event: task-updated") {
		t.Errorf("expected task-updated replay event, got:\n%s", body2)
	}
	// The replayed delta should reference the task.
	if !strings.Contains(body2, task.ID.String()) {
		t.Errorf("expected task ID %s in replayed delta, got:\n%s", task.ID, body2)
	}
}

// TestStreamTasks_GapFallbackToSnapshot verifies that when the client's
// last_event_id is too old for the replay buffer, a full snapshot is sent.
func TestStreamTasks_GapFallbackToSnapshot(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	h.store.CreateTask(ctx, "gap test", 15, false, "", "")

	// Reconnect with a very old sequence ID that will never be in the buffer.
	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream?last_event_id=-1", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	// last_event_id=-1 is a valid int64 but seq values start at 1, so
	// DeltasSince(-1) with oldest=1 → oldest(1) > seq+1(0) → tooOld=true
	// → fall back to snapshot.
	if !strings.Contains(body, "event: snapshot") {
		t.Errorf("expected snapshot on gap fallback, got:\n%s", body)
	}
}

// TestStreamTasks_NoLastEventID_AlwaysSnapshot verifies that a fresh connection
// (no last_event_id) always receives a snapshot.
func TestStreamTasks_NoLastEventID_AlwaysSnapshot(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	h.store.CreateTask(ctx, "fresh", 15, false, "", "")

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	if !strings.Contains(w.Body.String(), "event: snapshot") {
		t.Errorf("expected snapshot for fresh connection, got:\n%s", w.Body.String())
	}
}

// TestStreamTasks_ReplayViaLastEventIDHeader verifies that the Last-Event-ID
// HTTP header (sent automatically by the browser's native EventSource on
// reconnect) is also honoured.
func TestStreamTasks_ReplayViaLastEventIDHeader(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "header replay", 15, false, "", "")

	// Trigger a mutation so the replay buffer has at least one entry.
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

	// Record the current seq (after the mutation).
	seqBefore := h.store.LatestDeltaSeq()

	// Trigger another mutation that the client will have "missed".
	// in_progress → waiting is a valid transition.
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	// Reconnect using the Last-Event-ID header (as a native EventSource would).
	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	req.Header.Set("Last-Event-ID", fmt.Sprintf("%d", seqBefore))
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	if strings.Contains(body, "event: snapshot") {
		t.Errorf("expected no snapshot when replaying via Last-Event-ID header, got:\n%s", body)
	}
	if !strings.Contains(body, "event: task-updated") {
		t.Errorf("expected replayed task-updated event, got:\n%s", body)
	}
}
