// Package gitutil provides low-level git operations used for repository
// management, worktree isolation, and rebase conflict handling.
//
// It wraps the git CLI via [changkun.de/x/wallfacer/internal/pkg/cmdexec] and
// exposes structured results and error types. Operations include repository
// validation, branch discovery, worktree creation and removal, rebase with
// automatic conflict detection and recovery, stash management, and remote
// synchronization. The [ConflictError] type carries conflicted file lists for
// programmatic conflict resolution.
//
// # Connected packages
//
// Depends on [changkun.de/x/wallfacer/internal/pkg/cmdexec] for command execution.
// Consumed by [handler] (git status, push, sync, rebase UI actions) and [runner]
// (worktree lifecycle, commit pipeline, rebase during execution). Changes to git
// command behavior or error parsing affect both the execution engine and the UI
// git controls.
//
// # Usage
//
//	if gitutil.IsGitRepo(path) {
//	    branch, _ := gitutil.DefaultBranch(path)
//	    err := gitutil.CreateWorktree(repoPath, wtPath, "task-branch")
//	    if err := gitutil.RebaseOntoDefault(repoPath, wtPath); err != nil {
//	        var ce *gitutil.ConflictError
//	        if errors.As(err, &ce) {
//	            // handle conflicts in ce.Files
//	        }
//	    }
//	}
package gitutil
