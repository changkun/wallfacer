package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/spec"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

type dispatchRequest struct {
	Paths []string `json:"paths"`
	Run   bool     `json:"run"`
}

type dispatchResult struct {
	SpecPath string `json:"spec_path"`
	TaskID   string `json:"task_id"`
}

type dispatchError struct {
	SpecPath string `json:"spec_path"`
	Error    string `json:"error"`
}

// resolvedSpec holds a parsed spec along with its absolute filesystem path.
type resolvedSpec struct {
	spec     *spec.Spec
	absPath  string
	relPath  string   // original path from request (e.g. "specs/local/foo.md")
	taskDeps []string // resolved task dependency UUIDs
}

// DispatchSpecs creates kanban tasks from validated specs atomically.
// For each spec path, it reads and validates the spec, creates a task with
// the spec body as the prompt, and writes the task ID back to the spec's
// YAML frontmatter. Both task creation and frontmatter update succeed or
// both are rolled back.
func (h *Handler) DispatchSpecs(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[dispatchRequest](w, r)
	if !ok {
		return
	}

	if len(req.Paths) == 0 {
		http.Error(w, "paths must not be empty", http.StatusBadRequest)
		return
	}

	workspaces := h.currentWorkspaces()
	if len(workspaces) == 0 {
		http.Error(w, "no workspaces configured", http.StatusInternalServerError)
		return
	}

	// Phase 1: Resolve and validate all specs.
	resolved := make([]resolvedSpec, 0, len(req.Paths))
	var errs []dispatchError

	// Track which paths are in this batch for intra-batch dependency resolution.
	batchPaths := make(map[string]int) // relPath → index in resolved
	for _, p := range req.Paths {
		batchPaths[p] = -1 // placeholder, updated after validation
	}

	for _, relPath := range req.Paths {
		absPath := findSpecFile(workspaces, relPath)
		if absPath == "" {
			errs = append(errs, dispatchError{relPath, "spec file not found in any workspace"})
			continue
		}

		s, err := spec.ParseFile(absPath)
		if err != nil {
			errs = append(errs, dispatchError{relPath, fmt.Sprintf("parse error: %v", err)})
			continue
		}

		if s.Status != spec.StatusValidated {
			msg := fmt.Sprintf("spec status is %q, must be %q", s.Status, spec.StatusValidated)
			if s.Status == spec.StatusArchived {
				msg = fmt.Sprintf("spec status is %q — unarchive the spec first before dispatching", s.Status)
			}
			errs = append(errs, dispatchError{relPath, msg})
			continue
		}

		if s.DispatchedTaskID != nil {
			errs = append(errs, dispatchError{relPath, "spec already dispatched (dispatched_task_id is set)"})
			continue
		}

		if !spec.IsLeafPath(absPath) {
			errs = append(errs, dispatchError{relPath, "non-leaf specs cannot be dispatched (break down into child specs first)"})
			continue
		}

		batchPaths[relPath] = len(resolved)
		resolved = append(resolved, resolvedSpec{
			spec:    s,
			absPath: absPath,
			relPath: relPath,
		})
	}

	if len(resolved) == 0 {
		httpjson.Write(w, http.StatusBadRequest, map[string]any{
			"dispatched": []dispatchResult{},
			"errors":     errs,
		})
		return
	}

	// Phase 2: Resolve dependencies.
	// For each spec's depends_on, check if the dependency is in this batch
	// (use pre-assigned UUID) or already dispatched (use existing task ID).
	preAssignedIDs := make([]uuid.UUID, len(resolved))
	for i := range resolved {
		preAssignedIDs[i] = uuid.New()
	}

	for i, rs := range resolved {
		var deps []string
		for _, depPath := range rs.spec.DependsOn {
			// Check if dependency is in the current batch.
			if idx, ok := batchPaths[depPath]; ok && idx >= 0 {
				deps = append(deps, preAssignedIDs[idx].String())
				continue
			}
			// Check if dependency spec is already dispatched.
			depAbsPath := findSpecFile(workspaces, depPath)
			if depAbsPath == "" {
				continue // dependency not found, skip
			}
			depSpec, err := spec.ParseFile(depAbsPath)
			if err != nil {
				continue
			}
			// Archived dependencies are considered already satisfied — they
			// contribute no blocker edge to the resulting kanban task.
			if depSpec.Status == spec.StatusArchived {
				continue
			}
			if depSpec.DispatchedTaskID != nil {
				deps = append(deps, *depSpec.DispatchedTaskID)
			}
			// If dependency exists but isn't dispatched, no blocker is added.
		}
		resolved[i].taskDeps = deps
	}

	// Phase 3: Create tasks.
	// Simple sequential creation (topological sort is not strictly needed since
	// dependencies are resolved by pre-assigned UUIDs, not creation order).
	createdTaskIDs := make([]uuid.UUID, len(resolved))
	for i, rs := range resolved {
		tags := []string{"spec-dispatched"}
		if rs.spec.Track != "" {
			tags = append(tags, rs.spec.Track)
		}

		task, err := h.store.CreateTaskWithOptions(r.Context(), store.TaskCreateOptions{
			ID:             preAssignedIDs[i],
			Prompt:         rs.spec.Body,
			Timeout:        60,
			Tags:           tags,
			SpecSourcePath: rs.relPath,
			DependsOn:      rs.taskDeps,
		})
		if err != nil {
			// Rollback: delete any tasks created so far.
			for j := 0; j < i; j++ {
				_ = h.store.DeleteTask(r.Context(), createdTaskIDs[j], "dispatch rollback")
			}
			http.Error(w, fmt.Sprintf("create task for %s: %v", rs.relPath, err), http.StatusInternalServerError)
			return
		}

		createdTaskIDs[i] = task.ID

		h.insertEventOrLog(r.Context(), task.ID, store.EventTypeStateChange,
			store.NewStateChangeData("", store.TaskStatusBacklog, store.TriggerUser, nil))
		h.runner.GenerateTitleBackground(task.ID, task.Prompt)
	}

	// Phase 4: Write dispatched_task_id back to spec files.
	for i, rs := range resolved {
		taskIDStr := createdTaskIDs[i].String()
		err := spec.UpdateFrontmatter(rs.absPath, map[string]any{
			"dispatched_task_id": &taskIDStr,
			"updated":            time.Now(),
		})
		if err != nil {
			// Rollback: delete all created tasks.
			for j := range resolved {
				_ = h.store.DeleteTask(r.Context(), createdTaskIDs[j], "dispatch rollback: frontmatter write failed")
			}
			// Attempt to revert any frontmatter already written.
			for j := 0; j < i; j++ {
				_ = spec.UpdateFrontmatter(resolved[j].absPath, map[string]any{
					"dispatched_task_id": nil,
				})
			}
			http.Error(w, fmt.Sprintf("write dispatched_task_id to %s: %v", rs.relPath, err), http.StatusInternalServerError)
			return
		}
	}

	// Phase 5: If run is true, transition tasks to in_progress.
	if req.Run {
		for i := range resolved {
			_ = h.store.UpdateTaskStatus(r.Context(), createdTaskIDs[i], store.TaskStatusInProgress)
			h.insertEventOrLog(r.Context(), createdTaskIDs[i], store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusBacklog, store.TaskStatusInProgress, store.TriggerUser, nil))
			h.runner.RunBackground(createdTaskIDs[i], resolved[i].spec.Body, "", false)
		}
	}

	// Build response.
	dispatched := make([]dispatchResult, len(resolved))
	for i, rs := range resolved {
		dispatched[i] = dispatchResult{
			SpecPath: rs.relPath,
			TaskID:   createdTaskIDs[i].String(),
		}
	}

	httpjson.Write(w, http.StatusCreated, map[string]any{
		"dispatched": dispatched,
		"errors":     errs,
	})
}

