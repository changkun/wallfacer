package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxRebaseRetries = 3

type Runner struct {
	store        *Store
	command      string
	sandboxImage string
	envFile      string
	workspaces   string
	worktreesDir string // base dir for per-task worktrees, e.g. ~/.wallfacer/worktrees
}

type RunnerConfig struct {
	Command      string
	SandboxImage string
	EnvFile      string
	Workspaces   string
	WorktreesDir string
}

func NewRunner(store *Store, cfg RunnerConfig) *Runner {
	return &Runner{
		store:        store,
		command:      cfg.Command,
		sandboxImage: cfg.SandboxImage,
		envFile:      cfg.EnvFile,
		workspaces:   cfg.Workspaces,
		worktreesDir: cfg.WorktreesDir,
	}
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type claudeOutput struct {
	Result       string      `json:"result"`
	SessionID    string      `json:"session_id"`
	StopReason   string      `json:"stop_reason"`
	Subtype      string      `json:"subtype"`
	IsError      bool        `json:"is_error"`
	TotalCostUSD float64     `json:"total_cost_usd"`
	Usage        claudeUsage `json:"usage"`
}

func (r *Runner) Command() string {
	return r.command
}

// KillContainer sends a kill signal to the running container for a task.
// Safe to call when no container is running — errors are silently ignored.
func (r *Runner) KillContainer(taskID uuid.UUID) {
	containerName := "wallfacer-" + taskID.String()
	exec.Command(r.command, "kill", containerName).Run()
}

func (r *Runner) Workspaces() []string {
	if r.workspaces == "" {
		return nil
	}
	return strings.Fields(r.workspaces)
}

// setupWorktrees creates an isolated git worktree for each git-backed workspace.
// Non-git workspaces are skipped and will be mounted directly as before.
// Returns (worktreePaths, branchName, error).
// Idempotent: if the worktree directory already exists it is reused.
func (r *Runner) setupWorktrees(taskID uuid.UUID) (map[string]string, string, error) {
	branchName := "task/" + taskID.String()[:8]
	worktreePaths := make(map[string]string)

	for _, ws := range r.Workspaces() {
		if !isGitRepo(ws) {
			continue
		}

		basename := filepath.Base(ws)
		worktreePath := filepath.Join(r.worktreesDir, taskID.String(), basename)

		// Idempotent: reuse existing worktree (e.g. task resumed from waiting).
		if _, err := os.Stat(worktreePath); err == nil {
			worktreePaths[ws] = worktreePath
			continue
		}

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
			r.cleanupWorktrees(taskID, worktreePaths, branchName)
			return nil, "", fmt.Errorf("mkdir worktree parent: %w", err)
		}

		if err := createWorktree(ws, worktreePath, branchName); err != nil {
			r.cleanupWorktrees(taskID, worktreePaths, branchName)
			return nil, "", fmt.Errorf("createWorktree for %s: %w", ws, err)
		}

		worktreePaths[ws] = worktreePath
	}

	return worktreePaths, branchName, nil
}

// cleanupWorktrees removes all worktrees for a task and the task's worktree
// directory. Safe to call multiple times — errors are logged as warnings.
func (r *Runner) cleanupWorktrees(taskID uuid.UUID, worktreePaths map[string]string, branchName string) {
	for repoPath, wt := range worktreePaths {
		if err := removeWorktree(repoPath, wt, branchName); err != nil {
			logRunner.Warn("remove worktree", "task", taskID, "repo", repoPath, "error", err)
		}
	}
	taskWorktreeDir := filepath.Join(r.worktreesDir, taskID.String())
	if err := os.RemoveAll(taskWorktreeDir); err != nil {
		logRunner.Warn("remove worktree dir", "task", taskID, "error", err)
	}
}

