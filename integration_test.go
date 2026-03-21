package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/handler"
	"changkun.de/wallfacer/internal/metrics"
	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// newTestServer creates an in-process HTTP server backed by a real store and a
// MockRunner so that integration tests exercise the full mux→handler→store
// path without launching containers.
func newTestServer(t *testing.T) (*httptest.Server, *runner.MockRunner, *store.Store) {
	t.Helper()

	workdir := t.TempDir()
	envPath := filepath.Join(workdir, ".env")
	if err := os.WriteFile(envPath, []byte("ANTHROPIC_API_KEY=test\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	s, err := store.NewStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	mock := &runner.MockRunner{
		EnvFilePath: envPath,
		Cmd:         "true",
		Image:       "wallfacer:latest",
		WtDir:       filepath.Join(workdir, "wt"),
	}

	h := handler.NewHandler(s, mock, workdir, []string{workdir}, metrics.NewRegistry())
	mux := BuildMux(h, metrics.NewRegistry(), IndexViewData{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, mock, s
}

// readFirstSSEData reads from body line-by-line until it finds a line
// beginning with "data: " and returns the payload bytes.
func readFirstSSEData(t *testing.T, body io.Reader) []byte {
	t.Helper()
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			return []byte(after)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("SSE scanner error: %v", err)
	}
	t.Fatal("no data: line found in SSE stream")
	return nil
}

// postJSON sends a POST request with JSON body and returns the response.
func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// mustCreateTask creates a task via the API and returns its ID.
func mustCreateTask(t *testing.T, srvURL string) uuid.UUID {
	t.Helper()
	resp := postJSON(t, srvURL+"/api/tasks", `{"prompt":"test task"}`)
	defer func() { _ = resp.Body.Close()
 }()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create task: status %d, body: %s", resp.StatusCode, body)
	}
	var task store.Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return task.ID
}

// TestCreateAndListTask verifies that POST /api/tasks creates a task and
// GET /api/tasks returns it with status "backlog".
func TestCreateAndListTask(t *testing.T) {
	srv, _, _ := newTestServer(t)

	// Create a task.
	resp := postJSON(t, srv.URL+"/api/tasks", `{"prompt":"hello"}`)
	defer func() { _ = resp.Body.Close()
 }()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /api/tasks: status %d, body: %s", resp.StatusCode, b)
	}
	var created store.Task
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created task: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("created task has nil UUID")
	}
	if created.Status != store.TaskStatusBacklog {
		t.Fatalf("created task status = %q, want %q", created.Status, store.TaskStatusBacklog)
	}

	// List tasks and find the one we just created.
	listResp, err := http.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatalf("GET /api/tasks: %v", err)
	}
	defer func() { _ = listResp.Body.Close()
 }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/tasks: status %d", listResp.StatusCode)
	}
	var tasks []store.Task
	if err := json.NewDecoder(listResp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	found := false
	for _, task := range tasks {
		if task.ID == created.ID {
			found = true
			if task.Status != store.TaskStatusBacklog {
				t.Errorf("task status = %q, want %q", task.Status, store.TaskStatusBacklog)
			}
			break
		}
	}
	if !found {
		t.Fatalf("created task %s not found in task list", created.ID)
	}
}

