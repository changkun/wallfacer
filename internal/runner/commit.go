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
	"github.com/google/uuid"
)

// Commit creates its own timeout context and runs the full commit pipeline
// (stage → rebase → merge → PROGRESS.md) for a task.
func (r *Runner) Commit(taskID uuid.UUID, sessionID string) {
	task, err := r.store.GetTask(context.Background(), taskID)
	if err != nil {
		logger.Runner.Error("commit get task", "task", taskID, "error", err)
		return
	}
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = defaultTaskTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r.commit(ctx, taskID, sessionID, task.Turns, task.WorktreePaths, task.BranchName)
}

// commit runs Phase 1 (host-side commit in worktree), Phase 2 (host-side
// rebase+merge), Phase 3 (PROGRESS.md), Phase 4 (worktree cleanup).
func (r *Runner) commit(
	ctx context.Context,
	taskID uuid.UUID,
	sessionID string,
	turns int,
	worktreePaths map[string]string,
	branchName string,
) {
	bgCtx := context.Background()
	logger.Runner.Info("auto-commit", "task", taskID, "session", sessionID)

	// Phase 1: stage and commit all uncommitted changes on the host.
	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Phase 1/4: Staging and committing changes...",
	})
	task, _ := r.store.GetTask(bgCtx, taskID)
	taskPrompt := ""
	if task != nil {
		taskPrompt = task.Prompt
	}
	r.hostStageAndCommit(taskID, worktreePaths, taskPrompt)

	// Phase 2: host-side rebase and merge for each git worktree.
	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Phase 2/4: Rebasing and merging into default branch...",
	})
	commitHashes, baseHashes, mergeErr := r.rebaseAndMerge(ctx, taskID, worktreePaths, branchName, sessionID)
	if mergeErr != nil {
		logger.Runner.Error("rebase/merge failed", "task", taskID, "error", mergeErr)
		r.store.InsertEvent(bgCtx, taskID, "error", map[string]string{
			"error": "rebase/merge failed: " + mergeErr.Error(),
		})
		return
	}

	// Phase 3: persist commit hashes and write PROGRESS.md.
	r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
		"result": "Phase 3/4: Updating PROGRESS.md...",
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
	task, _ = r.store.GetTask(bgCtx, taskID)
	if task != nil {
		if err := r.writeProgressMD(task, commitHashes); err != nil {
			logger.Runner.Warn("write PROGRESS.md", "task", taskID, "error", err)
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
	logger.Runner.Info("commit completed", "task", taskID)
}

// hostStageAndCommit stages and commits all uncommitted changes in each
// worktree directly on the host. Returns true if any new commits were created.
func (r *Runner) hostStageAndCommit(taskID uuid.UUID, worktreePaths map[string]string, prompt string) bool {
	// First pass: stage all changes and collect diff stats for each worktree
	// that has pending changes.
	type pendingCommit struct {
		repoPath     string
		worktreePath string
		diffStat     string
		recentLog    string
	}
	var pending []pendingCommit

	for repoPath, worktreePath := range worktreePaths {
		if out, err := exec.Command("git", "-C", worktreePath, "add", "-A").CombinedOutput(); err != nil {
			logger.Runner.Warn("host commit: git add -A", "repo", repoPath, "error", err, "output", string(out))
			continue
		}

		out, _ := exec.Command("git", "-C", worktreePath, "status", "--porcelain").Output()
		if len(strings.TrimSpace(string(out))) == 0 {
			logger.Runner.Info("host commit: nothing to commit", "repo", repoPath)
			continue
		}

		statOut, _ := exec.Command("git", "-C", worktreePath, "diff", "--cached", "--stat").Output()
		logOut, _ := exec.Command("git", "-C", worktreePath, "log", "--oneline", "-3").Output()
		pending = append(pending, pendingCommit{repoPath, worktreePath, strings.TrimSpace(string(statOut)), strings.TrimSpace(string(logOut))})
	}

	if len(pending) == 0 {
		return false
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
	committed := false
	for _, p := range pending {
		if out, err := exec.Command("git", "-C", p.worktreePath, "commit", "-m", msg).CombinedOutput(); err != nil {
			logger.Runner.Warn("host commit: git commit", "repo", p.repoPath, "error", err, "output", string(out))
			continue
		}
		committed = true
		logger.Runner.Info("host commit: committed changes", "repo", p.repoPath)
	}
	return committed
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
		"- Subject line: imperative mood, max 72 characters, no trailing period\n" +
		"- Optionally add a blank line followed by a short body (2-4 lines) explaining what changed and why\n" +
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

		if !gitutil.IsGitRepo(repoPath) {
			// Non-git workspace: copy snapshot changes back to the original directory.
			r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
				"result": fmt.Sprintf("Extracting changes from sandbox to %s...", filepath.Base(repoPath)),
			})
			if err := extractSnapshotToWorkspace(worktreePath, repoPath); err != nil {
				return commitHashes, baseHashes, fmt.Errorf("extract snapshot for %s: %w", repoPath, err)
			}
			if hash, err := gitutil.GetCommitHash(worktreePath); err == nil {
				commitHashes[repoPath] = hash
			}
			r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
				"result": fmt.Sprintf("Changes extracted to %s.", filepath.Base(repoPath)),
			})
			continue
		}

		defBranch, err := gitutil.DefaultBranch(repoPath)
		if err != nil {
			return commitHashes, baseHashes, fmt.Errorf("defaultBranch for %s: %w", repoPath, err)
		}

		// Skip if there are no commits to merge.
		ahead, err := gitutil.HasCommitsAheadOf(worktreePath, defBranch)
		if err != nil {
			logger.Runner.Warn("rev-list check", "task", taskID, "repo", repoPath, "error", err)
		}
		if !ahead {
			logger.Runner.Info("no commits to merge, skipping", "task", taskID, "repo", repoPath)
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

			rebaseErr = gitutil.RebaseOntoDefault(repoPath, worktreePath)
			if rebaseErr == nil {
				break
			}

			if attempt == maxRebaseRetries {
				return commitHashes, baseHashes, fmt.Errorf(
					"rebase failed after %d attempts in %s: %w",
					maxRebaseRetries, repoPath, rebaseErr,
				)
			}

			if !isConflictError(rebaseErr) {
				return commitHashes, baseHashes, fmt.Errorf("rebase %s: %w", repoPath, rebaseErr)
			}

			logger.Runner.Warn("rebase conflict, invoking resolver",
				"task", taskID, "repo", repoPath, "attempt", attempt)
			r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
				"result": fmt.Sprintf("Conflict in %s — running resolver (attempt %d)...", repoPath, attempt),
			})

			if resolveErr := r.resolveConflicts(ctx, taskID, repoPath, worktreePath, sessionID); resolveErr != nil {
				return commitHashes, baseHashes, fmt.Errorf("conflict resolution failed: %w", resolveErr)
			}
		}

		// Capture defBranch HEAD before the merge so TaskDiff can reconstruct
		// the full task diff even after worktrees are cleaned up.
		if base, err := gitutil.GetCommitHash(repoPath); err == nil {
			baseHashes[repoPath] = base
		}

		r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
			"result": fmt.Sprintf("Fast-forward merging %s into %s...", branchName, defBranch),
		})
		if err := gitutil.FFMerge(repoPath, branchName); err != nil {
			return commitHashes, baseHashes, fmt.Errorf("ff-merge %s: %w", repoPath, err)
		}

		hash, err := gitutil.GetCommitHash(repoPath)
		if err != nil {
			logger.Runner.Warn("get commit hash", "task", taskID, "repo", repoPath, "error", err)
		} else {
			commitHashes[repoPath] = hash
			r.store.InsertEvent(bgCtx, taskID, "output", map[string]string{
				"result": fmt.Sprintf("Merged %s — commit %s", repoPath, hash[:8]),
			})
		}
	}

	return commitHashes, baseHashes, nil
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

	r.store.InsertEvent(context.Background(), taskID, "output", map[string]string{
		"result": "Conflict resolver: " + truncate(output.Result, 500),
	})
	return nil
}
