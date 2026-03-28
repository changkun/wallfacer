package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/prompts"
	"github.com/google/uuid"
)

// ErrCommitMessageGeneration marks failures that occur while generating the
// synthetic git commit message prior to the host-side commit step.
var ErrCommitMessageGeneration = errors.New("commit message generation failed")

// IsCommitMessageGenerationError reports whether err originated from commit
// message generation and should return the task to waiting rather than using a
// prompt-derived fallback commit message.
func IsCommitMessageGenerationError(err error) bool {
	return errors.Is(err, ErrCommitMessageGeneration)
}

// newCommitMessageGenerationError wraps a formatted message with ErrCommitMessageGeneration
// so callers can distinguish commit-message failures from other commit pipeline errors.
func newCommitMessageGenerationError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCommitMessageGeneration, fmt.Sprintf(format, args...))
}

// Commit creates its own timeout context and runs the full commit pipeline
// (stage → rebase → merge → cleanup) for a task.
// Returns an error if any phase of the pipeline fails.
func (r *Runner) Commit(taskID uuid.UUID, sessionID string) error {
	task, err := r.taskStore(taskID).GetTask(r.shutdownCtx, taskID)
	if err != nil {
		logger.Runner.Error("commit get task", "task", taskID, "error", err)
		return fmt.Errorf("get task: %w", err)
	}
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = constants.DefaultTaskTimeout
	}
	ctx, cancel := context.WithTimeout(r.shutdownCtx, timeout)
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
	_ int,
	worktreePaths map[string]string,
	branchName string,
) error {
	bgCtx := r.shutdownCtx
	logger.Runner.Info("auto-commit", "task", taskID, "session", sessionID)

	// Phase 1: stage and commit all uncommitted changes on the host.
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

		"result": "Phase 1/3: Staging and committing changes...",
	})
	task, _ := r.taskStore(taskID).GetTask(bgCtx, taskID)
	taskPrompt := ""
	if task != nil {
		taskPrompt = task.Prompt
	}
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: "stage"})

	_, stageErr := r.hostStageAndCommit(ctx, taskID, worktreePaths, taskPrompt)
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "commit", Label: "stage"})

	if stageErr != nil {
		logger.Runner.Error("host stage/commit failed", "task", taskID, "error", stageErr)
		eventMessage := "stage/commit failed: " + stageErr.Error()
		if IsCommitMessageGenerationError(stageErr) {
			eventMessage = stageErr.Error()
		}
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{

			"error": eventMessage,
		})
		return fmt.Errorf("stage and commit: %w", stageErr)
	}

	// Phase 2: host-side rebase and merge for each git worktree.
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

		"result": "Phase 2/3: Rebasing and merging into default branch...",
	})
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: "rebase_merge"})

	commitHashes, baseHashes, snapshotDiffs, mergeErr := r.rebaseAndMerge(ctx, taskID, worktreePaths, branchName, sessionID)
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "commit", Label: "rebase_merge"})

	if mergeErr != nil {
		logger.Runner.Error("rebase/merge failed", "task", taskID, "error", mergeErr)
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{

			"error": "rebase/merge failed: " + mergeErr.Error(),
		})
		return fmt.Errorf("rebase/merge: %w", mergeErr)
	}

	// Phase 3: persist commit hashes and clean up worktrees.
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

		"result": "Phase 3/3: Cleaning up...",
	})
	if len(commitHashes) > 0 {
		if err := r.taskStore(taskID).UpdateTaskCommitHashes(bgCtx, taskID, commitHashes); err != nil {
			logger.Runner.Warn("save commit hashes", "task", taskID, "error", err)
		}
	}
	if len(baseHashes) > 0 {
		if err := r.taskStore(taskID).UpdateTaskBaseCommitHashes(bgCtx, taskID, baseHashes); err != nil {
			logger.Runner.Warn("save base commit hashes", "task", taskID, "error", err)
		}
	}
	if len(snapshotDiffs) > 0 {
		if err := r.taskStore(taskID).UpdateTaskSnapshotDiffs(bgCtx, taskID, snapshotDiffs); err != nil {
			logger.Runner.Warn("save snapshot diffs", "task", taskID, "error", err)
		}
	}
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: "cleanup"})

	r.cleanupWorktrees(taskID, worktreePaths, branchName)
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "commit", Label: "cleanup"})

	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

		"result": "Commit pipeline completed.",
	})
	logger.Runner.Info("commit completed", "task", taskID)

	// Auto-push: if enabled, push each workspace whose local branch is at
	// least AutoPushThreshold commits ahead of its upstream.
	r.maybeAutoPush(bgCtx, taskID, worktreePaths)

	return nil
}

