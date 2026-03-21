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

	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	// Immediately cancel — non-running task with no logs.
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusCancelled)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	// Create an empty outputs directory but no turn files.
	outputsDir := h.store.OutputsDir(task.ID)
	_ = os.MkdirAll(outputsDir, 0755)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	outputsDir := h.store.OutputsDir(task.ID)
	_ = os.MkdirAll(outputsDir, 0755)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"result": "ok"}`), 0644)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0002.json"), []byte(`{"result": "done"}`), 0644)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	outputsDir := h.store.OutputsDir(task.ID)
	_ = os.MkdirAll(outputsDir, 0755)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"turn": 1}`), 0644)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0002.json"), []byte(`{"turn": 2}`), 0644)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0003.json"), []byte(`{"turn": 3}`), 0644)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	outputsDir := h.store.OutputsDir(task.ID)
	_ = os.MkdirAll(outputsDir, 0755)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"turn": 1}`), 0644)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0002.json"), []byte(`{"turn": 2}`), 0644)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0003.json"), []byte(`{"turn": 3}`), 0644)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	outputsDir := h.store.OutputsDir(task.ID)
	_ = os.MkdirAll(outputsDir, 0755)

	// Empty file — should be skipped.
	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(""), 0644)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0002.json"), []byte("  \n  "), 0644)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	outputsDir := h.store.OutputsDir(task.ID)
	_ = os.MkdirAll(outputsDir, 0755)

	// Non-turn file — should be skipped.
	_ = os.WriteFile(filepath.Join(outputsDir, "metadata.json"), []byte(`{"meta": true}`), 0644)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"turn": 1}`), 0644)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "my task", Timeout: 15})

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "delta test", Timeout: 15})
	// Create a second task so the full list has >1 entry; the delta must carry only 1.
	_, _ = h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "other task", Timeout: 15})


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
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "to delete", Timeout: 15})

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()

	time.Sleep(20 * time.Millisecond)
	_ = h.store.DeleteTask(ctx, task.ID, "")

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

// nonFlushingWriter wraps httptest.ResponseRecorder but deliberately does NOT
// expose http.Flusher so tests can exercise the "streaming not supported" path.
type nonFlushingWriter struct {
	header http.Header
	code   int
	body   strings.Builder
}

func newNonFlushingWriter() *nonFlushingWriter {
	return &nonFlushingWriter{header: make(http.Header), code: http.StatusOK}
}

func (w *nonFlushingWriter) Header() http.Header         { return w.header }
func (w *nonFlushingWriter) WriteHeader(code int)        { w.code = code }
func (w *nonFlushingWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *nonFlushingWriter) Body() string                { return w.body.String() }

// --- SSE id: field and delta replay tests ---

// TestStreamTasks_SnapshotCarriesID verifies that the snapshot event includes
// an "id:" field with a numeric sequence number.
func TestStreamTasks_SnapshotCarriesID(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	_, _ = h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task1", Timeout: 15})


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "id test", Timeout: 15})

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stream", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamTasks(w, req)
	}()

	time.Sleep(20 * time.Millisecond)
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "mono", Timeout: 15})

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
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "replay me", Timeout: 15})

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
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

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
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)


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
	_, _ = h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "gap test", Timeout: 15})


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
	_, _ = h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "fresh", Timeout: 15})


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "header replay", Timeout: 15})

	// Trigger a mutation so the replay buffer has at least one entry.
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)


	// Record the current seq (after the mutation).
	seqBefore := h.store.LatestDeltaSeq()

	// Trigger another mutation that the client will have "missed".
	// in_progress → waiting is a valid transition.
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)


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

// --- Additional StreamLogs and StreamRefineLogs coverage tests ---

// newTestHandlerWithMockRunner creates a Handler backed by a temp-dir store and
// a MockRunner (not a real Runner), so individual fields like ContainerNameFn
// and RefineContainerNameFn can be configured per-test.
func newTestHandlerWithMockRunner(t *testing.T, mock *runner.MockRunner) (*Handler, *store.Store) {
	t.Helper()
	storeDir, err := os.MkdirTemp("", "wallfacer-mock-handler-test-*")
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.NewStore(storeDir)
	if err != nil {
		_ = os.RemoveAll(storeDir)

		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(storeDir) })

	h := &Handler{runner: mock, store: s}
	return h, s
}

// TestStreamLogs_UnknownTask verifies that StreamLogs returns 404 for unknown task IDs.
func TestStreamLogs_UnknownTask(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+uuid.New().String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.StreamLogs(w, req, uuid.New())
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown task, got %d", w.Code)
	}
}

