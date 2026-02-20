package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type Handler struct {
	store      *Store
	runner     *Runner
	configDir  string
	workspaces []string
}

func NewHandler(store *Store, runner *Runner, configDir string, workspaces []string) *Handler {
	return &Handler{store: store, runner: runner, configDir: configDir, workspaces: workspaces}
}

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"workspaces":        h.runner.Workspaces(),
		"instructions_path": instructionsFilePath(h.configDir, h.workspaces),
	})
}

func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	tasks, err := h.store.ListTasks(r.Context(), includeArchived)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []Task{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt  string `json:"prompt"`
		Timeout int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	task, err := h.store.CreateTask(r.Context(), req.Prompt, req.Timeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), task.ID, "state_change", map[string]string{
		"to": "backlog",
	})

	go h.runner.GenerateTitle(task.ID, task.Prompt)

	writeJSON(w, http.StatusCreated, task)
}

func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Status     *string `json:"status"`
		Position   *int    `json:"position"`
		Prompt     *string `json:"prompt"`
		Timeout    *int    `json:"timeout"`
		FreshStart *bool   `json:"fresh_start"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	// Allow editing prompt, timeout, and fresh_start for backlog tasks.
	if task.Status == "backlog" && (req.Prompt != nil || req.Timeout != nil || req.FreshStart != nil) {
		if err := h.store.UpdateTaskBacklog(r.Context(), id, req.Prompt, req.Timeout, req.FreshStart); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.Position != nil {
		if err := h.store.UpdateTaskPosition(r.Context(), id, *req.Position); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.Status != nil {
		oldStatus := task.Status
		newStatus := *req.Status

		// Handle retry: done/failed → backlog
		if newStatus == "backlog" && (oldStatus == "done" || oldStatus == "failed") {
			newPrompt := task.Prompt
			if req.Prompt != nil {
				newPrompt = *req.Prompt
			}
			if err := h.store.ResetTaskForRetry(r.Context(), id, newPrompt); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
				"from": oldStatus,
				"to":   "backlog",
			})
		} else {
			if err := h.store.UpdateTaskStatus(r.Context(), id, newStatus); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
				"from": oldStatus,
				"to":   newStatus,
			})

			if newStatus == "in_progress" && oldStatus == "backlog" {
				sessionID := ""
				if !task.FreshStart && task.SessionID != nil {
					sessionID = *task.SessionID
				}
				go h.runner.Run(id, task.Prompt, sessionID, false)
			}
		}
	}

	updated, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) DeleteTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	// Clean up any worktrees before removing the task record.
	if task, err := h.store.GetTask(r.Context(), id); err == nil && len(task.WorktreePaths) > 0 {
		h.runner.cleanupWorktrees(id, task.WorktreePaths, task.BranchName)
	}
	if err := h.store.DeleteTask(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SubmitFeedback(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "waiting" {
		http.Error(w, "task is not in waiting status", http.StatusBadRequest)
		return
	}

	if err := h.store.UpdateTaskStatus(r.Context(), id, "in_progress"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), id, "feedback", map[string]string{
		"message": req.Message,
	})
	h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
		"from": "waiting",
		"to":   "in_progress",
	})

	sessionID := ""
	if task.SessionID != nil {
		sessionID = *task.SessionID
	}
	go h.runner.Run(id, req.Message, sessionID, true)

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (h *Handler) GetEvents(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	events, err := h.store.GetEvents(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []TaskEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

func (h *Handler) CompleteTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "waiting" {
		http.Error(w, "only waiting tasks can be completed", http.StatusBadRequest)
		return
	}

	if task.SessionID != nil && *task.SessionID != "" {
		// Transition to "committing" while auto-commit runs in the background.
		// The goroutine will move the task to "done" when finished.
		if err := h.store.UpdateTaskStatus(r.Context(), id, "committing"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
			"from": "waiting",
			"to":   "committing",
		})
		sessionID := *task.SessionID
		go func() {
			h.runner.Commit(id, sessionID)
			bgCtx := context.Background()
			h.store.UpdateTaskStatus(bgCtx, id, "done")
			h.store.InsertEvent(bgCtx, id, "state_change", map[string]string{
				"from": "committing",
				"to":   "done",
			})
		}()
	} else {
		// No session to commit — go directly to done.
		if err := h.store.UpdateTaskStatus(r.Context(), id, "done"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
			"from": "waiting",
			"to":   "done",
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) CancelTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	cancellable := map[string]bool{
		"backlog":     true,
		"in_progress": true,
		"waiting":     true,
		"failed":      true,
	}
	if !cancellable[task.Status] {
		http.Error(w, "task cannot be cancelled in its current status", http.StatusBadRequest)
		return
	}

	oldStatus := task.Status

	// For in_progress tasks: kill the running container first so the goroutine
	// exits cleanly. The goroutine checks for cancelled status before setting
	// failed, so it will not overwrite the transition we make below.
	if oldStatus == "in_progress" {
		h.runner.KillContainer(id)
	}

	// Clean up worktrees — discard all changes prepared so far.
	// Traces, events, and turn outputs are intentionally left intact so the
	// task history is preserved (useful if the task is later restored to backlog).
	if len(task.WorktreePaths) > 0 {
		h.runner.cleanupWorktrees(id, task.WorktreePaths, task.BranchName)
	}

	if err := h.store.UpdateTaskStatus(r.Context(), id, "cancelled"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
		"from": oldStatus,
		"to":   "cancelled",
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *Handler) ResumeTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "failed" {
		http.Error(w, "only failed tasks can be resumed", http.StatusBadRequest)
		return
	}
	if task.SessionID == nil || *task.SessionID == "" {
		http.Error(w, "task has no session to resume", http.StatusBadRequest)
		return
	}

	if err := h.store.ResumeTask(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
		"from": "failed",
		"to":   "in_progress",
	})

	go h.runner.Run(id, "continue", *task.SessionID, false)

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (h *Handler) ServeOutput(w http.ResponseWriter, r *http.Request, id uuid.UUID, filename string) {
	// Validate filename to prevent path traversal.
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	path := filepath.Join(h.store.dir, id.String(), "outputs", filename)
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if strings.HasSuffix(filename, ".json") {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	http.ServeFile(w, r, path)
}

func (h *Handler) StreamLogs(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
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

	containerName := "wallfacer-" + id.String()
	cmd := exec.CommandContext(r.Context(), h.runner.Command(), "logs", "-f", "--tail", "100", containerName)

	// Merge container stdout and stderr: podman logs writes container stdout to
	// its stdout and container stderr to its stderr. Claude Code emits live
	// progress (tool calls, etc.) to container stderr, so we must capture both.
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		http.Error(w, "failed to start log stream", http.StatusInternalServerError)
		return
	}

	// Close the write end once the subprocess exits so the scanner terminates.
	go func() {
		cmd.Wait()
		pw.Close()
	}()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := scanner.Text()
		_, werr := w.Write([]byte(line + "\n"))
		if werr != nil {
			break
		}
		flusher.Flush()
	}
	pr.Close()
}

func (h *Handler) ArchiveTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "done" {
		http.Error(w, "only done tasks can be archived", http.StatusBadRequest)
		return
	}
	if err := h.store.SetTaskArchived(r.Context(), id, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
		"to": "archived",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "archived"})
}

func (h *Handler) UnarchiveTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if _, err := h.store.GetTask(r.Context(), id); err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if err := h.store.SetTaskArchived(r.Context(), id, false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
		"to": "unarchived",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "unarchived"})
}

func (h *Handler) GenerateMissingTitles(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}

	tasks, err := h.store.ListTasks(r.Context(), true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var untitled []Task
	for _, t := range tasks {
		if t.Title == "" {
			untitled = append(untitled, t)
		}
	}

	total := len(untitled)
	if limit > 0 && len(untitled) > limit {
		untitled = untitled[:limit]
	}

	for _, t := range untitled {
		go h.runner.GenerateTitle(t.ID, t.Prompt)
	}

	writeJSON(w, http.StatusOK, map[string]int{
		"queued":              len(untitled),
		"total_without_title": total,
	})
}

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

	subID, ch := h.store.subscribe()
	defer h.store.unsubscribe(subID)

	send := func() bool {
		tasks, err := h.store.ListTasks(r.Context(), includeArchived)
		if err != nil {
			return false
		}
		if tasks == nil {
			tasks = []Task{}
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

// serveStoredLogs serves the saved turn output (raw NDJSON + sandbox stderr)
// for tasks that are no longer running (container removed with --rm so live
// logs are unavailable). Entries are served in alphabetical order so each
// turn's .json events are followed by its .stderr.txt sandbox trace.
// The frontend handles all rendering (pretty and raw modes).
func (h *Handler) serveStoredLogs(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	outputsDir := filepath.Join(h.store.dir, id.String(), "outputs")
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
		// Include both NDJSON turn output and sandbox stderr traces.
		if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".stderr.txt") {
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

func (h *Handler) TaskDiff(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if len(task.WorktreePaths) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"diff": "", "behind_counts": map[string]int{}})
		return
	}

	var combined strings.Builder
	behindCounts := make(map[string]int)
	for repoPath, worktreePath := range task.WorktreePaths {
		defBranch, err := defaultBranch(repoPath)
		if err != nil {
			continue
		}
		// Show all changes in worktree vs default branch (committed + uncommitted).
		out, _ := exec.CommandContext(r.Context(), "git", "-C", worktreePath, "diff", defBranch).Output()
		if len(out) > 0 {
			if len(task.WorktreePaths) > 1 {
				fmt.Fprintf(&combined, "=== %s ===\n", filepath.Base(repoPath))
			}
			combined.Write(out)
		}
		// Count commits the default branch has that the task branch does not.
		if n, err := commitsBehind(repoPath, worktreePath); err == nil && n > 0 {
			behindCounts[filepath.Base(repoPath)] = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"diff":          combined.String(),
		"behind_counts": behindCounts,
	})
}

func (h *Handler) SyncTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "waiting" && task.Status != "failed" {
		http.Error(w, "only waiting or failed tasks with worktrees can be synced", http.StatusBadRequest)
		return
	}
	if len(task.WorktreePaths) == 0 {
		http.Error(w, "task has no worktrees to sync", http.StatusBadRequest)
		return
	}

	oldStatus := task.Status
	if err := h.store.UpdateTaskStatus(r.Context(), id, "in_progress"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
		"from": oldStatus,
		"to":   "in_progress",
	})

	sessionID := ""
	if task.SessionID != nil {
		sessionID = *task.SessionID
	}
	go h.runner.SyncWorktrees(id, sessionID, oldStatus)
	writeJSON(w, http.StatusOK, map[string]string{"status": "syncing"})
}

func (h *Handler) GetInstructions(w http.ResponseWriter, r *http.Request) {
	path := instructionsFilePath(h.configDir, h.workspaces)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]string{"content": ""})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": string(content)})
}

func (h *Handler) UpdateInstructions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	path := instructionsFilePath(h.configDir, h.workspaces)
	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ReinitInstructions(w http.ResponseWriter, r *http.Request) {
	path, err := reinitWorkspaceInstructions(h.configDir, h.workspaces)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": string(content)})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logHandler.Error("write json", "error", err)
	}
}