type undispatchRequest struct {
	Paths []string `json:"paths"`
}

type undispatchResult struct {
	SpecPath string `json:"spec_path"`
	TaskID   string `json:"task_id"`
}

// UndispatchSpecs cancels the kanban tasks linked to dispatched specs and
// clears each spec's dispatched_task_id, returning the spec to validated status.
func (h *Handler) UndispatchSpecs(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[undispatchRequest](w, r)
	if !ok {
		return
	}

	if len(req.Paths) == 0 {
		http.Error(w, "paths must not be empty", http.StatusBadRequest)
		return
	}

	workspaces := h.currentWorkspaces()
	if len(workspaces) == 0 {
		http.Error(w, "no workspaces configured", http.StatusInternalServerError)
		return
	}

	var results []undispatchResult
	var errs []dispatchError

	for _, relPath := range req.Paths {
		absPath := findSpecFile(workspaces, relPath)
		if absPath == "" {
			errs = append(errs, dispatchError{relPath, "spec file not found in any workspace"})
			continue
		}

		s, err := spec.ParseFile(absPath)
		if err != nil {
			errs = append(errs, dispatchError{relPath, fmt.Sprintf("parse error: %v", err)})
			continue
		}

		if s.DispatchedTaskID == nil {
			errs = append(errs, dispatchError{relPath, "spec is not dispatched (dispatched_task_id is null)"})
			continue
		}

		taskIDStr := *s.DispatchedTaskID
		taskID, err := uuid.Parse(taskIDStr)
		if err != nil {
			errs = append(errs, dispatchError{relPath, fmt.Sprintf("invalid dispatched_task_id: %v", err)})
			continue
		}

		// Cancel the task if it's in a cancellable state.
		task, err := h.store.GetTask(r.Context(), taskID)
		if err == nil {
			// Task exists — cancel if not already done/cancelled.
			switch task.Status {
			case store.TaskStatusDone, store.TaskStatusCancelled:
				// Already terminal — skip cancellation.
			default:
				_ = h.store.CancelTask(r.Context(), taskID)
				h.insertEventOrLog(r.Context(), taskID, store.EventTypeStateChange,
					store.NewStateChangeData(task.Status, store.TaskStatusCancelled, store.TriggerUser, nil))
			}
		}
		// If task not found, still clear the spec linkage.

		// Clear the spec's dispatch linkage.
		err = spec.UpdateFrontmatter(absPath, map[string]any{
			"dispatched_task_id": nil,
			"status":             string(spec.StatusValidated),
			"updated":            time.Now(),
		})
		if err != nil {
			errs = append(errs, dispatchError{relPath, fmt.Sprintf("update frontmatter: %v", err)})
			continue
		}

		results = append(results, undispatchResult{
			SpecPath: relPath,
			TaskID:   taskIDStr,
		})
	}

	if len(results) == 0 && len(errs) > 0 {
		httpjson.Write(w, http.StatusBadRequest, map[string]any{
			"undispatched": []undispatchResult{},
			"errors":       errs,
		})
		return
	}

	httpjson.Write(w, http.StatusOK, map[string]any{
		"undispatched": results,
		"errors":       errs,
	})
}