// maybeAutoPush checks the auto-push configuration and, for each repo that
// qualifies (ahead_count >= threshold), runs `git push`.
func (r *Runner) maybeAutoPush(ctx context.Context, taskID uuid.UUID, worktreePaths map[string]string) {
	if r.envFile == "" {
		return
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil || !cfg.AutoPushEnabled {
		return
	}
	threshold := cfg.AutoPushThreshold
	if threshold <= 0 {
		threshold = 1
	}

	for repoPath := range worktreePaths {
		if !gitutil.IsGitRepo(repoPath) {
			continue
		}
		s := gitutil.WorkspaceStatus(repoPath)
		if !s.HasRemote || s.AheadCount < threshold {
			continue
		}
		logger.Runner.Info("auto-push", "task", taskID, "repo", repoPath, "ahead", s.AheadCount)
		_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Auto-pushing %s (%d commit(s) ahead)...", repoPath, s.AheadCount),
		})
		pushLabel := "push_" + filepath.Base(repoPath)
		_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: pushLabel})

		out, pushErr := cmdexec.Git(repoPath, "push").WithContext(ctx).Combined()
		_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "commit", Label: pushLabel})

		if pushErr != nil {
			logger.Runner.Error("auto-push failed", "task", taskID, "repo", repoPath, "error", pushErr)
			_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeError, map[string]string{

				"error": fmt.Sprintf("auto-push failed for %s: %v\n%s", repoPath, pushErr, out),
			})
		} else {
			logger.Runner.Info("auto-push succeeded", "task", taskID, "repo", repoPath)
			_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

				"result": fmt.Sprintf("Auto-push succeeded for %s.", repoPath),
			})
		}
	}
}