// pruneOrphanedWorktrees scans worktreesDir for directories whose UUID does not
// match any known task, removes them, and runs `git worktree prune` on all
// git workspaces to clean up stale internal references.
func (r *Runner) pruneOrphanedWorktrees(store *Store) {
	entries, err := os.ReadDir(r.worktreesDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logRunner.Warn("read worktrees dir", "error", err)
		}
		return
	}

	ctx := context.Background()
	tasks, _ := store.ListTasks(ctx, true)
	knownIDs := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		knownIDs[t.ID.String()] = true
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if knownIDs[entry.Name()] {
			continue
		}
		orphanDir := filepath.Join(r.worktreesDir, entry.Name())
		logRunner.Warn("pruning orphaned worktree dir", "dir", orphanDir)
		os.RemoveAll(orphanDir)
	}

	// Run `git worktree prune` on all workspaces to clean stale references.
	for _, ws := range r.Workspaces() {
		if isGitRepo(ws) {
			exec.Command("git", "-C", ws, "worktree", "prune").Run()
		}
	}
}

func (r *Runner) Run(taskID uuid.UUID, prompt, sessionID string, resumedFromWaiting bool) {
	bgCtx := context.Background()

	task, err := r.store.GetTask(bgCtx, taskID)
	if err != nil {
		logRunner.Error("get task", "task", taskID, "error", err)
		return
	}

	// Apply per-task total timeout across all turns.
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(bgCtx, timeout)
	defer cancel()

	// Set up worktrees only if not already present.
	// WorktreePaths is set when the task first starts; preserved across
	// waiting→resumed transitions so the same worktree is reused.
	worktreePaths := task.WorktreePaths
	branchName := task.BranchName
	if len(worktreePaths) == 0 {
		worktreePaths, branchName, err = r.setupWorktrees(taskID)
		if err != nil {
			logRunner.Error("setup worktrees", "task", taskID, "error", err)
			r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
			r.store.UpdateTaskResult(bgCtx, taskID, err.Error(), sessionID, "", task.Turns)
			r.store.InsertEvent(bgCtx, taskID, "error", map[string]string{"error": err.Error()})
			r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
				"from": "in_progress", "to": "failed",
			})
			return
		}
		if err := r.store.UpdateTaskWorktrees(bgCtx, taskID, worktreePaths, branchName); err != nil {
			logRunner.Error("save worktree paths", "task", taskID, "error", err)
		}
	}

	turns := task.Turns

	for {
		turns++
		logRunner.Info("turn", "task", taskID, "turn", turns, "session", sessionID, "timeout", timeout)

		output, rawStdout, rawStderr, err := r.runContainer(ctx, taskID, prompt, sessionID, worktreePaths)
		if saveErr := r.store.SaveTurnOutput(taskID, turns, rawStdout, rawStderr); saveErr != nil {
			logRunner.Error("save turn output", "task", taskID, "turn", turns, "error", saveErr)
		}
		if err != nil {
			logRunner.Error("container error", "task", taskID, "error", err)
			// Don't overwrite a cancelled status — the cancel handler may have
			// killed the container and already transitioned the task.
			if cur, _ := r.store.GetTask(bgCtx, taskID); cur != nil && cur.Status == "cancelled" {
				return
			}
			r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
			r.store.UpdateTaskResult(bgCtx, taskID, err.Error(), sessionID, "", turns)
			r.store.InsertEvent(bgCtx, taskID, "error", map[string]string{"error": err.Error()})
			r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
				"from": "in_progress", "to": "failed",
			})
			return
		}

		r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
			"result":      output.Result,
			"stop_reason": output.StopReason,
			"session_id":  output.SessionID,
		})

		if output.SessionID != "" {
			sessionID = output.SessionID
		}
		r.store.UpdateTaskResult(bgCtx, taskID, output.Result, sessionID, output.StopReason, turns)
		r.store.AccumulateTaskUsage(bgCtx, taskID, TaskUsage{
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		})

		if output.IsError {
			r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
			r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
				"from": "in_progress", "to": "failed",
			})
			return
		}

		switch output.StopReason {
		case "end_turn":
			r.store.UpdateTaskStatus(bgCtx, taskID, "done")
			r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
				"from": "in_progress", "to": "done",
			})
			// Always commit+rebase+merge when a task completes.
			r.commit(ctx, taskID, sessionID, turns, worktreePaths, branchName)
			return

		case "max_tokens", "pause_turn":
			logRunner.Info("auto-continuing", "task", taskID, "stop_reason", output.StopReason)
			prompt = ""
			continue

		default:
			// Empty or unknown stop_reason — waiting for user feedback.
			// Do NOT clean up worktrees; task may resume.
			r.store.UpdateTaskStatus(bgCtx, taskID, "waiting")
			r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
				"from": "in_progress", "to": "waiting",
			})
			return
		}
	}
}