// TestStreamLogs_DoneTaskServesStoredLogs verifies that StreamLogs falls back to
// serveStoredLogs for tasks that are done (no live container).
func TestStreamLogs_DoneTaskServesStoredLogs(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)


	// Write a turn file so serveStoredLogs returns 200 instead of 404.
	outputsDir := h.store.OutputsDir(task.ID)
	_ = os.MkdirAll(outputsDir, 0755)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"result":"ok"}`), 0644)


	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.StreamLogs(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for done task with logs, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"result":"ok"`) {
		t.Errorf("expected turn output in response, got: %s", w.Body.String())
	}
}

// TestStreamLogs_CancelledTaskServesStoredLogs verifies that StreamLogs falls
// back to serveStoredLogs for cancelled tasks.
func TestStreamLogs_CancelledTaskServesStoredLogs(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "cancel test", Timeout: 15})
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusCancelled)


	// No turn files; serveStoredLogs will return 404 "no logs saved".
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.StreamLogs(w, req, task.ID)

	// The task exists but has no saved logs — expect 404 from serveStoredLogs.
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 (no logs saved), got %d", w.Code)
	}
}

// TestStreamLogs_PhaseImplQueryParam verifies that ?phase=impl routes to
// serveStoredLogsUpTo. With TestRunStartTurn=0 (default), maxTurn=0 means
// no upper bound and all saved turns are served.
func TestStreamLogs_PhaseImplQueryParam(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "impl phase test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)


	outputsDir := h.store.OutputsDir(task.ID)
	_ = os.MkdirAll(outputsDir, 0755)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"turn":1}`), 0644)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0002.json"), []byte(`{"turn":2}`), 0644)


	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs?phase=impl", nil)
	w := httptest.NewRecorder()
	h.StreamLogs(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for impl phase with turn files, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"turn":1`) {
		t.Error("expected turn 1 in impl phase output")
	}
	if !strings.Contains(body, `"turn":2`) {
		t.Error("expected turn 2 in impl phase output")
	}
}

// TestStreamLogs_PhaseTestQueryParam verifies that ?phase=test routes to
// serveStoredLogsFrom for non-running tasks.
func TestStreamLogs_PhaseTestQueryParam(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test phase test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)


	outputsDir := h.store.OutputsDir(task.ID)
	_ = os.MkdirAll(outputsDir, 0755)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"turn":1}`), 0644)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0002.json"), []byte(`{"turn":2}`), 0644)


	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs?phase=test", nil)
	w := httptest.NewRecorder()
	h.StreamLogs(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for test phase with turn files, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"turn":1`) {
		t.Error("expected turn 1 in test phase output (fromTurn=0 means no lower bound)")
	}
	if !strings.Contains(body, `"turn":2`) {
		t.Error("expected turn 2 in test phase output")
	}
}

// TestStreamLogs_InProgressNoContainerExitsOnContextCancel verifies that
// StreamLogs for an in-progress task with no live container (runner returns "")
// falls back gracefully (serveStoredLogs) rather than panicking.
func TestStreamLogs_InProgressNoContainerExitsOnContextCancel(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "in progress no container", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)


	// The mock runner always returns "" for ContainerName, so StreamLogs
	// falls back to serveStoredLogs (no turn files → 404).
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.StreamLogs(w, req, task.ID)

	// No turn files, no container: expect the "no logs saved" 404 path.
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 (no logs saved for in-progress task with no container), got %d", w.Code)
	}
}

// TestStreamRefineLogs_NoContainerReturns204 verifies that StreamRefineLogs
// returns 204 No Content when the MockRunner reports no active refine container.
func TestStreamRefineLogs_NoContainerReturns204(t *testing.T) {
	h := newTestHandler(t)
	// The mock runner always returns "" for RefineContainerName.
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+uuid.New().String()+"/refine/logs", nil)
	w := httptest.NewRecorder()
	h.StreamRefineLogs(w, req, uuid.New())

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 when no refine container is active, got %d", w.Code)
	}
}

// TestStreamLogs_InProgress_NoFlusher verifies that StreamLogs returns 500 when
// the ResponseWriter does not implement http.Flusher and the task is in-progress
// with a live container name. Uses nonFlushingWriter which deliberately omits
// the http.Flusher interface.
func TestStreamLogs_InProgress_NoFlusher(t *testing.T) {
	mock := &runner.MockRunner{
		Cmd: "echo",
		ContainerNameFn: func(_ uuid.UUID) string { return "test-container" },
	}
	h, s := newTestHandlerWithMockRunner(t, mock)

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "no-flusher test", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	// nonFlushingWriter deliberately does not implement http.Flusher.
	w := newNonFlushingWriter()
	h.StreamLogs(w, req, task.ID)

	if w.code != http.StatusInternalServerError {
		t.Errorf("expected 500 (no flusher), got %d", w.code)
	}
}

