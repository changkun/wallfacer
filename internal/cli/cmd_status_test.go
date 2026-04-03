package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
)

// TestGroupByStatus verifies that tasks are correctly bucketed by their Status field.
func TestGroupByStatus(t *testing.T) {
	tasks := []taskSummary{
		{ID: "a", Status: "backlog"},
		{ID: "b", Status: "in_progress"},
		{ID: "c", Status: "backlog"},
		{ID: "d", Status: "done"},
		{ID: "e", Status: "failed"},
	}
	groups := groupByStatus(tasks)

	if got := len(groups["backlog"]); got != 2 {
		t.Errorf("backlog: got %d tasks, want 2", got)
	}
	if got := len(groups["in_progress"]); got != 1 {
		t.Errorf("in_progress: got %d tasks, want 1", got)
	}
	if got := len(groups["done"]); got != 1 {
		t.Errorf("done: got %d tasks, want 1", got)
	}
	if got := len(groups["failed"]); got != 1 {
		t.Errorf("failed: got %d tasks, want 1", got)
	}
	if got := len(groups["waiting"]); got != 0 {
		t.Errorf("waiting: got %d tasks, want 0", got)
	}
}

// TestGroupByStatusEmpty verifies that nil input produces an empty map.
func TestGroupByStatusEmpty(t *testing.T) {
	groups := groupByStatus(nil)
	if len(groups) != 0 {
		t.Errorf("expected empty map for nil input, got %v", groups)
	}
}

// TestFormatCost validates dollar-formatted cost output with 4 decimal places,
// including rounding behavior at the boundary.
func TestFormatCost(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0.0023, "$0.0023"},
		{0.0, "$0.0000"},
		{1.5, "$1.5000"},
		{0.00001, "$0.0000"},
		{12.3456, "$12.3456"},
		{0.00005, "$0.0001"}, // rounding: 5 rounds up at 4th decimal
	}
	for _, tc := range tests {
		got := formatCost(tc.input)
		if got != tc.want {
			t.Errorf("formatCost(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestTruncate validates string truncation including multi-byte rune handling
// and the ellipsis suffix behavior.
func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello…"},
		{"", 5, ""},
		{"exact", 5, "exact"},
		{"toolong", 4, "tool…"},
		{"αβγδε", 3, "αβγ…"}, // multi-byte rune handling
		{"αβγ", 3, "αβγ"},    // exact rune count, no ellipsis
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.n)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.n, got, tc.want)
		}
	}
}

// TestMatchContainers verifies the task-ID-to-container-name mapping, including
// that containers without a task ID are excluded.
func TestMatchContainers(t *testing.T) {
	containers := []containerSummary{
		{Name: "wallfacer-impl-uuid-1", TaskID: "uuid-1"},
		{Name: "wallfacer-impl-uuid-3", TaskID: "uuid-3"},
		{Name: "wallfacer-unrelated", TaskID: ""},
	}
	result := matchContainers(containers)

	if got := result["uuid-1"]; got != "wallfacer-impl-uuid-1" {
		t.Errorf("uuid-1: got %q, want %q", got, "wallfacer-impl-uuid-1")
	}
	if got := result["uuid-3"]; got != "wallfacer-impl-uuid-3" {
		t.Errorf("uuid-3: got %q, want %q", got, "wallfacer-impl-uuid-3")
	}
	if _, ok := result["uuid-2"]; ok {
		t.Errorf("uuid-2 should have no container mapping")
	}
	if _, ok := result[""]; ok {
		t.Errorf("empty task ID should not produce a mapping")
	}
}