// Commit creates its own timeout context and runs the full commit pipeline
// (Claude commit → rebase → merge → PROGRESS.md) for a task.
func (r *Runner) Commit(taskID uuid.UUID, sessionID string) {
	task, err := r.store.GetTask(context.Background(), taskID)
	if err != nil {
		logRunner.Error("commit get task", "task", taskID, "error", err)
		return
	}
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r.commit(ctx, taskID, sessionID, task.Turns, task.WorktreePaths, task.BranchName)
}

// SyncWorktrees rebases all task worktrees onto the latest default branch
// without merging. On success the task is restored to prevStatus; on
// unrecoverable failure it is moved to "failed".
func (r *Runner) SyncWorktrees(taskID uuid.UUID, sessionID, prevStatus string) {
	bgCtx := context.Background()
	task, err := r.store.GetTask(bgCtx, taskID)
	if err != nil {
		logRunner.Error("sync: get task", "task", taskID, "error", err)
		return
	}

	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(bgCtx, timeout)
	defer cancel()

	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Syncing worktrees with latest changes on default branch...",
	})

	for repoPath, worktreePath := range task.WorktreePaths {
		defBranch, err := defaultBranch(repoPath)
		if err != nil {
			r.failSync(bgCtx, taskID, sessionID, task.Turns,
				fmt.Sprintf("defaultBranch for %s: %v", filepath.Base(repoPath), err))
			return
		}

		n, _ := commitsBehind(repoPath, worktreePath)
		if n == 0 {
			r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
				"result": fmt.Sprintf("%s is already up to date with %s.", filepath.Base(repoPath), defBranch),
			})
			continue
		}

		r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
			"result": fmt.Sprintf("Rebasing %s onto %s (%d new commit(s))...", filepath.Base(repoPath), defBranch, n),
		})

		var rebaseErr error
		for attempt := 1; attempt <= maxRebaseRetries; attempt++ {
			rebaseErr = rebaseOntoDefault(repoPath, worktreePath)
			if rebaseErr == nil {
				break
			}
			if attempt == maxRebaseRetries || !isConflictError(rebaseErr) {
				break
			}
			logRunner.Warn("sync rebase conflict, invoking resolver",
				"task", taskID, "repo", repoPath, "attempt", attempt)
			r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
				"result": fmt.Sprintf("Conflict in %s — running resolver (attempt %d/%d)...",
					filepath.Base(repoPath), attempt, maxRebaseRetries),
			})
			if resolveErr := r.resolveConflicts(ctx, taskID, repoPath, worktreePath, sessionID); resolveErr != nil {
				rebaseErr = fmt.Errorf("conflict resolution failed: %w", resolveErr)
				break
			}
		}

		if rebaseErr != nil {
			r.failSync(bgCtx, taskID, sessionID, task.Turns,
				fmt.Sprintf("sync failed for %s: %v", filepath.Base(repoPath), rebaseErr))
			return
		}

		r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
			"result": fmt.Sprintf("Successfully synced %s with %s.", filepath.Base(repoPath), defBranch),
		})
	}

	r.store.UpdateTaskStatus(bgCtx, taskID, prevStatus)
	r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
		"from": "in_progress",
		"to":   prevStatus,
	})
	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Sync complete. Worktrees are up to date with the default branch.",
	})
	logRunner.Info("sync completed", "task", taskID)
}

