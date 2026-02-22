package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/gitutil"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// Commit creates its own timeout context and runs the full commit pipeline
// (stage → rebase → merge → cleanup) for a task.
// Returns an error if any phase of the pipeline fails.
func (r *Runner) Commit(taskID uuid.UUID, sessionID string) error {
	task, err := r.store.GetTask(context.Background(), taskID)
	if err != nil {
		logger.Runner.Error("commit get task", "task", taskID, "error", err)
		return fmt.Errorf("get task: %w", err)
	}
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = defaultTaskTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return r.commit(ctx, taskID, sessionID, task.Turns, task.WorktreePaths, task.BranchName)
}

// commit runs Phase 1 (host-side commit in worktree), Phase 2 (host-side
// rebase+merge), Phase 3 (worktree cleanup).
// Returns an error if the rebase/merge phase fails.
func (r *Runner) commit(
	ctx context.Context,
	taskID uuid.UUID,
	sessionID string,
	turns int,
	worktreePaths map[string]string,
	branchName string,
) error {
	bgCtx := context.Background()
	logger.Runner.Info("auto-commit", "task", taskID, "session", sessionID)

	// Phase 1: stage and commit all uncommitted changes on the host.
	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": "Phase 1/3: Staging and committing changes...",
	})
	task, _ := r.store.GetTask(bgCtx, taskID)
	taskPrompt := ""
	if task != nil {
		taskPrompt = task.Prompt
	}
	if _, stageErr := r.hostStageAndCommit(taskID, worktreePaths, taskPrompt); stageErr != nil {
		logger.Runner.Error("host stage/commit failed", "task", taskID, "error", stageErr)
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{
			"error": "stage/commit failed: " + stageErr.Error(),
		})
		return fmt.Errorf("stage and commit: %w", stageErr)
	}

	// Phase 2: host-side rebase and merge for each git worktree.
	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": "Phase 2/3: Rebasing and merging into default branch...",
	})
	commitHashes, baseHashes, mergeErr := r.rebaseAndMerge(ctx, taskID, worktreePaths, branchName, sessionID)
	if mergeErr != nil {
		logger.Runner.Error("rebase/merge failed", "task", taskID, "error", mergeErr)
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{
			"error": "rebase/merge failed: " + mergeErr.Error(),
		})
		return fmt.Errorf("rebase/merge: %w", mergeErr)
	}

	// Phase 3: persist commit hashes and clean up worktrees.
	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": "Phase 3/3: Cleaning up...",
	})
	if len(commitHashes) > 0 {
		if err := r.store.UpdateTaskCommitHashes(bgCtx, taskID, commitHashes); err != nil {
			logger.Runner.Warn("save commit hashes", "task", taskID, "error", err)
		}
	}
	if len(baseHashes) > 0 {
		if err := r.store.UpdateTaskBaseCommitHashes(bgCtx, taskID, baseHashes); err != nil {
			logger.Runner.Warn("save base commit hashes", "task", taskID, "error", err)
		}
	}
	r.cleanupWorktrees(taskID, worktreePaths, branchName)

	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": "Commit pipeline completed.",
	})
	logger.Runner.Info("commit completed", "task", taskID)
	return nil
}