// SpecCompletionHook returns a callback suitable for store.OnDone that updates
// the source spec's status to "complete" when a dispatched task finishes.
// workspaceFn is called each time to get the current workspace list (workspaces
// can change at runtime via the Settings UI).
func SpecCompletionHook(workspaceFn func() []string) func(store.Task) {
	return func(task store.Task) {
		if task.SpecSourcePath == "" {
			return
		}
		workspaces := workspaceFn()
		absPath := findSpecFile(workspaces, task.SpecSourcePath)
		if absPath == "" {
			logger.Store.Warn("spec completion hook: spec file not found",
				"task", task.ID, "spec", task.SpecSourcePath)
			return
		}
		err := spec.UpdateFrontmatter(absPath, map[string]any{
			"status":  string(spec.StatusComplete),
			"updated": time.Now(),
		})
		if err != nil {
			logger.Store.Error("spec completion hook: failed to update frontmatter",
				"task", task.ID, "spec", task.SpecSourcePath, "error", err)
			return
		}
		logger.Store.Info("spec completion hook: marked spec complete",
			"task", task.ID, "spec", task.SpecSourcePath)
	}
}

// findSpecFile locates a spec file across workspaces. The relPath is relative
// to the workspace root (e.g. "specs/local/foo.md"). Returns the absolute
// path if found, or empty string if not found.
func findSpecFile(workspaces []string, relPath string) string {
	for _, ws := range workspaces {
		abs := filepath.Join(ws, relPath)
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	return ""
}
