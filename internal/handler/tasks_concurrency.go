package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"latere.ai/x/pkg/statemachine"

	"latere.ai/x/wallfacer/internal/store"
)

// transitionMu serializes the status transitions that start a run or a
// commit (start, feedback, resume, sync, done), so the concurrency check and
// the status write are atomic and two requests cannot both act on one task.
var transitionMu sync.Mutex

// maxConcurrentTasks returns the configured parallel task limit. Per-group
// overrides (Group.MaxParallel in workspace-groups.json) take precedence
// over the env-file default; a 0 override means "unlimited" for that group.
func (h *Handler) maxConcurrentTasks() int {
	if override, ok := h.currentGroupParallelLimit(); ok {
		return override
	}
	return h.cachedMaxParallel.Get()
}

// checkConcurrencyAndUpdateStatus acquires transitionMu, enforces the regular
// in-progress concurrency limit, and calls store.UpdateTaskStatus. It writes
// the appropriate HTTP error response and returns false on any failure;
// on success it returns true with the mutex already released.
func (h *Handler) checkConcurrencyAndUpdateStatus(ctx context.Context, w http.ResponseWriter, id uuid.UUID, newStatus store.TaskStatus) bool {
	transitionMu.Lock()
	defer transitionMu.Unlock()

	regularInProgress := h.countGlobalInProgress()
	if regularInProgress >= h.maxConcurrentTasks() {
		http.Error(w, fmt.Sprintf("max concurrent tasks (%d) reached", h.maxConcurrentTasks()), http.StatusConflict)
		return false
	}
	s, ok := h.requireStore(w)
	if !ok {
		return false
	}
	if err := s.UpdateTaskStatus(ctx, id, newStatus); err != nil {
		if errors.Is(err, statemachine.ErrInvalidTransition) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return false
	}
	return true
}
