package handler

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/store"
)

// systemIdeationTag identifies legacy ideation routine cards left
// behind by earlier code paths. The tag is retained as a lookup key so
// cleanupLegacyIdeationRoutine can find and delete them; new cards are
// never created with this tag.
const systemIdeationTag = "system:ideation"

// cleanupLegacyIdeationRoutine deletes any routine card tagged
// system:ideation from the supplied store. The hidden "always-on"
// ideation routine has been retired in favour of letting users create
// Kind=idea-agent tasks from the standard composer (optionally ticked
// as recurring). Without this cleanup, users upgrading from the earlier
// migration would keep a ghost routine firing in the background.
func (h *Handler) cleanupLegacyIdeationRoutine(ctx context.Context, s *store.Store) {
	if s == nil {
		return
	}
	tasks, err := s.ListTasks(ctx, true) // include archived so we don't miss any
	if err != nil {
		return
	}
	for _, t := range tasks {
		if !t.IsRoutine() || !slices.Contains(t.Tags, systemIdeationTag) {
			continue
		}
		if err := s.DeleteTask(ctx, t.ID, "legacy system:ideation routine removed"); err != nil {
			logger.Handler.Warn("ideation cleanup: delete routine", "routine", t.ID, "error", err)
			continue
		}
		logger.Handler.Info("ideation cleanup: removed legacy routine", "routine", t.ID)
	}
}

// TriggerIdeation handles POST /api/ideate.
// Creates a one-shot idea-agent task and kicks off its runner. This
// replaces the old toggle-based ideation scheduler — the task is a
// normal card that goes through the regular execute path, and users
// can make it recurring by creating it from the composer with the
// Repeat-on-a-schedule option.
func (h *Handler) TriggerIdeation(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	// Prevent runaway triggers: if an idea-agent task is already
	// in-flight we surface 409 so callers can show a stale-state hint
	// rather than queue a parallel run.
	if existing, _ := s.ListTasks(r.Context(), false); existing != nil {
		for _, t := range existing {
			if !t.IsIdeaAgent() {
				continue
			}
			if t.Status == store.TaskStatusBacklog || t.Status == store.TaskStatusInProgress {
				http.Error(w, "ideation task is already in flight", http.StatusConflict)
				return
			}
		}
	}

	task, err := s.CreateTaskWithOptions(r.Context(), store.TaskCreateOptions{
		Prompt:  strings.TrimSpace("Brainstorm improvement ideas for the current workspace."),
		Kind:    store.TaskKindIdeaAgent,
		Timeout: constants.IdeaAgentDefaultTimeout,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.UpdateTaskStatus(r.Context(), task.ID, store.TaskStatusInProgress); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.insertEventOrLog(r.Context(), task.ID, store.EventTypeStateChange,
		store.NewStateChangeData("", store.TaskStatusInProgress, store.TriggerUser, nil))
	h.runner.RunBackground(task.ID, task.Prompt, "", false)

	httpjson.Write(w, http.StatusAccepted, map[string]any{
		"queued":  true,
		"task_id": task.ID.String(),
	})
}

// CancelIdeation handles DELETE /api/ideate.
// Cancels any currently-running or backlogged idea-agent task. The
// endpoint is retained for CLI and back-compat; the Automation toggle
// that called it has been removed from the UI.
func (h *Handler) CancelIdeation(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	tasks, err := s.ListTasks(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cancelled := false
	for _, t := range tasks {
		if !t.IsIdeaAgent() {
			continue
		}
		switch t.Status {
		case store.TaskStatusInProgress:
			h.runner.KillContainer(t.ID)
			_ = s.UpdateTaskStatus(r.Context(), t.ID, store.TaskStatusCancelled)
			h.insertEventOrLog(r.Context(), t.ID, store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusCancelled, "", nil))
			cancelled = true
		case store.TaskStatusBacklog:
			_ = s.UpdateTaskStatus(r.Context(), t.ID, store.TaskStatusCancelled)
			h.insertEventOrLog(r.Context(), t.ID, store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusBacklog, store.TaskStatusCancelled, "", nil))
			cancelled = true
		}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

// GetIdeationStatus handles GET /api/ideate.
// Reports whether an idea-agent task is currently running. The "enabled"
// and "next_run_at" fields are retained in the response shape for
// clients that still read them, but they are always false / absent now
// that the always-on toggle is gone.
func (h *Handler) GetIdeationStatus(w http.ResponseWriter, r *http.Request) {
	running := false
	if s, ok := h.currentStore(); ok {
		tasks, _ := s.ListTasks(r.Context(), false)
		for _, t := range tasks {
			if t.IsIdeaAgent() && t.Status == store.TaskStatusInProgress {
				running = true
				break
			}
		}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"enabled": false,
		"running": running,
	})
}
