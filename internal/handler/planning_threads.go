package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/planner"
)

// threadsManager returns the planner's thread manager if both are
// configured, else nil.
func (h *Handler) threadsManager() *planner.ThreadManager {
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
func (h *Handler) lookupThreadStore(id string) *planner.ConversationStore {
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
}

func toThreadSummary(m planner.ThreadMeta, activeID string) threadSummary {
	return threadSummary{
		ID:       m.ID,
		Name:     m.Name,
		Created:  m.Created.UTC().Format(time.RFC3339Nano),
		Updated:  m.Updated.UTC().Format(time.RFC3339Nano),
		Archived: m.Archived,
		Active:   m.ID == activeID,
	}
}

// writeThreadErr maps a ThreadManager error to an HTTP status code.
func writeThreadErr(w http.ResponseWriter, err error) {
	if errors.Is(err, planner.ErrThreadNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}

// ListPlanningThreads returns the set of planning chat threads for the
// current workspace group. Pass ?includeArchived=true to include
// archived threads in the result.
func (h *Handler) ListPlanningThreads(w http.ResponseWriter, r *http.Request) {
	tm := h.threadsManager()
	if tm == nil {
		httpjson.Write(w, http.StatusOK, map[string]any{"threads": []threadSummary{}, "active_id": ""})
		return
	}
	includeArchived := r.URL.Query().Get("includeArchived") == "true"
	metas := tm.List(includeArchived)
	activeID := tm.ActiveID()
	out := make([]threadSummary, 0, len(metas))
	for _, m := range metas {
		out = append(out, toThreadSummary(m, activeID))
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"threads":   out,
		"active_id": activeID,
	})
}

// CreatePlanningThread creates a new planning chat thread. Body is
// optional; when `name` is empty, a default "Chat N" name is used.
func (h *Handler) CreatePlanningThread(w http.ResponseWriter, r *http.Request) {
	tm := h.threadsManager()
	if tm == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	req, ok := httpjson.DecodeOptionalBody[struct {
		Name string `json:"name"`
	}](w, r)
	if !ok {
		return
	}
	meta, err := tm.Create(strings.TrimSpace(req.Name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusCreated, toThreadSummary(meta, tm.ActiveID()))
}

// RenamePlanningThread renames a thread. Body: {"name": "New name"}.
func (h *Handler) RenamePlanningThread(w http.ResponseWriter, r *http.Request) {
	tm := h.threadsManager()
	if tm == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	req, ok := httpjson.DecodeBody[struct {
		Name string `json:"name"`
	}](w, r)
	if !ok {
		return
	}
	if err := tm.Rename(id, req.Name); err != nil {
		writeThreadErr(w, err)
		return
	}
	meta, err := tm.Meta(id)
	if err != nil {
		writeThreadErr(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, toThreadSummary(meta, tm.ActiveID()))
}

// ArchivePlanningThread hides a thread from the tab bar. The thread
// currently serving an in-flight exec cannot be archived (409).
func (h *Handler) ArchivePlanningThread(w http.ResponseWriter, r *http.Request) {
	tm := h.threadsManager()
	if tm == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if h.planner != nil && h.planner.BusyThreadID() == id {
		httpjson.Write(w, http.StatusConflict, map[string]any{
			"error": "thread is busy; interrupt or wait before archiving",
		})
		return
	}
	if err := tm.Archive(id); err != nil {
		writeThreadErr(w, err)
		return
	}
	meta, err := tm.Meta(id)
	if err != nil {
		writeThreadErr(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, toThreadSummary(meta, tm.ActiveID()))
}

// UnarchivePlanningThread restores a thread to the visible tab set.
func (h *Handler) UnarchivePlanningThread(w http.ResponseWriter, r *http.Request) {
	tm := h.threadsManager()
	if tm == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := tm.Unarchive(id); err != nil {
		writeThreadErr(w, err)
		return
	}
	meta, err := tm.Meta(id)
	if err != nil {
		writeThreadErr(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, toThreadSummary(meta, tm.ActiveID()))
}

// ActivatePlanningThread records a new active thread for the UI.
func (h *Handler) ActivatePlanningThread(w http.ResponseWriter, r *http.Request) {
	tm := h.threadsManager()
	if tm == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := tm.SetActiveID(id); err != nil {
		writeThreadErr(w, err)
		return
	}
	meta, err := tm.Meta(id)
	if err != nil {
		writeThreadErr(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, toThreadSummary(meta, tm.ActiveID()))
}
