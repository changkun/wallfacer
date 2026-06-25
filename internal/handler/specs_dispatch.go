package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/spec"
	"latere.ai/x/wallfacer/internal/store"
)

// SpecTransition is the unified entry point for the four spec lifecycle
// actions (POST /api/specs/transition). It reads the `action`
// discriminator from the request body and delegates to the existing
// per-action handler, which re-decodes the body for its own fields.
// Response envelopes stay per-action: dispatch/undispatch return
// per-spec arrays; archive/unarchive return a single
// specTransitionResponse.
func (h *Handler) SpecTransition(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpjson.Write(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var probe struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Reset the body so the delegated handler can decode its own fields.
	r.Body = io.NopCloser(bytes.NewReader(raw))

	switch probe.Action {
	case "dispatch":
		h.DispatchSpecs(w, r)
	case "undispatch":
		h.UndispatchSpecs(w, r)
	case "archive":
		h.ArchiveSpec(w, r)
	case "unarchive":
		h.UnarchiveSpec(w, r)
	case "validate":
		h.ValidateSpecTransition(w, r)
	case "migrate":
		h.MigrateSpec(w, r)
	default:
		http.Error(w, "action must be one of: dispatch, undispatch, archive, unarchive, validate, migrate", http.StatusBadRequest)
	}
}

type dispatchRequest struct {
	Action string   `json:"action,omitempty"` // discriminator from SpecTransition; ignored here
	Paths  []string `json:"paths"`
	Run    bool     `json:"run"`
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

// promoteTarget is a non-leaf spec marked validated as part of folder dispatch.
type promoteTarget struct {
	relPath string
	absPath string
}

// dispatchExpansion is the result of expanding requested paths into the
// concrete leaves to dispatch. A leaf path passes through unchanged; a
// non-leaf path expands to its subtree leaves and contributes its drafted
// non-leaf descendants to promote.
type dispatchExpansion struct {
	leafPaths  []string        // dedup'd leaf relPaths to dispatch
	promote    []promoteTarget // drafted non-leaves to mark validated
	fromFolder map[string]bool // leaf relPaths that came from a non-leaf expansion
	errs       []dispatchError // non-leaf expansion failures
	folderMode bool            // true if any requested path was a non-leaf
}

// expandDispatchPaths turns requested paths into the leaf paths to dispatch.
// Leaf paths (and unresolvable paths, which the validation loop reports) pass
// through. A non-leaf path must be validated first; it expands to its subtree
// leaves, and its drafted non-leaf descendants are collected for promotion to
// validated. Folder dispatch is atomic: the caller rejects the whole request
// if errs is non-empty.
func (h *Handler) expandDispatchPaths(workspaces []string, paths []string) dispatchExpansion {
	exp := dispatchExpansion{fromFolder: make(map[string]bool)}
	seen := make(map[string]bool)
	addLeaf := func(p string) {
		if !seen[p] {
			seen[p] = true
			exp.leafPaths = append(exp.leafPaths, p)
		}
	}
	promoted := make(map[string]bool)

	for _, p := range paths {
		absPath := findSpecFile(workspaces, p)
		if absPath == "" || spec.IsLeafPath(absPath) {
			// Not found or a leaf — let the validation loop handle it.
			addLeaf(p)
			continue
		}

		// Non-leaf: folder dispatch.
		exp.folderMode = true
		s, err := spec.ParseFile(absPath)
		if err != nil {
			exp.errs = append(exp.errs, dispatchError{p, fmt.Sprintf("parse error: %v", err)})
			continue
		}
		if s.Status != spec.StatusValidated {
			exp.errs = append(exp.errs, dispatchError{p,
				fmt.Sprintf("non-leaf spec status is %q — validate it before folder dispatch", s.Status)})
			continue
		}

		ws := findWorkspaceRoot(workspaces, absPath)
		tree, err := spec.BuildTree(filepath.Join(ws, "specs"))
		if err != nil {
			exp.errs = append(exp.errs, dispatchError{p, fmt.Sprintf("build spec tree: %v", err)})
			continue
		}
		leaves, nonLeaves := spec.SubtreeSpecs(tree, p)
		if len(leaves) == 0 {
			exp.errs = append(exp.errs, dispatchError{p, "no dispatchable leaves in subtree"})
			continue
		}
		for _, lf := range leaves {
			addLeaf(lf.Key)
			exp.fromFolder[lf.Key] = true
		}
		for _, nl := range nonLeaves {
			if nl.Value == nil || nl.Value.Status != spec.StatusDrafted || promoted[nl.Key] {
				continue
			}
			promoted[nl.Key] = true
			exp.promote = append(exp.promote, promoteTarget{
				relPath: nl.Key,
				absPath: findSpecFile(workspaces, nl.Key),
			})
		}
	}
	return exp
}

// DispatchSpecs creates board tasks from validated specs atomically.
// For each spec path, it reads and validates the spec, creates a task with
// the spec body as the prompt, and writes the task ID back to the spec's
// YAML frontmatter. Both task creation and frontmatter update succeed or
// both are rolled back.
func (h *Handler) DispatchSpecs(w http.ResponseWriter, r *http.Request) {
	if !h.requireVisibleWorkspace(w, r) {
		return
	}
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

	// Phase 0: Expand non-leaf paths into their subtree leaves (folder
	// dispatch). Leaf paths pass through unchanged.
	exp := h.expandDispatchPaths(workspaces, req.Paths)
	if exp.folderMode && len(exp.errs) > 0 {
		// Folder dispatch is atomic: reject the whole request, dispatch nothing.
		httpjson.Write(w, http.StatusBadRequest, map[string]any{
			"dispatched": []dispatchResult{},
			"errors":     exp.errs,
		})
		return
	}

	// Phase 1: Resolve and validate all specs.
	resolved := make([]resolvedSpec, 0, len(exp.leafPaths))
	errs := exp.errs

	// Track which paths are in this batch for intra-batch dependency resolution.
	batchPaths := make(map[string]int) // relPath → index in resolved
	for _, p := range exp.leafPaths {
		batchPaths[p] = -1 // placeholder, updated after validation
	}

	for _, relPath := range exp.leafPaths {
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

		// A directly-requested leaf must be validated. A leaf reached through
		// folder dispatch may still be drafted — the validated non-leaf root
		// blessed the subtree, and Part 1 promotes the leaf to validated on
		// dispatch. Other statuses (stale, complete, vague, archived) are never
		// dispatchable.
		okStatus := s.Status == spec.StatusValidated ||
			(exp.fromFolder[relPath] && s.Status == spec.StatusDrafted)
		if !okStatus {
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

	// Folder dispatch is atomic: if any expanded leaf failed validation,
	// reject the whole request and create nothing. Direct leaf dispatch keeps
	// its best-effort behavior (dispatch the valid specs, report the rest).
	if exp.folderMode && len(errs) > 0 {
		httpjson.Write(w, http.StatusBadRequest, map[string]any{
			"dispatched": []dispatchResult{},
			"errors":     errs,
		})
		return
	}

	if len(resolved) == 0 {
		httpjson.Write(w, http.StatusBadRequest, map[string]any{
			"dispatched": []dispatchResult{},
			"errors":     errs,
		})
		return
	}

	s, ok := h.requireStore(w)
	if !ok {
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
			// contribute no blocker edge to the resulting board task.
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

		task, err := s.CreateTaskWithOptions(r.Context(), store.TaskCreateOptions{
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
				_ = s.DeleteTask(r.Context(), createdTaskIDs[j], "dispatch rollback")
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
			"status":             string(spec.StatusValidated),
			"updated":            time.Now(),
		})
		if err != nil {
			// Rollback: delete all created tasks.
			for j := range resolved {
				_ = s.DeleteTask(r.Context(), createdTaskIDs[j], "dispatch rollback: frontmatter write failed")
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

	// Phase 4b: Promote drafted non-leaf ancestors in dispatched subtrees to
	// validated. Best-effort — the leaves are already dispatched; a failed
	// promotion is cosmetic and logged, not fatal.
	for _, pt := range exp.promote {
		if pt.absPath == "" {
			continue
		}
		if err := spec.UpdateFrontmatter(pt.absPath, map[string]any{
			"status":  string(spec.StatusValidated),
			"updated": time.Now(),
		}); err != nil {
			logger.Handler.Error("folder dispatch: failed to promote non-leaf to validated",
				"spec", pt.relPath, "error", err)
		}
	}

	// Phase 5: If run is true, transition tasks to in_progress.
	if req.Run {
		for i := range resolved {
			if err := s.UpdateTaskStatus(r.Context(), createdTaskIDs[i], store.TaskStatusInProgress); err != nil {
				// The store write did not persist; do not emit a state-change
				// event for a transition that never happened, and do not launch
				// a runner for a task the store still believes is in backlog.
				logger.Handler.Error("dispatch: failed to transition task to in_progress",
					"task", createdTaskIDs[i], "spec", resolved[i].relPath, "error", err)
				continue
			}
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
	Action string   `json:"action,omitempty"` // discriminator from SpecTransition; ignored here
	Paths  []string `json:"paths"`
}

type undispatchResult struct {
	SpecPath string `json:"spec_path"`
	TaskID   string `json:"task_id"`
}

// UndispatchSpecs cancels the board tasks linked to dispatched specs and
// clears each spec's dispatched_task_id, returning the spec to validated status.
func (h *Handler) UndispatchSpecs(w http.ResponseWriter, r *http.Request) {
	if !h.requireVisibleWorkspace(w, r) {
		return
	}
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

	st, ok := h.requireStore(w)
	if !ok {
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
		task, err := st.GetTask(r.Context(), taskID)
		if err == nil {
			// Task exists — cancel if not already done/cancelled.
			switch task.Status {
			case store.TaskStatusDone, store.TaskStatusCancelled:
				// Already terminal — skip cancellation.
			default:
				_ = st.CancelTask(r.Context(), taskID)
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
// path if found, or empty string if not found. Candidates that escape the
// workspace (e.g. via "../") are rejected so the spec endpoints cannot read or
// write files outside the workspace tree.
func findSpecFile(workspaces []string, relPath string) string {
	for _, ws := range workspaces {
		abs := filepath.Join(ws, relPath)
		rel, err := filepath.Rel(ws, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue // escapes the workspace
		}
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	return ""
}