// hostStageAndCommit stages and commits all uncommitted changes in each
// worktree directly on the host. Returns true if any new commits were created.
// Returns an error if changes were present but could not be staged or committed.
// ctx is the task timeout context: all git subprocesses are tied to it so that
// a task timeout or server shutdown interrupts them promptly.
func (r *Runner) hostStageAndCommit(ctx context.Context, taskID uuid.UUID, worktreePaths map[string]string, prompt string) (bool, error) {
	if len(worktreePaths) == 0 {
		return false, fmt.Errorf("no worktrees to commit")
	}

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

	var missing []string
	for repoPath, worktreePath := range worktreePaths {
		if _, err := os.Stat(worktreePath); err != nil {
			logger.Runner.Warn("host commit: worktree missing, skipping", "repo", repoPath, "path", worktreePath)
			missing = append(missing, repoPath)
			continue
		}
		if !gitutil.IsGitRepo(worktreePath) {
			logger.Runner.Warn("host commit: worktree is not a valid git repo, skipping", "repo", repoPath, "path", worktreePath)
			missing = append(missing, repoPath)
			continue
		}
		if out, err := cmdexec.Git(worktreePath, "add", "-A").WithContext(ctx).Combined(); err != nil {
			if ctx.Err() != nil {
				return false, fmt.Errorf("context canceled during git add: %w", ctx.Err())
			}
			logger.Runner.Warn("host commit: git add -A", "repo", repoPath, "worktree", worktreePath, "error", err, "output", out)
			errs = append(errs, fmt.Sprintf("git add in %s (worktree %s): %v: %s", repoPath, worktreePath, err, out))
			continue
		}

		out, _ := cmdexec.Git(worktreePath, "status", "--porcelain").WithContext(ctx).Output()
		if len(out) == 0 {
			logger.Runner.Info("host commit: nothing to commit", "repo", repoPath)
			continue
		}

		statOut, _ := cmdexec.Git(worktreePath, "diff", "--cached", "--stat").WithContext(ctx).Output()
		logOut, _ := cmdexec.Git(worktreePath, "log", "--format=%s", "-5").WithContext(ctx).Output()
		pending = append(pending, pendingCommit{repoPath, worktreePath, statOut, logOut})
	}

	if len(pending) == 0 {
		if len(errs) > 0 {
			return false, fmt.Errorf("staging failed: %s", strings.Join(errs, "; "))
		}
		if len(missing) > 0 && len(missing) == len(worktreePaths) {
			return false, fmt.Errorf("all worktrees missing: %s", strings.Join(missing, ", "))
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
	msg, err := r.generateCommitMessage(ctx, taskID, prompt, allStats.String(), allLogs.String())
	if err != nil {
		msg = localFallbackCommitMessage(prompt, allStats.String())
		logger.Runner.Warn("commit message generation failed, using local fallback", "task", taskID, "error", err, "message", msg)
		_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": "Commit message generation failed; using fallback commit message.",
		})
	}

	// Persist the commit message so it can be displayed in the UI.
	if saveErr := r.taskStore(taskID).UpdateTaskCommitMessage(r.shutdownCtx, taskID, msg); saveErr != nil {
		logger.Runner.Warn("save commit message", "task", taskID, "error", saveErr)
	}

	// Second pass: commit each worktree with the generated message.
	// Use global git identity to prevent sandbox-set local configs from
	// overriding the host user's author information.
	var gitConfigOverrides []string
	if n, err := cmdexec.New("git", "config", "--global", "user.name").WithContext(ctx).Output(); err == nil && n != "" {
		gitConfigOverrides = append(gitConfigOverrides, "-c", "user.name="+n)
	}
	if e, err := cmdexec.New("git", "config", "--global", "user.email").WithContext(ctx).Output(); err == nil && e != "" {
		gitConfigOverrides = append(gitConfigOverrides, "-c", "user.email="+e)
	}

	committed := false
	for _, p := range pending {
		args := append([]string{"-C", p.worktreePath}, gitConfigOverrides...)
		args = append(args, "commit", "-m", msg)
		if out, err := cmdexec.New("git", args...).WithContext(ctx).Combined(); err != nil {
			if ctx.Err() != nil {
				return false, fmt.Errorf("context canceled during git commit: %w", ctx.Err())
			}
			logger.Runner.Warn("host commit: git commit", "repo", p.repoPath, "error", err, "output", out)
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

// localFallbackCommitMessage builds a "wallfacer: <subject>" commit message
// without invoking a container. Used when the agent-based commit message
// generation fails. The subject is derived from the first line of the prompt,
// falling back to the first line of the diff stat, and is capped at
// MaxCommitSubjectRunes.
func localFallbackCommitMessage(prompt, diffStat string) string {
	subject := strings.TrimSpace(prompt)
	if idx := strings.Index(subject, "\n"); idx >= 0 {
		subject = subject[:idx]
	}
	subject = strings.Join(strings.Fields(subject), " ")
	subject = strings.Trim(subject, "`")
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = strings.TrimSpace(diffStat)
		if idx := strings.Index(subject, "\n"); idx >= 0 {
			subject = subject[:idx]
		}
		subject = strings.Join(strings.Fields(subject), " ")
	}
	if subject == "" {
		subject = "update task changes"
	}

	const prefix = "wallfacer: "
	maxSubjectRunes := constants.MaxCommitSubjectRunes
	runes := []rune(subject)
	if len(runes) > maxSubjectRunes {
		subject = strings.TrimSpace(string(runes[:maxSubjectRunes]))
	}
	return prefix + subject
}

// generateCommitMessage runs a lightweight container to produce a descriptive
// git commit message from the task prompt, staged diff stats, and recent git
// log history (used to match the project's commit style).
// ctx is the caller-supplied task context; a 90-second sub-deadline is derived
// from it so that task cancellation or timeout propagates into the container.
func (r *Runner) generateCommitMessage(ctx context.Context, taskID uuid.UUID, prompt, diffStat, recentLog string) (string, error) {
	task, err := r.taskStore(taskID).GetTask(r.shutdownCtx, taskID)
	if err != nil {
		logger.Runner.Warn("generate commit message: get task", "task", taskID, "error", err)
	}

	sb := sandbox.Claude
	if task != nil {
		sb = r.sandboxForTaskActivity(task, activityCommitMessage)
	}

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	containerName := "wallfacer-commit-" + taskID.String()[:8]
	r.taskContainers.Set(taskID, containerName)
	defer r.taskContainers.Delete(taskID)
	commitPrompt := r.promptsMgr.CommitMessage(prompts.CommitData{
		Prompt:    prompt,
		DiffStat:  diffStat,
		RecentLog: recentLog,
	})
	runWithSandbox := func(selectedSandbox sandbox.Type) (*agentOutput, error) {
		selectedModel := r.modelFromEnvForSandbox(selectedSandbox)

		spec := r.buildBaseContainerSpec(containerName, selectedModel, selectedSandbox)
		spec.Labels = map[string]string{"wallfacer.task.id": taskID.String()}
		spec.Cmd = buildAgentCmd(commitPrompt, selectedModel)

		_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "container_run", Label: string(store.SandboxActivityCommitMessage)})

		handle, launchErr := r.backend.Launch(ctx, spec)
		if launchErr != nil {
			_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: string(store.SandboxActivityCommitMessage)})
			return nil, fmt.Errorf("launch commit message container: %w", launchErr)
		}
		r.taskContainers.SetHandle(taskID, handle, nil)

		rawStdout, _ := io.ReadAll(handle.Stdout())
		rawStderr, _ := io.ReadAll(handle.Stderr())
		exitCode, _ := handle.Wait()
		_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "container_run", Label: string(store.SandboxActivityCommitMessage)})

		if exitCode != 0 && ctx.Err() == nil {
			return nil, fmt.Errorf("container exited with code %d: stderr=%s", exitCode, truncate(string(rawStderr), 200))
		}

		raw := strings.TrimSpace(string(rawStdout))
		if raw == "" {
			return nil, fmt.Errorf("empty output")
		}

		output, err := parseOutput(raw)
		if err != nil {
			return nil, fmt.Errorf("parse failure: raw=%s", truncate(raw, 200))
		}
		output.ActualSandbox = selectedSandbox
		return output, nil
	}

	initialSandbox := sb
	output, err := runWithSandbox(initialSandbox)
	if err != nil {
		if initialSandbox == sandbox.Claude && isLikelyTokenLimitError(err.Error()) {
			logger.Runner.Warn("commit message generation: claude token limit hit; retrying with codex", "task", taskID)
			_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSystem, map[string]string{

				"result": "Sandbox fallback: claude → codex (token/rate limit hit during commit message generation)",
			})
			output, err = runWithSandbox(sandbox.Codex)
		}
		if err != nil {
			logger.Runner.Warn("commit message generation failed", "task", taskID, "error", err)
			return "", newCommitMessageGenerationError("%v", err)
		}
	}
	if initialSandbox == sandbox.Claude && output != nil && output.IsError &&
		isLikelyTokenLimitError(output.Result, output.Subtype) {
		logger.Runner.Warn("commit message generation: claude output reported token limit; retrying with codex", "task", taskID)
		_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": "Sandbox fallback: claude → codex (token/rate limit in commit message output)",
		})
		output, err = runWithSandbox(sandbox.Codex)
		if err != nil {
			logger.Runner.Warn("commit message generation failed", "task", taskID, "error", err)
			return "", newCommitMessageGenerationError("%v", err)
		}
	}
	if output != nil && output.IsError {
		logger.Runner.Warn("commit message generation: agent error", "task", taskID, "subtype", output.Subtype)
		message := strings.TrimSpace(output.Result)
		if message == "" {
			message = "agent returned an error result"
		}
		return "", newCommitMessageGenerationError("%s", message)
	}

	msg := strings.TrimSpace(output.Result)
	msg = strings.Trim(msg, "`")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		logger.Runner.Warn("commit message generation: blank result", "task", taskID)
		return "", newCommitMessageGenerationError("blank result")
	}

	if output.Usage.InputTokens > 0 || output.Usage.OutputTokens > 0 || output.TotalCostUSD > 0 {
		_ = r.taskStore(taskID).AccumulateSubAgentUsage(r.shutdownCtx, taskID, store.SandboxActivityCommitMessage, store.TaskUsage{

			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		})
		if appErr := r.taskStore(taskID).AppendTurnUsage(taskID, store.TurnUsageRecord{
			Turn:                 1,
			Timestamp:            time.Now().UTC(),
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
			Sandbox:              output.ActualSandbox,
			SubAgent:             store.SandboxActivityCommitMessage,
		}); appErr != nil {
			logger.Runner.Warn("commit message: append turn usage failed", "task", taskID, "error", appErr)
		}
	}

	return msg, nil
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
) (commitHashes, baseHashes, snapshotDiffs map[string]string, err error) {
	bgCtx := r.shutdownCtx
	commitHashes = make(map[string]string)
	baseHashes = make(map[string]string)
	snapshotDiffs = make(map[string]string)

	var missing int
	for repoPath, worktreePath := range worktreePaths {
		if _, err := os.Stat(worktreePath); err != nil {
			logger.Runner.Warn("rebase+merge: worktree missing, skipping", "task", taskID, "repo", repoPath, "path", worktreePath)
			missing++
			continue
		}
		logger.Runner.Info("rebase+merge", "task", taskID, "repo", repoPath)

		// Serialize rebase+merge per repo so concurrent tasks on the same
		// repo don't race (the second task sees the first task's merge
		// before rebasing). Tasks on different repos remain fully concurrent.
		mu := r.repoLock(repoPath)
		mu.Lock()

		err := r.rebaseAndMergeOne(ctx, taskID, repoPath, worktreePath, branchName, sessionID, bgCtx, commitHashes, baseHashes, snapshotDiffs)
		mu.Unlock()
		if err != nil {
			return commitHashes, baseHashes, snapshotDiffs, err
		}
	}

	if missing > 0 && missing == len(worktreePaths) {
		return commitHashes, baseHashes, snapshotDiffs, fmt.Errorf("all worktrees missing, nothing to rebase/merge")
	}

	return commitHashes, baseHashes, snapshotDiffs, nil
}

