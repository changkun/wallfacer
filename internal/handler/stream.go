package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/pkg/livelog"
	"latere.ai/x/wallfacer/internal/pkg/sse"
	"latere.ai/x/wallfacer/internal/store"
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
	// Capture the store once so that all operations in this handler (subscribe,
	// delta replay, snapshot) use the same workspace store. Without this, a
	// workspace switch mid-handler could cause Subscribe to attach to workspace A
	// while ListTasksAndSeq reads from workspace B, leaking cross-group tasks.
	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	stream := sse.NewWriter(w)
	if stream == nil {
		return
	}

	includeArchived := r.URL.Query().Get("include_archived") == "true"

	// Subscribe BEFORE reading any state so we cannot miss events between the
	// snapshot/replay phase and the live loop.
	subID, ch := s.Subscribe()
	defer s.Unsubscribe(subID)

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
			deltas, tooOld := s.DeltasSince(seq)
			if !tooOld {
				// Replay missed deltas; the client already has a consistent
				// base state so no snapshot is required.
				for _, d := range deltas {
					payload, encErr := marshalDeltaPayload(d.Value)
					if encErr != nil {
						continue
					}
					if err := stream.EventID(strconv.FormatInt(d.Seq, 10), deltaEventType(d.Value), payload); err != nil {
						return
					}
					replayUpTo = d.Seq
				}
				didReplay = true
			}
			// If tooOld == true, fall through to the full snapshot below.
		}
	}

	if !didReplay {
		// Send the initial full snapshot so the client can bootstrap its local
		// state. ListTasksAndSeq reads both the task list and the current
		// sequence under the same read lock to guarantee consistency.
		tasks, currentSeq, err := s.ListTasksAndSeq(r.Context(), includeArchived)
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
		if err := stream.EventID(strconv.FormatInt(currentSeq, 10), "snapshot", snapshot); err != nil {
			return
		}
		// Include cross-group task counts in the initial payload.
		if err := stream.JSON("active_groups", h.activeGroupInfos(r.Context())); err != nil {
			return
		}
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
			if err := stream.EventID(strconv.FormatInt(delta.Seq, 10), deltaEventType(delta.Value), payload); err != nil {
				return
			}
			// Emit cross-group task counts so the frontend can update
			// workspace tab badges for background groups in real time.
			if err := stream.JSON("active_groups", h.activeGroupInfos(r.Context())); err != nil {
				return
			}
		case <-keepalive.C:
			// SSE heartbeat event — prevents proxies and OS-level TCP
			// idle timeouts from silently closing the connection. Sent as
			// a real "heartbeat" event (not a comment) so the browser's
			// EventSource dispatches it to JavaScript, allowing the client
			// to detect stale connections and trigger a recovery fetch.
			if err := stream.Heartbeat(); err != nil {
				return
			}
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
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	task, err := s.GetTask(r.Context(), id)
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

	// Prefer the in-process live log reader for the running turn; otherwise
	// serve the saved turn outputs from disk. (Host processes stream through
	// the live reader; there is no container log to shell out to.)
	if lr := h.runner.TaskLogReader(id); lr != nil {
		h.streamLiveLog(w, r, flusher, id, lr)
		return
	}
	h.serveStoredLogs(w, r, id)
}

// streamLiveLog streams output from the in-process live log buffer for the
// currently running turn. It first sends any stored turn outputs (completed
// turns from earlier in the execution), then streams the live turn data.
func (h *Handler) streamLiveLog(w http.ResponseWriter, r *http.Request, flusher http.Flusher, id uuid.UUID, lr *livelog.Reader) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	// Send previously completed turns from disk so the client has the
	// full history (not just the current turn).
	h.writeStoredTurns(w, id)
	flusher.Flush()

	// Stream the current turn's live output.
	relayLiveChunks(w, flusher, r, lr)
}

// chunkReader is the minimal reader the live-chunk relay needs. Both the live
// log reader and the planning log reader satisfy it.
type chunkReader interface {
	ReadChunk(ctx context.Context) ([]byte, error)
}

// relayLiveChunks streams chunks from lr to w as text/plain until the reader
// ends or the request context is cancelled, emitting keepalive newlines while
// idle. The producer goroutine's send is guarded by the request context so a
// disconnected client with a full channel buffer cannot strand it forever.
func relayLiveChunks(w http.ResponseWriter, flusher http.Flusher, r *http.Request, lr chunkReader) {
	ch := make(chan []byte, 4)
	go pumpChunks(r.Context(), lr, ch)

	keepalive := time.NewTicker(constants.SSEKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(data); err != nil {
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

// pumpChunks reads chunks from lr and forwards them to ch until the reader
// ends or ctx is cancelled, closing ch on exit. The send is guarded by ctx so
// a cancelled (disconnected) consumer with a full buffer cannot strand this
// goroutine — ReadChunk is otherwise the only place that observes ctx, and it
// is not reached while the producer is blocked on a full channel.
func pumpChunks(ctx context.Context, lr chunkReader, ch chan<- []byte) {
	defer close(ch)
	for {
		data, err := lr.ReadChunk(ctx)
		if len(data) > 0 {
			select {
			case ch <- data:
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// writeStoredTurns writes all previously stored turn outputs to w.
// It is a non-failing best-effort helper — errors are silently ignored.
func (h *Handler) writeStoredTurns(w http.ResponseWriter, id uuid.UUID) {
	s, ok := h.currentStore()
	if !ok {
		return
	}
	keys, err := s.ListBlobs(id, "outputs/turn-")
	if err != nil {
		return
	}
	for _, key := range keys {
		name := filepath.Base(key)
		if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".stderr.txt") {
			continue
		}
		content, readErr := s.ReadBlob(id, key)
		if readErr != nil || len(strings.TrimSpace(string(content))) == 0 {
			continue
		}
		_, _ = w.Write(content)
		_, _ = fmt.Fprintln(w)
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
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	keys, err := s.ListBlobs(id, "outputs/turn-")
	if err != nil {
		http.Error(w, "no logs saved for this task", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	wrote := false
	for _, key := range keys {
		name := filepath.Base(key)
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
		content, readErr := s.ReadBlob(id, key)
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