// TestStreamRefineLogs_NoFlusher verifies that StreamRefineLogs returns 500
// when the ResponseWriter does not implement http.Flusher but a refine
// container is active. Uses nonFlushingWriter which deliberately omits Flusher.
func TestStreamRefineLogs_NoFlusher(t *testing.T) {
	mock := &runner.MockRunner{
		Cmd: "echo",
		RefineContainerNameFn: func(_ uuid.UUID) string { return "refine-container" },
	}
	h, _ := newTestHandlerWithMockRunner(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+uuid.New().String()+"/refine/logs", nil)
	// nonFlushingWriter deliberately does not implement http.Flusher.
	w := newNonFlushingWriter()
	h.StreamRefineLogs(w, req, uuid.New())

	if w.code != http.StatusInternalServerError {
		t.Errorf("expected 500 (no flusher), got %d", w.code)
	}
}

// TestStreamLogs_InProgress_ContainerExitsCleanly exercises the live streaming
// path in StreamLogs end-to-end. The MockRunner returns a non-empty container
// name and uses a wrapper script as the command, which prints a couple of log
// lines and exits. The handler's select loop reads the lines and then
// terminates naturally when the lines channel is closed.
func TestStreamLogs_InProgress_ContainerExitsCleanly(t *testing.T) {
	// Write a tiny shell script that prints two log lines and exits.
	scriptPath := filepath.Join(t.TempDir(), "fake-logs.sh")
	script := "#!/bin/sh\nprintf 'log line 1\\nlog line 2\\n'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	// Wrapper script: ignores all arguments (logs -f --tail 100 <name>) and
	// just runs the inner script.
	wrapperPath := filepath.Join(t.TempDir(), "fake-podman.sh")
	wrapper := fmt.Sprintf("#!/bin/sh\nexec sh \"%s\"\n", scriptPath)
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}

	mock := &runner.MockRunner{
		Cmd:             wrapperPath,
		ContainerNameFn: func(_ uuid.UUID) string { return "fake-container" },
	}
	h, s := newTestHandlerWithMockRunner(t, mock)

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "live stream test", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.StreamLogs(w, req, task.ID)

	body := w.Body.String()
	if !strings.Contains(body, "log line 1") {
		t.Errorf("expected 'log line 1' in response, got: %s", body)
	}
	if !strings.Contains(body, "log line 2") {
		t.Errorf("expected 'log line 2' in response, got: %s", body)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestStreamLogs_InProgress_CommandStartFails exercises the cmd.Start error
// path in StreamLogs. When the container command binary does not exist,
// cmd.Start returns an error and StreamLogs responds with 500.
func TestStreamLogs_InProgress_CommandStartFails(t *testing.T) {
	mock := &runner.MockRunner{
		// Use a non-existent binary so cmd.Start() fails.
		Cmd:             "/nonexistent-binary-that-cannot-be-found",
		ContainerNameFn: func(_ uuid.UUID) string { return "some-container" },
	}
	h, s := newTestHandlerWithMockRunner(t, mock)

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "start-fail test", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.StreamLogs(w, req, task.ID)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when command fails to start, got %d", w.Code)
	}
}

// TestStreamLogs_InProgress_ContextCancellation verifies that StreamLogs
// exits cleanly when the request context is cancelled while logs are streaming.
func TestStreamLogs_InProgress_ContextCancellation(t *testing.T) {
	// A script that blocks indefinitely.
	scriptPath := filepath.Join(t.TempDir(), "blocking.sh")
	script := "#!/bin/sh\nwhile true; do sleep 1; done\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	wrapperPath := filepath.Join(t.TempDir(), "fake-podman-block.sh")
	wrapper := fmt.Sprintf("#!/bin/sh\nexec sh \"%s\"\n", scriptPath)
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}

	mock := &runner.MockRunner{
		Cmd:             wrapperPath,
		ContainerNameFn: func(_ uuid.UUID) string { return "blocking-container" },
	}
	h, s := newTestHandlerWithMockRunner(t, mock)

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "cancellation test", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamLogs(w, req, task.ID)
	}()

	// Give the handler time to start the subprocess, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Handler exited cleanly after context cancellation.
	case <-time.After(5 * time.Second):
		t.Error("StreamLogs did not exit after context cancellation")
	}
}

