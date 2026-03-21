package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

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

func TestGroupByStatusEmpty(t *testing.T) {
	groups := groupByStatus(nil)
	if len(groups) != 0 {
		t.Errorf("expected empty map for nil input, got %v", groups)
	}
}

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
		{"αβγδε", 3, "αβγ…"},    // multi-byte rune handling
		{"αβγ", 3, "αβγ"},       // exact rune count, no ellipsis
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.n)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.n, got, tc.want)
		}
	}
}

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

func TestMatchContainersEmpty(t *testing.T) {
	result := matchContainers(nil)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

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
		runStatus("", []string{"--addr", ts.URL})
	})
	if !bytes.Contains([]byte(output), []byte("Done")) || !bytes.Contains([]byte(output), []byte("11111111")) {
		t.Fatalf("expected board output, got: %s", output)
	}

	jsonOutput := captureStdout(func() {
		runStatus("", []string{"--addr", ts.URL, "--json"})
	})
	if !bytes.Contains([]byte(jsonOutput), []byte("\"id\":\"11111111-1111-1111-1111-111111111111\"")) {
		t.Fatalf("expected raw JSON output, got: %s", jsonOutput)
	}
}

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	defer func() { _ = r.Close()
 }()
	defer func() { _ = w.Close()
 }()
	defer func() { os.Stdout = old }()
	os.Stdout = w

	fn()

	_ = w.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	defer func() { _ = r.Close()
 }()
	defer func() { _ = w.Close()
 }()
	defer func() { os.Stderr = old }()
	os.Stderr = w

	fn()

	_ = w.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}
