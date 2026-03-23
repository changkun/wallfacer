package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/logpipe"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// StreamTasks streams task changes as SSE with typed events.
//
// On first connect (no last_event_id) an initial "snapshot" event is sent
// containing the full task list (filtered by ?include_archived) with an SSE
// id: field set to the current delta sequence number.
//
// On reconnect the client passes the last received sequence via the
// ?last_event_id=<seq> query parameter or the Last-Event-ID HTTP header.
// If the store's replay buffer covers the gap, only missed delta events are
// replayed. If the gap is too old the handler falls back to a full snapshot.
//
// Every SSE event carries an "id:" field so browsers can resume automatically.
//
//	event: snapshot      — full task list (data: []Task JSON)
//	event: task-updated  — a single task was created or mutated (data: Task JSON)
//	event: task-deleted  — a task was deleted (data: {"id":"<uuid>"})
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

	// Subscribe BEFORE reading any state so we cannot miss events between the
	// snapshot/replay phase and the live loop.
	subID, ch := h.store.Subscribe()
	defer h.store.Unsubscribe(subID)

	// replayUpTo is the highest sequence number already written to the client.
	// Live channel items with Seq <= replayUpTo are skipped to avoid duplicates.
	var replayUpTo int64 = -1

	// Try delta replay when the client provides a previous event ID.
	lastEventIDStr := r.URL.Query().Get("last_event_id")
	if lastEventIDStr == "" {
		lastEventIDStr = r.Header.Get("Last-Event-ID")
	}

	didReplay := false
	if lastEventIDStr != "" {
		if seq, err := strconv.ParseInt(lastEventIDStr, 10, 64); err == nil {
			deltas, tooOld := h.store.DeltasSince(seq)
			if !tooOld {
				// Replay missed deltas; the client already has a consistent
				// base state so no snapshot is required.
				for _, d := range deltas {
					payload, encErr := marshalDeltaPayload(d.Value)
					if encErr != nil {
						continue
					}
					eventType := deltaEventType(d.Value)
					if _, werr := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", d.Seq, eventType, payload); werr != nil {
						return
					}
					replayUpTo = d.Seq
				}
				flusher.Flush()
				didReplay = true
			}
			// If tooOld == true, fall through to the full snapshot below.
		}
	}

	if !didReplay {
		// Send the initial full snapshot so the client can bootstrap its local
		// state. ListTasksAndSeq reads both the task list and the current
		// sequence under the same read lock to guarantee consistency.
		tasks, currentSeq, err := h.store.ListTasksAndSeq(r.Context(), includeArchived)
		if err != nil {
			return
		}
		if tasks == nil {
			tasks = []store.Task{}
		}
		snapshot, err := json.Marshal(tasks)
		if err != nil {
			return
		}
		replayUpTo = currentSeq
		if _, err := fmt.Fprintf(w, "id: %d\nevent: snapshot\ndata: %s\n\n", currentSeq, snapshot); err != nil {
			return
		}
		flusher.Flush()
	}

	keepalive := time.NewTicker(constants.SSEKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case delta, ok := <-ch:
			if !ok {
				return
			}
			// Skip deltas already covered by the replay or snapshot phase.
			if delta.Seq <= replayUpTo {
				continue
			}
			payload, err := marshalDeltaPayload(delta.Value)
			if err != nil {
				continue
			}
			eventType := deltaEventType(delta.Value)
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", delta.Seq, eventType, payload); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			// SSE heartbeat event — prevents proxies and OS-level TCP
			// idle timeouts from silently closing the connection. Sent as
			// a real "heartbeat" event (not a comment) so the browser's
			// EventSource dispatches it to JavaScript, allowing the client
			// to detect stale connections and trigger a recovery fetch.
			if _, err := fmt.Fprint(w, "event: heartbeat\ndata: \n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// deltaEventType returns the SSE event name for a TaskDelta.
func deltaEventType(d store.TaskDelta) string {
	if d.Deleted {
		return "task-deleted"
	}
	return "task-updated"
}

// marshalDeltaPayload encodes the SSE data payload for a TaskDelta.
func marshalDeltaPayload(d store.TaskDelta) ([]byte, error) {
	if d.Deleted {
		return json.Marshal(map[string]string{"id": d.Task.ID.String()})
	}
	return json.Marshal(d.Task)
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
	if r.URL.Query().Get("phase") == "test" && task.Status != store.TaskStatusInProgress && task.Status != store.TaskStatusCommitting {
		h.serveStoredLogsFrom(w, r, id, task.TestRunStartTurn)
		return
	}

	if task.Status != store.TaskStatusInProgress && task.Status != store.TaskStatusCommitting {
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
	p, err := logpipe.Start(cmd, logpipe.MergeStderr())
	if err != nil {
		http.Error(w, "failed to start log stream", http.StatusInternalServerError)
		return
	}
	streamLines(w, r, flusher, p)
}

// StreamRefineLogs streams live container logs for an active sandbox refinement run.
// If no refinement container is running, returns 204 No Content so the client
// knows the run has ended and should read the result from the task object.
func (h *Handler) StreamRefineLogs(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	containerName := h.runner.RefineContainerName(id)
	if containerName == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Do not pipe cmd.Stderr into the response: errors from the log command
	// itself (e.g. "no such container" when the container was already removed)
	// would be forwarded verbatim to the client. Stderr is discarded by logpipe.
	cmd := exec.CommandContext(r.Context(), h.runner.Command(), "logs", "-f", "--tail", "100", containerName)
	p, err := logpipe.Start(cmd)
	if err != nil {
		http.Error(w, "failed to start log stream", http.StatusInternalServerError)
		return
	}
	streamLines(w, r, flusher, p)
}

// streamLines writes lines from a logpipe to the HTTP response with periodic
// keepalive newlines. It blocks until the pipe is exhausted or the request
// context is cancelled.
func streamLines(w http.ResponseWriter, r *http.Request, flusher http.Flusher, p *logpipe.Pipe) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	keepalive := time.NewTicker(constants.SSEKeepaliveInterval)
	defer keepalive.Stop()
	defer p.Close()

	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-p.Lines():
			if !ok {
				return
			}
			if _, err := w.Write([]byte(line + "\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := w.Write([]byte("\n")); err != nil {
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
func (h *Handler) serveStoredLogsRange(w http.ResponseWriter, _ *http.Request, id uuid.UUID, fromTurn, maxTurn int) {
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
		if _, writeErr := w.Write(content); writeErr != nil {
			return
		}
		if _, writeErr := fmt.Fprintln(w); writeErr != nil {
			return
		}
		wrote = true
	}
	if !wrote {
		if _, err := fmt.Fprintln(w, "(no output saved for this task)"); err != nil {
			logger.Handler.Debug("output response write failed", "error", err)
		}
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