// TestMatchContainersEmpty verifies that nil input produces an empty map.
func TestMatchContainersEmpty(t *testing.T) {
	result := matchContainers(nil)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

// TestMatchContainersDuplicateTaskID verifies last-write-wins semantics when
// multiple containers share the same task ID.
func TestMatchContainersDuplicateTaskID(t *testing.T) {
	// Last container wins when multiple containers share a task ID.
	containers := []containerSummary{
		{Name: "first", TaskID: "uuid-1"},
		{Name: "second", TaskID: "uuid-1"},
	}
	result := matchContainers(containers)
	if result["uuid-1"] != "second" {
		t.Errorf("expected last-write-wins, got %q", result["uuid-1"])
	}
}

// TestStatusLabel validates that known statuses return their display names and
// unknown statuses get auto-capitalized.
func TestStatusLabel(t *testing.T) {
	if got := statusLabel("backlog"); got != "Backlog" {
		t.Fatalf("statusLabel(backlog) = %q, want Backlog", got)
	}
	if got := statusLabel("done"); got != "Done" {
		t.Fatalf("statusLabel(done) = %q, want Done", got)
	}
	if got := statusLabel("mystery"); got != "Mystery" {
		t.Fatalf("statusLabel(unknown) = %q, want Mystery", got)
	}
}

// TestFetchTasks validates successful task list fetching and error handling for
// malformed JSON responses.
func TestFetchTasks(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/tasks" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"11111111-1111-1111-1111-111111111111","title":"A","status":"done","turns":1,"usage":{"cost_usd":1.23}}]`))
		}))
		defer ts.Close()

		tasks, err := fetchTasks(ts.URL)
		if err != nil {
			t.Fatalf("fetchTasks failed: %v", err)
		}
		if len(tasks) != 1 {
			t.Fatalf("expected 1 task, got %d", len(tasks))
		}
		if tasks[0].ID != "11111111-1111-1111-1111-111111111111" {
			t.Fatalf("unexpected task id: %s", tasks[0].ID)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`not-json`))

		}))
		defer ts.Close()

		if _, err := fetchTasks(ts.URL); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

// TestFetchContainers validates successful container list fetching and JSON
// deserialization of task_id fields.
func TestFetchContainers(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/containers" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"c1","task_id":"11111111-1111-1111-1111-111111111111"}]`))
	}))
	defer ts.Close()

	containers, err := fetchContainers(ts.URL)
	if err != nil {
		t.Fatalf("fetchContainers failed: %v", err)
	}
	if len(containers) != 1 || containers[0].TaskID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected containers: %#v", containers)
	}
}

// TestPrintBoard verifies that the board output contains status headings,
// container name annotations, truncated task IDs, and cost totals.
func TestPrintBoard(t *testing.T) {
	tasks := []taskSummary{
		{ID: "1111111111111111111111111111111111111111", Title: "Short", Status: "done", Turns: 1, Usage: taskUsage{CostUSD: 0.1234}},
		{ID: "2222222222222222222222222222222222222222", Prompt: "Long prompt with many words", Status: "done", Turns: 2, Usage: taskUsage{CostUSD: 1.0}},
		{ID: "3333333333333333333333333333333333333333", Title: "Backlog", Status: "backlog", Turns: 0, Usage: taskUsage{CostUSD: 0.0}},
	}
	containers := map[string]string{"1111111111111111111111111111111111111111": "wallfacer-c1"}

	output := captureStdout(func() {
		printBoard("http://localhost:8080", tasks, containers)
	})

	for _, want := range []string{"Done", "Backlog", "[wallfacer-c1]", "Total:", "33333333", "$1.0000"} {
		if !bytes.Contains([]byte(output), []byte(want)) {
			t.Fatalf("expected output to contain %q, got: %s", want, output)
		}
	}
}

