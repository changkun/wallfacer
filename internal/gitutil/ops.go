package gitutil

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// RebaseOntoDefault rebases the task branch (currently checked out in worktreePath)
// onto the default branch of repoPath. On conflict it aborts the rebase and returns
// ErrConflict so the caller can invoke conflict resolution and retry.
func RebaseOntoDefault(repoPath, worktreePath string) error {
	defBranch, err := DefaultBranch(repoPath)
	if err != nil {
		return err
	}
	out, err := exec.Command("git", "-C", worktreePath, "rebase", defBranch).CombinedOutput()
	if err != nil {
		// Abort so the repo is not stuck mid-rebase.
		exec.Command("git", "-C", worktreePath, "rebase", "--abort").Run()
		if IsConflictOutput(string(out)) {
			return &ConflictError{
				WorktreePath:    worktreePath,
				ConflictedFiles: parseConflictedFiles(string(out)),
				RawOutput:       string(out),
			}
		}
		return fmt.Errorf("git rebase in %s: %w\n%s", worktreePath, err, out)
	}
	return nil
}

// FFMerge fast-forward merges branchName into the default branch of repoPath.
func FFMerge(repoPath, branchName string) error {
	defBranch, err := DefaultBranch(repoPath)
	if err != nil {
		return err
	}
	if out, err := exec.Command("git", "-C", repoPath, "checkout", defBranch).CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s in %s: %w\n%s", defBranch, repoPath, err, out)
	}
	out, err := exec.Command("git", "-C", repoPath, "merge", "--ff-only", branchName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git merge --ff-only %s in %s: %w\n%s", branchName, repoPath, err, out)
	}
	return nil
}

// CommitsBehind returns the number of commits the default branch has ahead of
// the worktree's HEAD (i.e. how many commits the task branch is behind).
func CommitsBehind(repoPath, worktreePath string) (int, error) {
	defBranch, err := DefaultBranch(repoPath)
	if err != nil {
		return 0, err
	}
	out, err := exec.Command(
		"git", "-C", worktreePath,
		"rev-list", "--count", "HEAD.."+defBranch,
	).Output()
	if err != nil {
		return 0, fmt.Errorf("git rev-list in %s: %w", worktreePath, err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n, nil
}

// HasCommitsAheadOf reports whether worktreePath has commits not yet in baseBranch.
func HasCommitsAheadOf(worktreePath, baseBranch string) (bool, error) {
	out, err := exec.Command(
		"git", "-C", worktreePath,
		"rev-list", "--count", baseBranch+"..HEAD",
	).Output()
	if err != nil {
		return false, fmt.Errorf("git rev-list in %s: %w", worktreePath, err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n > 0, nil
}

// MergeBase returns the best common ancestor (merge-base) of two refs,
// evaluated in the given repository/worktree path.
func MergeBase(repoPath, ref1, ref2 string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "merge-base", ref1, ref2).Output()
	if err != nil {
		return "", fmt.Errorf("git merge-base %s %s in %s: %w", ref1, ref2, repoPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// BranchTipCommit returns the hash, subject, and author timestamp of the most
// recent commit on branch in repoPath. It runs:
//
//	git -C <repoPath> log -1 --format=%H|%s|%aI <branch>
//
// Returns an error if the branch does not exist or the path is not a git repo.
func BranchTipCommit(repoPath, branch string) (hash, subject string, ts time.Time, err error) {
	out, cmdErr := exec.Command(
		"git", "-C", repoPath,
		"log", "-1", "--format=%H|%s|%aI", branch,
	).Output()
	if cmdErr != nil {
		err = fmt.Errorf("git log in %s for branch %s: %w", repoPath, branch, cmdErr)
		return
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		err = fmt.Errorf("branch %s not found or has no commits in %s", branch, repoPath)
		return
	}
	parts := strings.SplitN(line, "|", 3)
	if len(parts) != 3 {
		err = fmt.Errorf("unexpected git log output %q in %s", line, repoPath)
		return
	}
	hash = parts[0]
	subject = parts[1]
	ts, err = time.Parse(time.RFC3339, parts[2])
	if err != nil {
		err = fmt.Errorf("parse commit timestamp %q: %w", parts[2], err)
	}
	return
}

// IsConflictOutput reports whether git output text indicates a merge conflict.
func IsConflictOutput(s string) bool {
	return strings.Contains(s, "CONFLICT") ||
		strings.Contains(s, "Merge conflict") ||
		strings.Contains(s, "conflict")
}

// HasConflicts reports whether the worktree at worktreePath has any unresolved
// merge/rebase conflicts (files with conflict-marker status codes in git status).
func HasConflicts(worktreePath string) (bool, error) {
	out, err := exec.Command("git", "-C", worktreePath, "status", "--porcelain").Output()
	if err != nil {
		return false, fmt.Errorf("git status in %s: %w", worktreePath, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		// Conflict status codes: UU, AA, DD, AU, UA, DU, UD
		xy := line[:2]
		switch xy {
		case "UU", "AA", "DD", "AU", "UA", "DU", "UD":
			return true, nil
		}
	}
	return false, nil
}