// failSync transitions a task to "failed" after a sync error.
func (r *Runner) failSync(ctx context.Context, taskID uuid.UUID, sessionID string, turns int, msg string) {
	logRunner.Error("sync failed", "task", taskID, "error", msg)
	r.store.InsertEvent(ctx, taskID, "error", map[string]string{"error": msg})
	r.store.UpdateTaskStatus(ctx, taskID, "failed")
	r.store.InsertEvent(ctx, taskID, "state_change", map[string]string{
		"from": "in_progress",
		"to":   "failed",
	})
	r.store.UpdateTaskResult(ctx, taskID, "Sync failed: "+msg, sessionID, "sync_failed", turns)
}

// hostStageAndCommit stages and commits all uncommitted changes in each
// worktree directly on the host. This replaces the broken container-based
// approach where the worktree's .git file references host-absolute paths
// that don't exist inside the container, causing all git commands to fail.
// Returns true if any new commits were created.
func (r *Runner) hostStageAndCommit(worktreePaths map[string]string, prompt string) bool {
	committed := false
	for repoPath, worktreePath := range worktreePaths {
		// Stage all changes.
		if out, err := exec.Command("git", "-C", worktreePath, "add", "-A").CombinedOutput(); err != nil {
			logRunner.Warn("host commit: git add -A", "repo", repoPath, "error", err, "output", string(out))
			continue
		}

		// Check if there are staged changes.
		out, _ := exec.Command("git", "-C", worktreePath, "status", "--porcelain").Output()
		if len(strings.TrimSpace(string(out))) == 0 {
			logRunner.Info("host commit: nothing to commit", "repo", repoPath)
			continue
		}

		// Create commit message from the task prompt.
		line := prompt
		if idx := strings.IndexByte(line, '\n'); idx >= 0 {
			line = line[:idx]
		}
		msg := "wallfacer: " + truncate(line, 72)
		if out, err := exec.Command("git", "-C", worktreePath, "commit", "-m", msg).CombinedOutput(); err != nil {
			logRunner.Warn("host commit: git commit", "repo", repoPath, "error", err, "output", string(out))
			continue
		}

		committed = true
		logRunner.Info("host commit: committed changes", "repo", repoPath)
	}
	return committed
}

// commit runs Phase 1 (host-side commit in worktree), Phase 2 (host-side
// rebase+merge), Phase 3 (PROGRESS.md), Phase 4 (worktree cleanup).
func (r *Runner) commit(ctx context.Context, taskID uuid.UUID, sessionID string, turns int, worktreePaths map[string]string, branchName string) {
	bgCtx := context.Background()
	logRunner.Info("auto-commit", "task", taskID, "session", sessionID)

	// Phase 1: stage and commit all uncommitted changes on the host.
	// This runs git directly in the worktree (not inside a container) because
	// worktree .git files reference host-absolute paths that don't exist in
	// containers — making container-side git commands fail silently.
	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Phase 1/4: Staging and committing changes...",
	})

	task, _ := r.store.GetTask(bgCtx, taskID)
	taskPrompt := ""
	if task != nil {
		taskPrompt = task.Prompt
	}
	r.hostStageAndCommit(worktreePaths, taskPrompt)

	// Phase 2: host-side rebase and merge for each git worktree.
	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Phase 2/4: Rebasing and merging into default branch...",
	})
	commitHashes, mergeErr := r.rebaseAndMerge(ctx, taskID, worktreePaths, branchName, sessionID)
	if mergeErr != nil {
		logRunner.Error("rebase/merge failed", "task", taskID, "error", mergeErr)
		r.store.InsertEvent(bgCtx, taskID, "error", map[string]string{
			"error": "rebase/merge failed: " + mergeErr.Error(),
		})
		// Worktree cleanup happens in rebaseAndMerge on unrecoverable failure.
		return
	}

	// Phase 3: persist commit hashes and write PROGRESS.md.
	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Phase 3/4: Updating PROGRESS.md...",
	})
	if len(commitHashes) > 0 {
		if err := r.store.UpdateTaskCommitHashes(bgCtx, taskID, commitHashes); err != nil {
			logRunner.Warn("save commit hashes", "task", taskID, "error", err)
		}
	}

	task, _ = r.store.GetTask(bgCtx, taskID)
	if task != nil {
		if err := r.writeProgressMD(task, commitHashes); err != nil {
			logRunner.Warn("write PROGRESS.md", "task", taskID, "error", err)
		}
	}

	// Phase 4: remove worktrees now that the branch has been merged.
	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Phase 4/4: Cleaning up worktrees...",
	})
	r.cleanupWorktrees(taskID, worktreePaths, branchName)

	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Commit pipeline completed.",
	})
	logRunner.Info("commit completed", "task", taskID)
}

