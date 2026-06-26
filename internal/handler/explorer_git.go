package handler

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"latere.ai/x/wallfacer/internal/gitutil"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/pkg/cmdexec"
)

const (
	// editSourceTrailerPrefix tags explorer commits so they are queryable with
	// `git log --grep="^Edit-Source: "`, consistent with the Plan-Round /
	// Plan-Thread trailers planning commits carry. The trailer is what makes a
	// manual explorer edit attributable and revertible by the same `git revert`
	// mechanism the planning path uses.
	editSourceTrailerPrefix = "Edit-Source: "

	// explorerEditSource is the value written into the Edit-Source trailer.
	explorerEditSource = "explorer"

	// explorerCommitScope tags explorer commits inside the subject-line scope
	// (e.g. `specs/foo.md(edit): ...`), matching conventional-commits scope
	// style and the planning path's `(plan)` marker.
	explorerCommitScope = "(edit)"

	// explorerCommitTimeout bounds the synchronous git commit so a slow
	// filesystem or a wedged git process cannot block an explorer save
	// indefinitely. Commits are normally tens of milliseconds; hooks are
	// skipped with --no-verify so this only guards pathological stalls.
	explorerCommitTimeout = 10 * time.Second
)

// commitExplorerEdit commits a single file saved through the explorer with a
// deterministic, scope-prefixed message:
//
//	<rel-path>(edit): update <basename>
//
//	Edit-Source: explorer
//
// The message is template-based (no commit-message agent round-trip) to keep
// saves fast. Only relPath is committed: the partial-commit pathspec form
// (`git commit -- <path>`) records the working-tree content of relPath alone
// and leaves the rest of the index untouched, so a concurrent task's or
// planning round's staged changes are never swept into an explorer commit.
// Hooks are skipped with --no-verify because autosave is in the save's
// critical path.
//
// Best-effort, mirroring commitPlanningRound: returns nil (no commit) when the
// workspace is not a git repo or relPath has no pending change; returns an
// error only when the git commit itself fails. Callers log and continue, since
// the file is already written and a commit failure must not fail the save.
func commitExplorerEdit(ctx context.Context, ws, relPath string) error {
	if !gitutil.IsGitRepo(ws) {
		return nil
	}
	relPath = filepath.ToSlash(relPath)

	// Skip the no-op case (identical content) so saves that change nothing do
	// not produce empty commits, and bail when the status probe itself fails.
	out, err := cmdexec.Git(ws, "status", "--porcelain", "--", relPath).WithContext(ctx).Output()
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}

	// Stage the file: a partial `git commit -- <path>` rejects an untracked
	// path, so a newly created file must be added first.
	if err := cmdexec.Git(ws, "add", "--", relPath).WithContext(ctx).Run(); err != nil {
		return fmt.Errorf("git add %s: %w", relPath, err)
	}

	msg := buildExplorerCommitMessage(relPath)

	// Force the host's global git identity via -c overrides so a
	// sandbox-polluted repo-local user.name/user.email (a task container can
	// write to .git/config, shared across worktrees) never authors the commit.
	// Mirrors commitPlanningRound and the board commit pipeline.
	args := gitutil.GlobalIdentityOverrides(ctx)
	// The trailing `-- <path>` pathspec makes this a partial commit: only
	// relPath's staged content is recorded, leaving any other staged or
	// working-tree changes (a concurrent task, a planning round) untouched.
	args = append(args, "commit", "-m", msg, "--no-verify", "--", relPath)
	if err := cmdexec.Git(ws, args...).WithContext(ctx).Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// buildExplorerCommitMessage assembles the deterministic commit message for an
// explorer save: a scope-prefixed subject derived from relPath plus the
// Edit-Source trailer. The full relPath is the subject scope (unbounded, like
// the planning path's primary-path prefix) so the edited file is obvious in
// `git log --oneline`.
func buildExplorerCommitMessage(relPath string) string {
	subject := relPath + explorerCommitScope + ": update " + path.Base(relPath)
	return subject + "\n\n" + editSourceTrailerPrefix + explorerEditSource
}

// commitExplorerWrite auto-commits a file just saved through the explorer.
// It is best-effort: every failure path logs and returns without surfacing an
// error, because the atomic write already succeeded and the save must not fail
// on a commit problem.
//
// Commits are serialized per workspace via explorerCommitMu so a burst of
// rapid saves queues on the git index instead of colliding on index.lock and
// silently dropping a commit.
func (h *Handler) commitExplorerWrite(workspace, resolvedFile string) {
	resolvedWS, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return
	}
	rel, err := filepath.Rel(resolvedWS, resolvedFile)
	if err != nil {
		return
	}

	muAny, _ := h.explorerCommitMu.LoadOrStore(resolvedWS, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), explorerCommitTimeout)
	defer cancel()
	if cerr := commitExplorerEdit(ctx, resolvedWS, rel); cerr != nil {
		logger.Handler.Warn("explorer auto-commit failed", "workspace", resolvedWS, "path", rel, "error", cerr)
	}
}
