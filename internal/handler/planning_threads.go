package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/agentsession"
)

// isTaskLockedByPlanner reports whether any task-mode planning thread currently
// has an in-flight turn pinned to taskID. Returns (true, threadID) when locked.
func (h *Handler) isTaskLockedByPlanner(taskID string) (bool, string) {
	if h.planner == nil {
		return false, ""
	}
	return h.planner.IsTaskLocked(taskID)
}

// cascadeArchiveThreadsForTask archives all non-archived task-mode threads
// pinned to taskID and sets their AutoArchivedByTaskLifecycle flag.
func (h *Handler) cascadeArchiveThreadsForTask(taskID string) {
	tm := h.threadsManager()
	if tm == nil {
		return
	}
	archived, err := tm.CascadeArchiveForTask(taskID)
	if err != nil {
		logger.Handler.Warn("cascade archive threads failed", "task", taskID, "err", err)
		return
	}
	if len(archived) > 0 {
		logger.Handler.Debug("cascade archived task-mode threads", "task", taskID, "threads", archived)
	}
}

// cascadeUnarchiveThreadsForTask reverses AutoArchivedByTaskLifecycle archiving
// for threads pinned to taskID (only those still carrying the cascade flag).
func (h *Handler) cascadeUnarchiveThreadsForTask(taskID string) {
	tm := h.threadsManager()
	if tm == nil {
		return
	}
	if err := tm.CascadeUnarchiveForTask(taskID); err != nil {
		logger.Handler.Warn("cascade unarchive threads failed", "task", taskID, "err", err)
	}
}

// threadsManager returns the planner's thread manager if both are
// configured, else nil.
func (h *Handler) threadsManager() *agentsession.ThreadManager {
	if h.planner == nil {
		return nil
	}
	return h.planner.Threads()
}

// threadIDFromRequest returns the thread ID from the `?thread=` query
// parameter, or the active thread's ID when none is supplied. Returns
// an empty string if no thread manager is configured or no thread is
// active.
func (h *Handler) threadIDFromRequest(r *http.Request) string {
	if q := strings.TrimSpace(r.URL.Query().Get("thread")); q != "" {
		return q
	}
	tm := h.threadsManager()
	if tm == nil {
		return ""
	}
	return tm.ActiveID()
}

// lookupThreadStore resolves a thread ID to its ConversationStore.
// Returns nil if the thread manager is not configured or the ID is
// empty / unknown (the latter so callers can return an empty response
// for "no thread yet" rather than a 404).
func (h *Handler) lookupThreadStore(id string) *agentsession.ConversationStore {
	tm := h.threadsManager()
	if tm == nil || id == "" {
		return nil
	}
	s, err := tm.Store(id)
	if err != nil {
		return nil
	}
	return s
}

// threadSummary is the JSON shape returned by thread CRUD handlers.
type threadSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Created  string `json:"created"`
	Updated  string `json:"updated"`
	Archived bool   `json:"archived"`
	Active   bool   `json:"active,omitempty"`
	Mode     string `json:"mode"`              // "spec" or "task"
	TaskID   string `json:"task_id,omitempty"` // set when Mode == "task"
}

func toThreadSummary(m agentsession.ThreadMeta, activeID, mode, taskID string) threadSummary {
	return threadSummary{
		ID:       m.ID,
		Name:     m.Name,
		Created:  m.Created.UTC().Format(time.RFC3339Nano),
		Updated:  m.Updated.UTC().Format(time.RFC3339Nano),
		Archived: m.Archived,
		Active:   m.ID == activeID,
		Mode:     mode,
		TaskID:   taskID,
	}
}

// threadMode derives the mode and task ID from a thread's session.
// Returns ("spec", "") when the session is absent or task-mode is not pinned.
func threadMode(tm *agentsession.ThreadManager, id string) (mode, taskID string) {
	cs, err := tm.Store(id)
	if err != nil {
		return "spec", ""
	}
	sess, err := cs.LoadSession()
	if err != nil {
		return "spec", ""
	}
	if sess.FocusedTask != "" {
		return "task", sess.FocusedTask
	}
	return "spec", ""
}

// writeThreadErr maps a ThreadManager error to an HTTP status code.
func writeThreadErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentsession.ErrThreadNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, agentsession.ErrThreadNotArchived):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// ListPlanningThreads returns the set of planning chat threads for the
// current workspace group. Pass ?includeArchived=true to include
// archived threads in the result.
func (h *Handler) ListPlanningThreads(w http.ResponseWriter, r *http.Request) {
	tm := h.threadsManager()
	// Hidden workspace (org-scoped, not visible to this principal): present no
	// threads, matching /api/config's "no workspace" state.
	if tm == nil || h.workspaceHiddenFromRequest(r) {
		httpjson.Write(w, http.StatusOK, map[string]any{"threads": []threadSummary{}, "active_id": ""})
		return
	}
	includeArchived := r.URL.Query().Get("includeArchived") == "true"
	metas := tm.List(includeArchived)
	activeID := tm.ActiveID()
	out := make([]threadSummary, 0, len(metas))
	for _, m := range metas {
		mode, taskID := threadMode(tm, m.ID)
		out = append(out, toThreadSummary(m, activeID, mode, taskID))
	}
	busyID := ""
	if h.planner != nil {
		busyID = h.planner.BusyThreadID()
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"threads":        out,
		"active_id":      activeID,
		"busy_thread_id": busyID,
	})
}