// rebaseAndMergeOne handles the rebase+merge pipeline for a single repo/worktree pair.
// Extracted so the caller can hold/release the per-repo lock cleanly.
func (r *Runner) rebaseAndMergeOne(
	ctx context.Context,
	taskID uuid.UUID,
	repoPath, worktreePath, branchName, sessionID string,
	bgCtx context.Context, //nolint:revive // bgCtx is a separate long-lived context, not a replacement for ctx
	commitHashes, baseHashes, snapshotDiffs map[string]string,
) error {
	if !gitutil.IsGitRepo(repoPath) || !gitutil.HasCommits(repoPath) {
		// Non-git workspace or empty git repo (no commits): the worktree was
		// set up as a snapshot — copy changes back to the original directory.
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Extracting changes from sandbox to %s...", filepath.Base(repoPath)),
		})

		// Capture the diff before extraction so we can show it in the UI.
		// The snapshot has an initial commit + agent changes committed on top.
		if diff := computeSnapshotDiff(ctx, worktreePath); diff != "" {
			snapshotDiffs[repoPath] = diff
		}

		if err := extractSnapshotToWorkspace(worktreePath, repoPath); err != nil {
			return fmt.Errorf("extract snapshot for %s: %w", repoPath, err)
		}
		if hash, err := gitutil.GetCommitHash(worktreePath); err == nil {
			commitHashes[repoPath] = hash
		}
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

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
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Skipping %s — no new commits to merge.", repoPath),
		})
		return nil
	}

	// Rebase with conflict-resolution retry loop.
	var rebaseErr error
	for attempt := 1; attempt <= constants.MaxRebaseRetries; attempt++ {
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Rebasing %s onto %s (attempt %d/%d)...", repoPath, defBranch, attempt, constants.MaxRebaseRetries),
		})

		rebaseErr = gitutil.RebaseOntoDefault(repoPath, worktreePath)
		if rebaseErr == nil {
			break
		}

		// Emit a structured event with conflicted file paths for observability.
		var ce *gitutil.ConflictError
		if errors.As(rebaseErr, &ce) && len(ce.ConflictedFiles) > 0 {
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]any{

				"error":            ce.Error(),
				"phase":            "rebase",
				"conflicted_files": ce.ConflictedFiles,
				"worktree":         ce.WorktreePath,
			})
		}

		if attempt == constants.MaxRebaseRetries {
			return fmt.Errorf(
				"rebase failed after %d attempts in %s: %w",
				constants.MaxRebaseRetries, repoPath, rebaseErr,
			)
		}

		if !isConflictError(rebaseErr) {
			return fmt.Errorf("rebase %s: %w", repoPath, rebaseErr)
		}

		logger.Runner.Warn("rebase conflict, invoking resolver",
			"task", taskID, "repo", repoPath, "attempt", attempt)
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Conflict in %s — running resolver (attempt %d)...", repoPath, attempt),
		})

		if resolveErr := r.resolveConflicts(ctx, taskID, repoPath, worktreePath, sessionID, defBranch, ConflictResolverTriggerCommit, attempt, constants.MaxRebaseRetries); resolveErr != nil {
			return fmt.Errorf("conflict resolution failed: %w", resolveErr)
		}
	}

	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

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
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Merged %s — commit %s", repoPath, hash[:8]),
		})
	}

	return nil
}