// rebaseAndMerge performs the host-side git pipeline for all worktrees:
// rebase onto default branch (with conflict-resolution retries), ff-merge, collect hashes.
func (r *Runner) rebaseAndMerge(
	ctx context.Context,
	taskID uuid.UUID,
	worktreePaths map[string]string,
	branchName string,
	sessionID string,
) (map[string]string, error) {
	bgCtx := context.Background()
	commitHashes := make(map[string]string)

	for repoPath, worktreePath := range worktreePaths {
		logRunner.Info("rebase+merge", "task", taskID, "repo", repoPath)

		defBranch, err := defaultBranch(repoPath)
		if err != nil {
			return commitHashes, fmt.Errorf("defaultBranch for %s: %w", repoPath, err)
		}

		// Skip if there are no commits to merge.
		ahead, err := hasCommitsAheadOf(worktreePath, defBranch)
		if err != nil {
			logRunner.Warn("rev-list check", "task", taskID, "repo", repoPath, "error", err)
		}
		if !ahead {
			logRunner.Info("no commits to merge, skipping", "task", taskID, "repo", repoPath)
			r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
				"result": fmt.Sprintf("Skipping %s — no new commits to merge.", repoPath),
			})
			continue
		}

		// Rebase with conflict-resolution retry loop.
		var rebaseErr error
		for attempt := 1; attempt <= maxRebaseRetries; attempt++ {
			r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
				"result": fmt.Sprintf("Rebasing %s onto %s (attempt %d/%d)...", repoPath, defBranch, attempt, maxRebaseRetries),
			})

			rebaseErr = rebaseOntoDefault(repoPath, worktreePath)
			if rebaseErr == nil {
				break
			}

			if attempt == maxRebaseRetries {
				return commitHashes, fmt.Errorf(
					"rebase failed after %d attempts in %s: %w",
					maxRebaseRetries, repoPath, rebaseErr,
				)
			}

			// Only retry on conflict; surface other errors immediately.
			if !isConflictError(rebaseErr) {
				return commitHashes, fmt.Errorf("rebase %s: %w", repoPath, rebaseErr)
			}

			logRunner.Warn("rebase conflict, invoking resolver",
				"task", taskID, "repo", repoPath, "attempt", attempt)
			r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
				"result": fmt.Sprintf("Conflict in %s — running resolver (attempt %d)...", repoPath, attempt),
			})

			if resolveErr := r.resolveConflicts(ctx, taskID, repoPath, worktreePath, sessionID); resolveErr != nil {
				return commitHashes, fmt.Errorf("conflict resolution failed: %w", resolveErr)
			}
		}

		// Fast-forward merge into default branch.
		r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
			"result": fmt.Sprintf("Fast-forward merging %s into %s...", branchName, defBranch),
		})
		if err := ffMerge(repoPath, branchName); err != nil {
			return commitHashes, fmt.Errorf("ff-merge %s: %w", repoPath, err)
		}

		hash, err := getCommitHash(repoPath)
		if err != nil {
			logRunner.Warn("get commit hash", "task", taskID, "repo", repoPath, "error", err)
		} else {
			commitHashes[repoPath] = hash
			r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
				"result": fmt.Sprintf("Merged %s — commit %s", repoPath, hash[:8]),
			})
		}
	}

	return commitHashes, nil
}