// hostStageAndCommit stages and commits all uncommitted changes in each
// worktree directly on the host. Returns true if any new commits were created.
// Returns an error if changes were present but could not be staged or committed.
func (r *Runner) hostStageAndCommit(taskID uuid.UUID, worktreePaths map[string]string, prompt string) (bool, error) {
	// First pass: stage all changes and collect diff stats for each worktree
	// that has pending changes.
	type pendingCommit struct {
		repoPath     string
		worktreePath string
		diffStat     string
		recentLog    string
	}
	var pending []pendingCommit
	var errs []string

	for repoPath, worktreePath := range worktreePaths {
		if out, err := exec.Command("git", "-C", worktreePath, "add", "-A").CombinedOutput(); err != nil {
			logger.Runner.Warn("host commit: git add -A", "repo", repoPath, "error", err, "output", string(out))
			errs = append(errs, fmt.Sprintf("git add in %s: %v", repoPath, err))
			continue
		}

		out, _ := exec.Command("git", "-C", worktreePath, "status", "--porcelain").Output()
		if len(strings.TrimSpace(string(out))) == 0 {
			logger.Runner.Info("host commit: nothing to commit", "repo", repoPath)
			continue
		}

		statOut, _ := exec.Command("git", "-C", worktreePath, "diff", "--cached", "--stat").Output()
		logOut, _ := exec.Command("git", "-C", worktreePath, "log", "--format=%s", "-5").Output()
		pending = append(pending, pendingCommit{repoPath, worktreePath, strings.TrimSpace(string(statOut)), strings.TrimSpace(string(logOut))})
	}

	if len(pending) == 0 {
		if len(errs) > 0 {
			return false, fmt.Errorf("staging failed: %s", strings.Join(errs, "; "))
		}
		return false, nil
	}

	// Build combined diff stat and git log context across all worktrees, then
	// generate a descriptive commit message via a lightweight Claude container.
	var allStats strings.Builder
	var allLogs strings.Builder
	for _, p := range pending {
		if len(pending) > 1 {
			allStats.WriteString("Repository: " + p.repoPath + "\n")
			allLogs.WriteString("Repository: " + p.repoPath + "\n")
		}
		allStats.WriteString(p.diffStat + "\n")
		if p.recentLog != "" {
			allLogs.WriteString(p.recentLog + "\n")
		}
	}
	msg := r.generateCommitMessage(taskID, prompt, allStats.String(), allLogs.String())

	// Second pass: commit each worktree with the generated message.
	// Use global git identity to prevent sandbox-set local configs from
	// overriding the host user's author information.
	var gitConfigOverrides []string
	if out, err := exec.Command("git", "config", "--global", "user.name").Output(); err == nil {
		if n := strings.TrimSpace(string(out)); n != "" {
			gitConfigOverrides = append(gitConfigOverrides, "-c", "user.name="+n)
		}
	}
	if out, err := exec.Command("git", "config", "--global", "user.email").Output(); err == nil {
		if e := strings.TrimSpace(string(out)); e != "" {
			gitConfigOverrides = append(gitConfigOverrides, "-c", "user.email="+e)
		}
	}

	committed := false
	for _, p := range pending {
		args := append([]string{"-C", p.worktreePath}, gitConfigOverrides...)
		args = append(args, "commit", "-m", msg)
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			logger.Runner.Warn("host commit: git commit", "repo", p.repoPath, "error", err, "output", string(out))
			errs = append(errs, fmt.Sprintf("git commit in %s: %v", p.repoPath, err))
			continue
		}
		committed = true
		logger.Runner.Info("host commit: committed changes", "repo", p.repoPath)
	}

	if !committed && len(errs) > 0 {
		return false, fmt.Errorf("commit failed: %s", strings.Join(errs, "; "))
	}
	return committed, nil
}

// generateCommitMessage runs a lightweight container to produce a descriptive
// git commit message from the task prompt, staged diff stats, and recent git
// log history (used to match the project's commit style).
// Falls back to a truncated prompt on any error.
func (r *Runner) generateCommitMessage(taskID uuid.UUID, prompt, diffStat, recentLog string) string {
	firstLine := prompt
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	fallback := "wallfacer: " + truncate(firstLine, 72)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	containerName := "wallfacer-commit-" + taskID.String()[:8]
	exec.Command(r.command, "rm", "-f", containerName).Run()

	args := []string{"run", "--rm", "--network=host", "--name", containerName}
	if r.envFile != "" {
		args = append(args, "--env-file", r.envFile)
	}
	args = append(args, "-v", "claude-config:/home/claude/.claude")
	args = append(args, r.sandboxImage)

	commitPrompt := "Write a git commit message for the following task and file changes.\n" +
		"Rules:\n" +
		"- Format: <primary-path>: <short imperative description>\n" +
		"  where <primary-path> is the common directory or file prefix of the changed files\n" +
		"  (e.g. 'content/posts', 'Makefile', 'internal/runner', 'ui/js')\n" +
		"- Single line only — no body, no blank lines\n" +
		"- Max 72 characters total, no trailing period\n" +
		"- Output ONLY the raw commit message text, no markdown, no code fences, no explanation\n" +
		"- Match the style and tone of the recent commit history shown below\n\n" +
		"Task:\n" + prompt + "\n\n" +
		"Changed files:\n" + diffStat
	if recentLog != "" {
		commitPrompt += "\nRecent commits (for style reference):\n" + recentLog
	}
	args = append(args, "-p", commitPrompt, "--output-format", "stream-json", "--verbose")

	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		logger.Runner.Warn("commit message generation failed", "task", taskID, "error", err,
			"stderr", truncate(stderr.String(), 200))
		return fallback
	}

	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		logger.Runner.Warn("commit message generation: empty output", "task", taskID)
		return fallback
	}

	output, err := parseOutput(raw)
	if err != nil {
		logger.Runner.Warn("commit message generation: parse failure", "task", taskID, "raw", truncate(raw, 200))
		return fallback
	}

	msg := strings.TrimSpace(output.Result)
	msg = strings.Trim(msg, "`")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		logger.Runner.Warn("commit message generation: blank result", "task", taskID)
		return fallback
	}

	return msg
}