// CreatePlanningThread creates a new planning chat thread. Body is
// optional; when `name` is empty, a default "Chat N" name is used.
// When `focused_task` is set, the thread is pinned to task-mode immediately.
func (h *Handler) CreatePlanningThread(w http.ResponseWriter, r *http.Request) {
	if !h.requireVisibleWorkspace(w, r) {
		return
	}
	tm := h.threadsManager()
	if tm == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	req, ok := httpjson.DecodeOptionalBody[struct {
		Name        string `json:"name"`
		FocusedTask string `json:"focused_task"`
	}](w, r)
	if !ok {
		return
	}

	// Validate focused_task UUID and existence if provided.
	var taskIDStr string
	if ft := strings.TrimSpace(req.FocusedTask); ft != "" {
		taskUUID, parseErr := uuid.Parse(ft)
		if parseErr != nil {
			http.Error(w, "focused_task: invalid UUID", http.StatusBadRequest)
			return
		}
		if s, ok := h.currentStore(); ok {
			if _, lookupErr := s.GetTask(context.Background(), taskUUID); lookupErr != nil {
				http.Error(w, "focused_task: task not found", http.StatusNotFound)
				return
			}
		}
		taskIDStr = taskUUID.String()
	}

	meta, err := tm.Create(strings.TrimSpace(req.Name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Pin task-mode immediately by writing session.json before any exec.
	mode, taskID := "spec", ""
	if taskIDStr != "" {
		cs, csErr := tm.Store(meta.ID)
		if csErr == nil {
			_ = cs.SaveSession(agentsession.SessionInfo{FocusedTask: taskIDStr})
		}
		mode, taskID = "task", taskIDStr
	}

	httpjson.Write(w, http.StatusCreated, toThreadSummary(meta, tm.ActiveID(), mode, taskID))
}

// PatchPlanningThread mutates a single planning chat thread. The body
// carries exactly one mutation:
//
//	{"name": "New name"}     — rename the thread
//	{"state": "archived"}    — hide from the tab bar (409 if busy)
//	{"state": "visible"}     — restore from archive
//	{"state": "active"}      — set the UI's active thread
//
// All variants return the refreshed threadSummary.
func (h *Handler) PatchPlanningThread(w http.ResponseWriter, r *http.Request) {
	if !h.requireVisibleWorkspace(w, r) {
		return
	}
	tm := h.threadsManager()
	if tm == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	req, ok := httpjson.DecodeBody[struct {
		Name  *string `json:"name"`
		State string  `json:"state"`
	}](w, r)
	if !ok {
		return
	}

	if req.Name != nil {
		id := r.PathValue("id")
		if err := tm.Rename(id, *req.Name); err != nil {
			writeThreadErr(w, err)
			return
		}
		h.writeThreadSummary(w, tm, id)
		return
	}

	switch req.State {
	case "archived":
		h.mutatePlanningThread(w, r,
			"thread is busy; interrupt or wait before archiving",
			(*agentsession.ThreadManager).Archive,
		)
	case "visible":
		h.mutatePlanningThread(w, r, "", (*agentsession.ThreadManager).Unarchive)
	case "active":
		h.mutatePlanningThread(w, r, "", (*agentsession.ThreadManager).SetActiveID)
	default:
		http.Error(w, "body must set name or state (archived|visible|active)", http.StatusBadRequest)
	}
}

// DeletePlanningThread permanently removes an archived thread and its stored
// conversation. A visible thread must be archived first (409), and a thread
// with an in-flight turn cannot be deleted (409).
func (h *Handler) DeletePlanningThread(w http.ResponseWriter, r *http.Request) {
	if !h.requireVisibleWorkspace(w, r) {
		return
	}
	tm := h.threadsManager()
	if tm == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if h.planner != nil && h.planner.BusyThreadID() == id {
		httpjson.Write(w, http.StatusConflict, map[string]any{"error": "thread is busy; interrupt or wait before deleting"})
		return
	}
	if err := tm.Delete(id); err != nil {
		writeThreadErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeThreadSummary refreshes a thread's meta and writes its summary as
// a 200 response, mapping ThreadManager errors to the right status.
func (h *Handler) writeThreadSummary(w http.ResponseWriter, tm *agentsession.ThreadManager, id string) {
	meta, err := tm.Meta(id)
	if err != nil {
		writeThreadErr(w, err)
		return
	}
	mode, taskID := threadMode(tm, id)
	httpjson.Write(w, http.StatusOK, toThreadSummary(meta, tm.ActiveID(), mode, taskID))
}

// mutatePlanningThread is the shared scaffold for the single-verb
// planning-thread state mutations (archive, unarchive, activate)
// dispatched from PatchPlanningThread. Each caller supplies its own
// ThreadManager method; the require-configured, busy-check (archive
// only), apply, and response shape stay identical across the trio.
func (h *Handler) mutatePlanningThread(
	w http.ResponseWriter, r *http.Request,
	busyConflict string,
	apply func(*agentsession.ThreadManager, string) error,
) {
	tm := h.threadsManager()
	if tm == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if busyConflict != "" && h.planner != nil && h.planner.BusyThreadID() == id {
		httpjson.Write(w, http.StatusConflict, map[string]any{"error": busyConflict})
		return
	}
	if err := apply(tm, id); err != nil {
		writeThreadErr(w, err)
		return
	}
	h.writeThreadSummary(w, tm, id)
}