// isConflictError reports whether err wraps ErrConflict.
func isConflictError(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrConflict.Error())
}

// resolveConflicts runs a Claude container session to resolve rebase conflicts
// in worktreePath. It resumes the task's original session so Claude retains
// full context of what it implemented and can correctly choose which side of
// each conflict to keep (its own work vs upstream changes).
func (r *Runner) resolveConflicts(
	ctx context.Context,
	taskID uuid.UUID,
	repoPath, worktreePath string,
	sessionID string,
) error {
	basename := filepath.Base(worktreePath)
	containerPath := "/workspace/" + basename

	prompt := fmt.Sprintf(
		"There are git rebase conflicts in %s that need to be resolved. "+
			"Run `git status` to see which files are conflicted. "+
			"For each conflicted file: read the file, understand both sides of the conflict, "+
			"resolve it by keeping the correct implementation while incorporating upstream changes, "+
			"then run `git add <file>` to mark it resolved. "+
			"Once ALL conflicts are resolved, run `git rebase --continue`. "+
			"Do NOT run `git commit` manually — only resolve conflicts and continue the rebase. "+
			"Report what conflicts you found and how you resolved each one.",
		containerPath,
	)

	// Mount only the conflicted worktree for this targeted fix.
	override := map[string]string{repoPath: worktreePath}

	// Resume the task's session so Claude has full context of its implementation
	// and can make informed decisions about which conflicting changes to keep.
	output, rawStdout, rawStderr, err := r.runContainer(ctx, taskID, prompt, sessionID, override)

	task, _ := r.store.GetTask(context.Background(), taskID)
	turns := 0
	if task != nil {
		turns = task.Turns + 1
	}
	r.store.SaveTurnOutput(taskID, turns, rawStdout, rawStderr)

	if err != nil {
		return fmt.Errorf("conflict resolver container: %w", err)
	}
	if output.IsError {
		return fmt.Errorf("conflict resolver reported error: %s", truncate(output.Result, 300))
	}

	r.store.InsertEvent(context.Background(), taskID, "output", map[string]string{
		"result": "Conflict resolver: " + truncate(output.Result, 500),
	})
	return nil
}

// writeProgressMD appends a structured entry to PROGRESS.md in each workspace
// root (the main working tree, not the task worktree), then commits it.
func (r *Runner) writeProgressMD(task *Task, commitHashes map[string]string) error {
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	result := "(no result recorded)"
	if task.Result != nil && *task.Result != "" {
		result = truncate(*task.Result, 1000)
	}

	for _, ws := range r.Workspaces() {
		hash := commitHashes[ws]
		if hash == "" {
			hash = "(no commit)"
		}

		entry := fmt.Sprintf(
			"\n## Task: %s\n\n**Date**: %s  \n**Branch**: %s  \n**Commit**: `%s`\n\n**Prompt**:\n> %s\n\n**Result**:\n%s\n\n---\n",
			task.ID.String()[:8],
			timestamp,
			task.BranchName,
			hash,
			strings.ReplaceAll(task.Prompt, "\n", "\n> "),
			result,
		)

		progressPath := filepath.Join(ws, "PROGRESS.md")

		// Ensure the file starts with a header if it doesn't exist yet.
		if _, err := os.Stat(progressPath); os.IsNotExist(err) {
			header := "# Progress Log\n\nRecords of completed tasks, problems encountered, and lessons learned.\n"
			if err := os.WriteFile(progressPath, []byte(header), 0644); err != nil {
				logRunner.Warn("create PROGRESS.md", "path", progressPath, "error", err)
				continue
			}
		}

		f, err := os.OpenFile(progressPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			logRunner.Warn("open PROGRESS.md", "path", progressPath, "error", err)
			continue
		}
		_, writeErr := f.WriteString(entry)
		f.Close()
		if writeErr != nil {
			logRunner.Warn("write PROGRESS.md", "path", progressPath, "error", writeErr)
			continue
		}

		// Commit the PROGRESS.md update to the main working tree.
		if isGitRepo(ws) {
			exec.Command("git", "-C", ws, "add", "PROGRESS.md").Run()
			exec.Command("git", "-C", ws, "commit", "-m",
				fmt.Sprintf("wallfacer: progress log for task %s", task.ID.String()[:8]),
			).Run()
		}
	}
	return nil
}