// TestPatchTaskToInProgress verifies that PATCH /api/tasks/{id} with
// {"status":"in_progress"} triggers RunBackground on the mock runner.
func TestPatchTaskToInProgress(t *testing.T) {
	srv, mock, _ := newTestServer(t)
	taskID := mustCreateTask(t, srv.URL)

	// PATCH status to in_progress.
	req, _ := http.NewRequest(http.MethodPatch,
		fmt.Sprintf("%s/api/tasks/%s", srv.URL, taskID),
		bytes.NewBufferString(`{"status":"in_progress"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH task: %v", err)
	}
	defer func() { _ = resp.Body.Close()
 }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("PATCH task status: status %d, body: %s", resp.StatusCode, b)
	}

	// The handler calls RunBackground synchronously before returning the
	// response, so the mock should have the task ID recorded by now.
	runs := mock.RunCalls()
	for _, id := range runs {
		if id == taskID {
			return
		}
	}
	t.Fatalf("RunBackground not called with task ID %s; calls: %v", taskID, runs)
}

// TestCancelTask verifies that POST /api/tasks/{id}/cancel → 200 and the task
// status becomes "cancelled".
func TestCancelTask(t *testing.T) {
	srv, _, _ := newTestServer(t)
	taskID := mustCreateTask(t, srv.URL)

	// Cancel the task.
	resp := postJSON(t, fmt.Sprintf("%s/api/tasks/%s/cancel", srv.URL, taskID), "")
	defer func() { _ = resp.Body.Close()
 }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST cancel: status %d, body: %s", resp.StatusCode, b)
	}

	// Verify the status is now "cancelled".
	listResp, err := http.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatalf("GET /api/tasks: %v", err)
	}
	defer func() { _ = listResp.Body.Close()
 }()
	var tasks []store.Task
	if err := json.NewDecoder(listResp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	for _, task := range tasks {
		if task.ID == taskID {
			if task.Status != store.TaskStatusCancelled {
				t.Errorf("task status = %q, want %q", task.Status, store.TaskStatusCancelled)
			}
			return
		}
	}
	t.Fatalf("task %s not found after cancel", taskID)
}

// TestGetTaskEvents verifies that GET /api/tasks/{id}/events returns a JSON
// array containing at least one state_change event.
func TestGetTaskEvents(t *testing.T) {
	srv, _, _ := newTestServer(t)
	taskID := mustCreateTask(t, srv.URL)

	evResp, err := http.Get(fmt.Sprintf("%s/api/tasks/%s/events", srv.URL, taskID))
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer func() { _ = evResp.Body.Close()
 }()
	if evResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(evResp.Body)
		t.Fatalf("GET events: status %d, body: %s", evResp.StatusCode, b)
	}

	var events []store.TaskEvent
	if err := json.NewDecoder(evResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one event, got 0")
	}
	for _, ev := range events {
		if ev.EventType == store.EventTypeStateChange {
			return
		}
	}
	t.Fatalf("no state_change event found; events: %v", events)
}

// TestEnvRoundtrip verifies that PUT /api/env updates a field and GET /api/env
// reflects the change.
func TestEnvRoundtrip(t *testing.T) {
	srv, _, _ := newTestServer(t)

	const model = "claude-3-5-sonnet-20241022"

	// PUT the new model.
	body := fmt.Sprintf(`{"default_model":%q}`, model)
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/env",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/env: %v", err)
	}
	defer func() { _ = putResp.Body.Close()
 }()
	if putResp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(putResp.Body)
		t.Fatalf("PUT /api/env: status %d, want 204, body: %s", putResp.StatusCode, b)
	}

	// GET and check.
	getResp, err := http.Get(srv.URL + "/api/env")
	if err != nil {
		t.Fatalf("GET /api/env: %v", err)
	}
	defer func() { _ = getResp.Body.Close()
 }()
	if getResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(getResp.Body)
		t.Fatalf("GET /api/env: status %d, body: %s", getResp.StatusCode, b)
	}
	var cfg map[string]interface{}
	if err := json.NewDecoder(getResp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode env config: %v", err)
	}
	got, _ := cfg["default_model"].(string)
	if got != model {
		t.Fatalf("default_model = %q, want %q", got, model)
	}
}

// TestSSETaskStream verifies that GET /api/tasks/stream immediately sends an
// SSE snapshot event containing a valid JSON task list.
func TestSSETaskStream(t *testing.T) {
	srv, _, _ := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/tasks/stream", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/tasks/stream: %v", err)
	}
	defer func() { _ = resp.Body.Close()
 }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/tasks/stream: status %d", resp.StatusCode)
	}

	data := readFirstSSEData(t, resp.Body)
	var tasks []interface{}
	if err := json.Unmarshal(data, &tasks); err != nil {
		t.Fatalf("unmarshal SSE task list: %v (data=%q)", err, data)
	}
	// An empty list is expected for a fresh server — just verify it's a valid array.
}

// TestUnknownTaskID verifies that cancelling a non-existent task returns 404.
func TestUnknownTaskID(t *testing.T) {
	srv, _, _ := newTestServer(t)

	nonExistent := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	resp := postJSON(t, fmt.Sprintf("%s/api/tasks/%s/cancel", srv.URL, nonExistent), "")
	defer func() { _ = resp.Body.Close()
 }()
	if resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("cancel unknown task: status %d, want 404, body: %s", resp.StatusCode, b)
	}
}

// TestInvalidJSON verifies that POST /api/tasks with malformed JSON returns 400.
func TestInvalidJSON(t *testing.T) {
	srv, _, _ := newTestServer(t)

	resp := postJSON(t, srv.URL+"/api/tasks", `{bad json`)
	defer func() { _ = resp.Body.Close()
 }()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("invalid JSON: status %d, want 400, body: %s", resp.StatusCode, b)
	}
}
