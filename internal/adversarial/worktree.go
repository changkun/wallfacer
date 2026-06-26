package adversarial

import (
	"path/filepath"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/gitutil"
)

// newCriticWorktree creates a throwaway git worktree at the current HEAD of
// srcWorktree so an agon critic can read the codebase and run tests in
// isolation. This matters because wallfacer's claude harness runs every agent
// with --dangerously-skip-permissions: a critic pointed at the real worktree
// could write to it and run tests that dirty the tree, which the commit
// pipeline's `git add -A` would then stage into the task branch. The critic
// runs in this disposable copy instead; cleanup removes it.
//
// The worktree is a sibling of srcWorktree (in the shared per-task dir), so it
// is never inside the real worktree and is removed by worktree GC if cleanup is
// missed. cleanup is always safe to call (no-op on partial failure).
func newCriticWorktree(srcWorktree string) (path string, cleanup func(), err error) {
	noop := func() {}
	head, err := gitutil.ResolveHead(srcWorktree)
	if err != nil {
		return "", noop, err
	}
	id := uuid.NewString()[:8]
	path = filepath.Join(filepath.Dir(srcWorktree), ".agon-critic-"+id)
	branch := "agon-critic-" + id
	if err := gitutil.CreateWorktreeAt(srcWorktree, path, branch, head); err != nil {
		return "", noop, err
	}
	return path, func() { _ = gitutil.RemoveWorktree(srcWorktree, path, branch) }, nil
}
