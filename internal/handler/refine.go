package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// StartRefinementRequest is the optional body for POST /api/tasks/{id}/refine.
type StartRefinementRequest struct {
	UserInstructions string `json:"user_instructions"`
}

// StartRefinement starts a sandbox-based refinement run for a backlog task.
// The sandbox agent explores the codebase and produces a detailed implementation spec.
// POST /api/tasks/{id}/refine
func (h *Handler) StartRefinement(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "backlog" {
		http.Error(w, "task is not in backlog", http.StatusBadRequest)
		return
	}

	var req StartRefinementRequest
	if r.ContentLength > 0 {
		// Body is optional; ignore decode errors (empty or malformed body → no instructions).
		decodeOptionalJSONBody(r, &req)
	}

	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "running",
		Source:    "runner",
	}
	if err := h.store.StartRefinementJobIfIdle(r.Context(), id, job); err != nil {
		if errors.Is(err, store.ErrRefinementAlreadyRunning) {
			http.Error(w, "refinement already running", http.StatusConflict)
			return
		}
		logger.Handler.Error("start refinement: start job", "task", id, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.runner.RunRefinementBackground(id, strings.TrimSpace(req.UserInstructions))

	updated, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, updated)
}

// CancelRefinement stops a running sandbox refinement by killing the container.
// DELETE /api/tasks/{id}/refine
func (h *Handler) CancelRefinement(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.CurrentRefinement == nil || task.CurrentRefinement.Status != "running" {
		http.Error(w, "no refinement running", http.StatusBadRequest)
		return
	}

	h.runner.KillRefineContainer(id)

	// Mark as failed (cancelled).
	task.CurrentRefinement.Status = "failed"
	task.CurrentRefinement.Error = "cancelled by user"
	if err := h.store.UpdateRefinementJob(r.Context(), id, task.CurrentRefinement); err != nil {
		logger.Handler.Error("cancel refinement: update job", "task", id, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	updated, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// RefineDissmiss clears a completed refinement result without applying it.
// The task prompt remains unchanged; the CurrentRefinement field is cleared so
// the task can be started normally again.
// POST /api/tasks/{id}/refine/dismiss
func (h *Handler) RefineDismiss(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "backlog" {
		http.Error(w, "task is not in backlog", http.StatusBadRequest)
		return
	}
	if task.CurrentRefinement == nil || task.CurrentRefinement.Status != "done" {
		http.Error(w, "no completed refinement to dismiss", http.StatusBadRequest)
		return
	}

	if err := h.store.DismissRefinement(r.Context(), id); err != nil {
		logger.Handler.Error("dismiss refinement", "task", id, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	updated, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// RefineApplyRequest is the body for POST /api/tasks/{id}/refine/apply.
type RefineApplyRequest struct {
	Prompt string `json:"prompt"`
}

// RefineApply persists the refined prompt, recording the session in
// RefineSessions and moving the old prompt to PromptHistory.
// POST /api/tasks/{id}/refine/apply
func (h *Handler) RefineApply(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "backlog" {
		http.Error(w, "task is not in backlog", http.StatusBadRequest)
		return
	}

	var req RefineApplyRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	// Build a session recording what the sandbox produced vs what the user applied.
	sandboxResult := ""
	if task.CurrentRefinement != nil {
		sandboxResult = task.CurrentRefinement.Result
	}
	session := store.RefinementSession{
		ID:          uuid.New().String(),
		CreatedAt:   time.Now(),
		StartPrompt: task.Prompt,
		Result:      sandboxResult,
	}

	if err := h.store.ApplyRefinement(r.Context(), id, req.Prompt, session); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Regenerate title for the updated prompt.
	h.runner.GenerateTitleBackground(id, req.Prompt)

	updated, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