// TestStreamRefineLogs_ContainerExitsCleanly exercises the live streaming path
// in StreamRefineLogs. The command prints two lines and exits so the lines
// channel closes and the handler returns naturally.
func TestStreamRefineLogs_ContainerExitsCleanly(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "refine-logs.sh")
	script := "#!/bin/sh\nprintf 'refine line 1\\nrefine line 2\\n'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	wrapperPath := filepath.Join(t.TempDir(), "fake-podman-refine.sh")
	wrapper := fmt.Sprintf("#!/bin/sh\nexec sh \"%s\"\n", scriptPath)
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}

	mock := &runner.MockRunner{
		Cmd:                   wrapperPath,
		RefineContainerNameFn: func(_ uuid.UUID) string { return "refine-container" },
	}
	h, _ := newTestHandlerWithMockRunner(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+uuid.New().String()+"/refine/logs", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.StreamRefineLogs(w, req, uuid.New())

	body := w.Body.String()
	if !strings.Contains(body, "refine line 1") {
		t.Errorf("expected 'refine line 1' in response, got: %s", body)
	}
	if !strings.Contains(body, "refine line 2") {
		t.Errorf("expected 'refine line 2' in response, got: %s", body)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestStreamRefineLogs_CommandStartFails exercises the cmd.Start error path in
// StreamRefineLogs. When the binary does not exist, Start() fails and the
// handler responds with 500.
func TestStreamRefineLogs_CommandStartFails(t *testing.T) {
	mock := &runner.MockRunner{
		Cmd:                   "/nonexistent-binary-xyz",
		RefineContainerNameFn: func(_ uuid.UUID) string { return "refine-container" },
	}
	h, _ := newTestHandlerWithMockRunner(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+uuid.New().String()+"/refine/logs", nil)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.StreamRefineLogs(w, req, uuid.New())

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when command fails to start, got %d", w.Code)
	}
}

// TestStreamRefineLogs_ContextCancellation verifies that StreamRefineLogs
// exits cleanly when the request context is cancelled while streaming.
func TestStreamRefineLogs_ContextCancellation(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "blocking-refine.sh")
	script := "#!/bin/sh\nwhile true; do sleep 1; done\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	wrapperPath := filepath.Join(t.TempDir(), "fake-podman-block-refine.sh")
	wrapper := fmt.Sprintf("#!/bin/sh\nexec sh \"%s\"\n", scriptPath)
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}

	mock := &runner.MockRunner{
		Cmd:                   wrapperPath,
		RefineContainerNameFn: func(_ uuid.UUID) string { return "blocking-refine" },
	}
	h, _ := newTestHandlerWithMockRunner(t, mock)

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+uuid.New().String()+"/refine/logs", nil).WithContext(reqCtx)
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.StreamRefineLogs(w, req, uuid.New())
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Handler exited cleanly.
	case <-time.After(5 * time.Second):
		t.Error("StreamRefineLogs did not exit after context cancellation")
	}
}

// TestStreamLogs_PhaseTest_InProgress verifies that ?phase=test for an
// in-progress task does NOT go to serveStoredLogsFrom but instead falls
// through to the live container path (or stored logs if no container).
func TestStreamLogs_PhaseTest_InProgress(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "phase-test in-progress", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)


	// No container (real runner returns ""), so falls back to serveStoredLogs.
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs?phase=test", nil)
	w := httptest.NewRecorder()
	h.StreamLogs(w, req, task.ID)

	// No turn files saved → serveStoredLogs → 404 "no logs saved".
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 (no logs saved, not stored-from path), got %d", w.Code)
	}
}

// TestStreamLogs_Committing_ServesStoredLogs verifies that a task in the
// "committing" status is treated as non-running and falls back to stored logs.
func TestStreamLogs_Committing_ServesStoredLogs(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "committing test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCommitting)


	outputsDir := h.store.OutputsDir(task.ID)
	_ = os.MkdirAll(outputsDir, 0755)

	_ = os.WriteFile(filepath.Join(outputsDir, "turn-0001.json"), []byte(`{"status":"committing"}`), 0644)


	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.StreamLogs(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for committing task with logs, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "committing") {
		t.Errorf("expected stored log content in response, got: %s", w.Body.String())
	}
}

// TestServeStoredLogsRange_DirectoryMissing verifies that serveStoredLogsRange
// returns 404 when the outputs directory does not exist.
func TestServeStoredLogsRange_DirectoryMissing(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "missing dir", Timeout: 15})

	// Do NOT create the outputs directory.
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/logs", nil)
	w := httptest.NewRecorder()
	h.serveStoredLogsRange(w, req, task.ID, 0, 0)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing outputs directory, got %d", w.Code)
	}
}