// TestRunStatus exercises both the formatted and --json output modes of the
// status subcommand against a mock HTTP server.
func TestRunStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tasks":
			if r.URL.Query().Get("include_archived") != "false" {
				t.Fatalf("unexpected include_archived=%q", r.URL.Query().Get("include_archived"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"11111111-1111-1111-1111-111111111111","title":"A","status":"done","turns":1,"usage":{"cost_usd":0.1}}]`))
		case "/api/containers":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"name":"wallfacer-task-a","task_id":"11111111-1111-1111-1111-111111111111"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	output := captureStdout(func() {
		RunStatus("", []string{"--addr", ts.URL})
	})
	if !bytes.Contains([]byte(output), []byte("Done")) || !bytes.Contains([]byte(output), []byte("11111111")) {
		t.Fatalf("expected board output, got: %s", output)
	}

	jsonOutput := captureStdout(func() {
		RunStatus("", []string{"--addr", ts.URL, "--json"})
	})
	if !bytes.Contains([]byte(jsonOutput), []byte("\"id\":\"11111111-1111-1111-1111-111111111111\"")) {
		t.Fatalf("expected raw JSON output, got: %s", jsonOutput)
	}
}

// TestFetchTasks_ReadBodyError verifies that fetchTasks returns an error when
// the response body cannot be read.
func TestFetchTasks_ReadBodyError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Write header with a content-length that exceeds the actual body,
		// which causes the body read to fail. Use a hijack-based approach instead.
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		// Only write a few bytes then close the connection.
		if h, ok := w.(http.Hijacker); ok {
			conn, _, _ := h.Hijack()
			_ = conn.Close()
			return
		}
		// Fallback: write truncated body.
		_, _ = w.Write([]byte(`[`))
	}))
	defer ts.Close()

	_, err := fetchTasks(ts.URL)
	if err == nil {
		t.Fatal("expected error when body read fails or JSON is truncated")
	}
}

// TestFetchContainers_ReadBodyError verifies error handling when the container
// response body is malformed.
func TestFetchContainers_ReadBodyError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		if h, ok := w.(http.Hijacker); ok {
			conn, _, _ := h.Hijack()
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(`[`))
	}))
	defer ts.Close()

	_, err := fetchContainers(ts.URL)
	if err == nil {
		t.Fatal("expected error when body read fails or JSON is truncated")
	}
}

// TestFetchContainers_InvalidJSON verifies that fetchContainers returns an
// error when the server responds with malformed JSON.
func TestFetchContainers_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer ts.Close()

	if _, err := fetchContainers(ts.URL); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestFetchTasks_ServerDown verifies that fetchTasks returns an error when
// the server is unreachable.
func TestFetchTasks_ServerDown(t *testing.T) {
	_, err := fetchTasks("http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

// TestFetchContainers_ServerDown verifies that fetchContainers returns an error
// when the server is unreachable.
func TestFetchContainers_ServerDown(t *testing.T) {
	_, err := fetchContainers("http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

// TestRunStatus_SubprocessHelper is a test helper used by subprocess tests.
func TestRunStatus_SubprocessHelper(_ *testing.T) {
	if os.Getenv("WALLFACER_STATUS_HELPER") != "1" {
		return
	}
	mode := os.Getenv("WALLFACER_STATUS_MODE")
	addr := os.Getenv("WALLFACER_STATUS_ADDR")
	switch mode {
	case "render":
		RunStatus("", []string{"-addr", addr})
	case "json":
		RunStatus("", []string{"-addr", addr, "--json"})
	}
}

// TestRunStatus_RenderServerDown verifies that `wallfacer status` exits with
// a non-zero code when the server is unreachable.
func TestRunStatus_RenderServerDown(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestRunStatus_SubprocessHelper", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"WALLFACER_STATUS_HELPER=1",
		"WALLFACER_STATUS_MODE=render",
		"WALLFACER_STATUS_ADDR=http://127.0.0.1:1",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit code for unreachable server")
	}
	if !bytes.Contains(out, []byte("not reachable")) {
		t.Fatalf("expected 'not reachable' in output, got: %s", out)
	}
}

// TestRunStatus_JsonServerDown verifies that `wallfacer status --json` exits
// with non-zero code when the server is unreachable.
func TestRunStatus_JsonServerDown(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestRunStatus_SubprocessHelper", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"WALLFACER_STATUS_HELPER=1",
		"WALLFACER_STATUS_MODE=json",
		"WALLFACER_STATUS_ADDR=http://127.0.0.1:1",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit code for unreachable server")
	}
	if !bytes.Contains(out, []byte("not reachable")) {
		t.Fatalf("expected 'not reachable' in output, got: %s", out)
	}
}

// TestPrintBoard_EmptyTasks verifies that printBoard handles an empty task list
// without panicking and still shows the total line.
func TestPrintBoard_EmptyTasks(t *testing.T) {
	output := captureStdout(func() {
		printBoard("http://localhost:8080", nil, nil)
	})
	if !bytes.Contains([]byte(output), []byte("Total: 0 tasks")) {
		t.Fatalf("expected total line with 0 tasks, got: %s", output)
	}
}

// TestPrintBoard_TitleFallbackToPrompt verifies that when a task has no title,
// the prompt is displayed instead.
func TestPrintBoard_TitleFallbackToPrompt(t *testing.T) {
	tasks := []taskSummary{
		{ID: "abcdef1234567890", Title: "", Prompt: "My prompt text", Status: "backlog", Turns: 0},
	}
	output := captureStdout(func() {
		printBoard("http://localhost:8080", tasks, nil)
	})
	if !bytes.Contains([]byte(output), []byte("My prompt text")) {
		t.Fatalf("expected prompt as fallback display, got: %s", output)
	}
}

// captureStdout redirects os.Stdout to a pipe, runs fn, and returns
// everything written to stdout as a string.
func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	defer func() {
		_ = r.Close()
	}()
	defer func() {
		_ = w.Close()
	}()
	defer func() { os.Stdout = old }()
	os.Stdout = w

	fn()

	_ = w.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// captureStderr redirects os.Stderr to a pipe, runs fn, and returns
// everything written to stderr as a string.
func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	defer func() {
		_ = r.Close()
	}()
	defer func() {
		_ = w.Close()
	}()
	defer func() { os.Stderr = old }()
	os.Stderr = w

	fn()

	_ = w.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}