// isConflictError reports whether err wraps ErrConflict.
func isConflictError(err error) bool {
	return errors.Is(err, gitutil.ErrConflict)
}

// resolveConflicts runs a Claude container session to resolve rebase conflicts.
// The rebase has already been aborted by RebaseOntoDefault, so the worktree is
// on the task branch in a clean state. The agent must start the rebase itself,
// resolve any conflicts, and complete the rebase with `git rebase --continue`.
func (r *Runner) resolveConflicts(
	ctx context.Context,
	taskID uuid.UUID,
	repoPath, worktreePath string,
	sessionID string,
	defBranch string,
	trigger ConflictResolverTrigger,
	attempt int,
	maxAttempts int,
) error {
	basename := filepath.Base(worktreePath)
	containerPath := "/workspace/" + basename
	repoName := filepath.Base(repoPath)

	prompt := r.promptsMgr.ConflictResolution(prompts.ConflictData{
		ContainerPath: containerPath,
		DefaultBranch: defBranch,
	})

	_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSystem, map[string]any{

		"phase":        "conflict_resolver",
		"status":       "started",
		"trigger":      string(trigger),
		"repo":         repoName,
		"attempt":      attempt,
		"max_attempts": maxAttempts,
		"result":       fmt.Sprintf("Conflict resolver started for %s (%s, attempt %d/%d).", repoName, trigger, attempt, maxAttempts),
	})

	// Mount only the conflicted worktree for this targeted fix.
	override := map[string]string{repoPath: worktreePath}

	output, rawStdout, rawStderr, err := r.runContainer(ctx, taskID, prompt, sessionID, override, "", nil, "", activityCommitMessage)

	task, _ := r.taskStore(taskID).GetTask(r.shutdownCtx, taskID)
	turns := 0
	if task != nil {
		turns = task.Turns + 1
	}
	_ = r.taskStore(taskID).SaveTurnOutput(taskID, turns, rawStdout, rawStderr)

	if len(rawStderr) > 0 {
		stderrFile := fmt.Sprintf("turn-%04d.stderr.txt", turns)
		_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{

			"stderr_file": stderrFile,
			"turn":        fmt.Sprintf("%d", turns),
			"phase":       "conflict_resolver",
		})
	}

	if err != nil {
		_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeError, map[string]any{

			"phase":        "conflict_resolver",
			"status":       "failed",
			"trigger":      string(trigger),
			"repo":         repoName,
			"attempt":      attempt,
			"max_attempts": maxAttempts,
			"error":        fmt.Sprintf("Conflict resolver container failed for %s: %v", repoName, err),
		})
		return fmt.Errorf("conflict resolver container: %w", err)
	}
	if output.IsError {
		_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeError, map[string]any{

			"phase":        "conflict_resolver",
			"status":       "failed",
			"trigger":      string(trigger),
			"repo":         repoName,
			"attempt":      attempt,
			"max_attempts": maxAttempts,
			"error":        "Conflict resolver reported error: " + truncate(output.Result, 300),
		})
		return fmt.Errorf("conflict resolver reported error: %s", truncate(output.Result, 300))
	}

	_ = r.taskStore(taskID).InsertEvent(r.shutdownCtx, taskID, store.EventTypeSystem, map[string]any{

		"phase":        "conflict_resolver",
		"status":       "succeeded",
		"trigger":      string(trigger),
		"repo":         repoName,
		"attempt":      attempt,
		"max_attempts": maxAttempts,
		"result":       "Conflict resolver: " + truncate(output.Result, 500),
	})
	return nil
}
