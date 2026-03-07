package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// StreamTasks streams the task list as SSE, pushing an update on every state change.
func (h *Handler) StreamTasks(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	includeArchived := r.URL.Query().Get("include_archived") == "true"

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	subID, ch := h.store.Subscribe()
	defer h.store.Unsubscribe(subID)

	send := func() bool {
		tasks, err := h.store.ListTasks(r.Context(), includeArchived)
		if err != nil {
			return false
		}
		if tasks == nil {
			tasks = []store.Task{}
		}
		data, err := json.Marshal(tasks)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send() {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if !send() {
				return
			}
		}
	}
}

// StreamLogs streams live container logs for an in-progress task, or serves
// saved turn outputs for tasks that are no longer running.
// When phase=impl is specified, serves only the implementation-phase turn files
// (up to task.TestRunStartTurn) so the UI can display impl and test outputs separately.
func (h *Handler) StreamLogs(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	// Implementation-phase logs: serve only the turns that belong to the
	// implementation agent (before the test run started).
	if r.URL.Query().Get("phase") == "impl" {
		h.serveStoredLogsUpTo(w, r, id, task.TestRunStartTurn)
		return
	}

	// Test-phase logs: serve only the turns that belong to the test agent
	// (after the implementation turns). Only meaningful for completed tasks
	// where the test agent has already run.
	if r.URL.Query().Get("phase") == "test" && task.Status != "in_progress" && task.Status != "committing" {
		h.serveStoredLogsFrom(w, r, id, task.TestRunStartTurn)
		return
	}

	if task.Status != "in_progress" && task.Status != "committing" {
		// Container is gone (--rm). Serve saved stderr from disk instead.
		h.serveStoredLogs(w, r, id)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Resolve the actual container name. ContainerName checks the in-memory map
	// first (populated when a container is launched), then falls back to scanning
	// all wallfacer containers by label — covering both the current slug-based
	// format and the legacy wallfacer-<uuid> format. If it still returns empty,
	// the container is already gone (race: task status not yet updated to done).
	containerName := h.runner.ContainerName(id)
	if containerName == "" {
		h.serveStoredLogs(w, r, id)
		return
	}
	cmd := exec.CommandContext(r.Context(), h.runner.Command(), "logs", "-f", "--tail", "100", containerName)

	// Merge container stdout and stderr.
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		http.Error(w, "failed to start log stream", http.StatusInternalServerError)
		return
	}

	// Close the write end once the subprocess exits.
	go func() {
		cmd.Wait()
		pw.Close()
	}()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Feed scanner output through a channel so we can interleave keepalives.
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			pr.Close()
			return
		case line, ok := <-lines:
			if !ok {
				return
			}
			if _, err := w.Write([]byte(line + "\n")); err != nil {
				pr.Close()
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := w.Write([]byte("\n")); err != nil {
				pr.Close()
				return
			}
			flusher.Flush()
		}
	}
}

// serveStoredLogs serves saved turn output for tasks no longer running.
func (h *Handler) serveStoredLogs(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	h.serveStoredLogsRange(w, r, id, 0, 0)
}

// serveStoredLogsUpTo serves saved turn files up to maxTurn (inclusive).
// If maxTurn is 0, all turn files are served.
func (h *Handler) serveStoredLogsUpTo(w http.ResponseWriter, r *http.Request, id uuid.UUID, maxTurn int) {
	h.serveStoredLogsRange(w, r, id, 0, maxTurn)
}

// serveStoredLogsFrom serves saved turn files after fromTurn (exclusive).
// If fromTurn is 0, all turn files are served.
func (h *Handler) serveStoredLogsFrom(w http.ResponseWriter, r *http.Request, id uuid.UUID, fromTurn int) {
	h.serveStoredLogsRange(w, r, id, fromTurn, 0)
}

// serveStoredLogsRange serves saved turn files in the range (fromTurn, maxTurn].
// fromTurn=0 means no lower bound; maxTurn=0 means no upper bound.
func (h *Handler) serveStoredLogsRange(w http.ResponseWriter, r *http.Request, id uuid.UUID, fromTurn, maxTurn int) {
	outputsDir := h.store.OutputsDir(id)
	entries, err := os.ReadDir(outputsDir)
	if err != nil {
		http.Error(w, "no logs saved for this task", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	wrote := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "turn-") {
			continue
		}
		if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".stderr.txt") {
			continue
		}
		turn := parseTurnNumber(name)
		if maxTurn > 0 && turn > maxTurn {
			continue
		}
		if fromTurn > 0 && turn <= fromTurn {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(outputsDir, name))
		if readErr != nil || len(strings.TrimSpace(string(content))) == 0 {
			continue
		}
		w.Write(content)
		fmt.Fprintln(w)
		wrote = true
	}
	if !wrote {
		fmt.Fprintln(w, "(no output saved for this task)")
	}
}

// parseTurnNumber extracts the numeric turn index from a file name like
// "turn-0001.json" or "turn-0001.stderr.txt". Returns 0 if not parseable.
func parseTurnNumber(name string) int {
	base := strings.TrimPrefix(name, "turn-")
	dotIdx := strings.IndexByte(base, '.')
	if dotIdx < 0 {
		return 0
	}
	n, _ := strconv.Atoi(base[:dotIdx])
	return n
}