func (r *Runner) runContainer(ctx context.Context, taskID uuid.UUID, prompt, sessionID string, worktreeOverrides map[string]string) (*claudeOutput, []byte, []byte, error) {
	containerName := "wallfacer-" + taskID.String()

	// Remove any leftover container from a previous interrupted run.
	exec.Command(r.command, "rm", "-f", containerName).Run()

	args := []string{"run", "--rm", "--network=host", "--name", containerName}

	if r.envFile != "" {
		args = append(args, "--env-file", r.envFile)
	}

	// Mount claude config volume.
	args = append(args, "-v", "claude-config:/home/claude/.claude")

	// Mount workspaces, substituting per-task worktree paths where available.
	if r.workspaces != "" {
		for _, ws := range strings.Fields(r.workspaces) {
			ws = strings.TrimSpace(ws)
			if ws == "" {
				continue
			}
			hostPath := ws
			if wt, ok := worktreeOverrides[ws]; ok {
				hostPath = wt
			}
			parts := strings.Split(ws, "/")
			basename := parts[len(parts)-1]
			if basename == "" && len(parts) > 1 {
				basename = parts[len(parts)-2]
			}
			args = append(args, "-v", hostPath+":/workspace/"+basename+":z")
		}
	}

	args = append(args, "-w", "/workspace", r.sandboxImage)
	args = append(args, "-p", prompt, "--verbose", "--output-format", "stream-json")
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logRunner.Debug("exec", "cmd", r.command, "args", strings.Join(args, " "))
	runErr := cmd.Run()

	var output claudeOutput
	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		if runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("container exited with code %d: stderr=%s", exitErr.ExitCode(), stderr.String())
			}
			return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("exec container: %w", runErr)
		}
		return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("empty output from container")
	}

	// Claude Code may output a single JSON result or a stream of NDJSON events.
	// Try parsing the whole output first; if that fails scan from the end.
	parseErr := json.Unmarshal([]byte(raw), &output)
	if parseErr != nil {
		lines := strings.Split(raw, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			if err := json.Unmarshal([]byte(line), &output); err == nil {
				parseErr = nil
				break
			}
		}
	}

	if parseErr != nil {
		if runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("container exited with code %d: stderr=%s stdout=%s", exitErr.ExitCode(), stderr.String(), truncate(raw, 500))
			}
			return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("exec container: %w", runErr)
		}
		return nil, stdout.Bytes(), stderr.Bytes(), fmt.Errorf("parse output: %w (raw: %s)", parseErr, truncate(raw, 200))
	}

	// Claude Code may exit non-zero even when it produces a valid result.
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			logRunner.Warn("container exited non-zero but produced valid output", "task", taskID, "code", exitErr.ExitCode())
		} else {
			logRunner.Warn("container error but produced valid output", "task", taskID, "error", runErr)
		}
	}

	return &output, stdout.Bytes(), stderr.Bytes(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