// rebaseAndMerge performs the host-side git pipeline for all worktrees:
// rebase onto default branch (with conflict-resolution retries), ff-merge, collect hashes.
// Returns (commitHashes, baseHashes, error).
func (r *Runner) rebaseAndMerge(
	ctx context.Context,
	taskID uuid.UUID,
	worktreePaths map[string]string,
	branchName string,
	sessionID string,
) (map[string]string, map[string]string, error) {
	bgCtx := context.Background()
	commitHashes := make(map[string]string)
	baseHashes := make(map[string]string)

	for repoPath, worktreePath := range worktreePaths {
		logger.Runner.Info("rebase+merge", "task", taskID, "repo", repoPath)

		// Serialize rebase+merge per repo so concurrent tasks on the same
		// repo don't race (the second task sees the first task's merge
		// before rebasing). Tasks on different repos remain fully concurrent.
		mu := r.repoLock(repoPath)
		mu.Lock()

		err := r.rebaseAndMergeOne(ctx, taskID, repoPath, worktreePath, branchName, sessionID, bgCtx, commitHashes, baseHashes)
		mu.Unlock()
		if err != nil {
			return commitHashes, baseHashes, err
		}
	}

	return commitHashes, baseHashes, nil
}

// rebaseAndMergeOne handles the rebase+merge pipeline for a single repo/worktree pair.
// Extracted so the caller can hold/release the per-repo lock cleanly.
func (r *Runner) rebaseAndMergeOne(
	ctx context.Context,
	taskID uuid.UUID,
	repoPath, worktreePath, branchName, sessionID string,
	bgCtx context.Context,
	commitHashes, baseHashes map[string]string,
) error {
	if !gitutil.IsGitRepo(repoPath) {
		// Non-git workspace: copy snapshot changes back to the original directory.
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Extracting changes from sandbox to %s...", filepath.Base(repoPath)),
		})
		if err := extractSnapshotToWorkspace(worktreePath, repoPath); err != nil {
			return fmt.Errorf("extract snapshot for %s: %w", repoPath, err)
		}
		if hash, err := gitutil.GetCommitHash(worktreePath); err == nil {
			commitHashes[repoPath] = hash
		}
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Changes extracted to %s.", filepath.Base(repoPath)),
		})
		return nil
	}

	defBranch, err := gitutil.DefaultBranch(repoPath)
	if err != nil {
		return fmt.Errorf("defaultBranch for %s: %w", repoPath, err)
	}

	// Always capture defBranch HEAD for diff reconstruction, even if there
	// are no commits to merge. This ensures TaskDiff can show "genuinely no
	// changes" rather than failing silently when the early return fires.
	if base, err := gitutil.GetCommitHashForRef(repoPath, defBranch); err == nil {
		baseHashes[repoPath] = base
	}

	// Skip if there are no commits to merge.
	ahead, err := gitutil.HasCommitsAheadOf(worktreePath, defBranch)
	if err != nil {
		logger.Runner.Warn("rev-list check", "task", taskID, "repo", repoPath, "error", err)
	}
	if !ahead {
		logger.Runner.Info("no commits to merge, skipping", "task", taskID, "repo", repoPath)
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Skipping %s — no new commits to merge.", repoPath),
		})
		return nil
	}

	// Rebase with conflict-resolution retry loop.
	var rebaseErr error
	for attempt := 1; attempt <= maxRebaseRetries; attempt++ {
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Rebasing %s onto %s (attempt %d/%d)...", repoPath, defBranch, attempt, maxRebaseRetries),
		})

		rebaseErr = gitutil.RebaseOntoDefault(repoPath, worktreePath)
		if rebaseErr == nil {
			break
		}

		if attempt == maxRebaseRetries {
			return fmt.Errorf(
				"rebase failed after %d attempts in %s: %w",
				maxRebaseRetries, repoPath, rebaseErr,
			)
		}

		if !isConflictError(rebaseErr) {
			return fmt.Errorf("rebase %s: %w", repoPath, rebaseErr)
		}

		logger.Runner.Warn("rebase conflict, invoking resolver",
			"task", taskID, "repo", repoPath, "attempt", attempt)
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Conflict in %s — running resolver (attempt %d)...", repoPath, attempt),
		})

		if resolveErr := r.resolveConflicts(ctx, taskID, repoPath, worktreePath, sessionID); resolveErr != nil {
			return fmt.Errorf("conflict resolution failed: %w", resolveErr)
		}
	}

	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": fmt.Sprintf("Fast-forward merging %s into %s...", branchName, defBranch),
	})
	if err := gitutil.FFMerge(repoPath, branchName); err != nil {
		return fmt.Errorf("ff-merge %s: %w", repoPath, err)
	}

	hash, err := gitutil.GetCommitHash(repoPath)
	if err != nil {
		logger.Runner.Warn("get commit hash", "task", taskID, "repo", repoPath, "error", err)
	} else {
		commitHashes[repoPath] = hash
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Merged %s — commit %s", repoPath, hash[:8]),
		})
	}

	return nil
}

// isConflictError reports whether err wraps ErrConflict.
func isConflictError(err error) bool {
	return err != nil && strings.Contains(err.Error(), gitutil.ErrConflict.Error())
}

// resolveConflicts runs a Claude container session to resolve rebase conflicts.
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

	r.store.InsertEvent(context.Background(), taskID, store.EventTypeSystem, map[string]string{
		"result": "Conflict resolver: " + truncate(output.Result, 500),
	})
	return nil
}
