package gitutil

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// ErrConflict is returned by RebaseOntoDefault when a merge conflict is detected.
var ErrConflict = errors.New("rebase conflict")

// ConflictError is returned by RebaseOntoDefault when a merge conflict is detected.
// It wraps ErrConflict and carries the list of conflicted file paths.
type ConflictError struct {
	WorktreePath    string
	ConflictedFiles []string
	RawOutput       string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("rebase conflict in %s: %d file(s) conflicted", e.WorktreePath, len(e.ConflictedFiles))
}

func (e *ConflictError) Unwrap() error { return ErrConflict }

var conflictFileRe = regexp.MustCompile(`CONFLICT \([^)]+\): (?:Merge conflict in|content conflict in) (.+)$`)

// parseConflictedFiles extracts conflicted file paths from git rebase output.
func parseConflictedFiles(output string) []string {
	var files []string
	for _, line := range strings.Split(output, "\n") {
		if m := conflictFileRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			files = append(files, m[1])
		}
	}
	return files
}

// IsRebaseNeedsMergeOutput reports if git output likely indicates the repo is
// stuck in or blocked by an active/previous rebase or merge state.
func IsRebaseNeedsMergeOutput(s string) bool {
	ls := strings.ToLower(s)
	return strings.Contains(ls, "needs merge") ||
		strings.Contains(ls, "you have not concluded your merge") ||
		strings.Contains(ls, "please commit your changes before you can rebase") ||
		strings.Contains(ls, "could not rebase") ||
		strings.Contains(ls, "unable to rebase") ||
		strings.Contains(ls, "rebase in progress") ||
		strings.Contains(ls, "another rebase-apply") ||
		strings.Contains(ls, "resolve your current index first") ||
		strings.Contains(ls, "merge in progress")
}

// IsGitRepo reports whether path is inside a git repository.
func IsGitRepo(path string) bool {
	return exec.Command("git", "-C", path, "rev-parse", "--git-dir").Run() == nil
}

// HasCommits reports whether the repository at path has at least one commit.
// Returns false for empty repos (git init with no commits) and non-git directories.
func HasCommits(path string) bool {
	return exec.Command("git", "-C", path, "rev-parse", "--verify", "HEAD").Run() == nil
}

// DefaultBranch returns the default branch name for a repo (tries the current
// local HEAD branch first, falls back to origin/HEAD, then "main").
func DefaultBranch(repoPath string) (string, error) {
	// Prefer the currently checked-out branch so that tasks merge back to
	// whatever branch the user is working on (e.g. "develop"), not the
	// remote's default (which is typically "main").
	out, err := exec.Command("git", "-C", repoPath, "branch", "--show-current").Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "" {
			return branch, nil
		}
	}
	// Detached HEAD — fall back to origin/HEAD (most reliable for cloned repos).
	out, err = exec.Command("git", "-C", repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		// output is e.g. "origin/main" — strip the "origin/" prefix.
		branch := strings.TrimSpace(strings.TrimPrefix(string(out), "origin/"))
		if branch != "" && branch != string(out) {
			return branch, nil
		}
	}
	return "main", nil
}

// RemoteDefaultBranch returns the default branch of the "origin" remote
// (e.g. "main" or "master"). It does NOT consider the current checkout.
func RemoteDefaultBranch(repoPath string) string {
	out, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		branch := strings.TrimSpace(strings.TrimPrefix(string(out), "origin/"))
		if branch != "" && branch != strings.TrimSpace(string(out)) {
			return branch
		}
	}
	if exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "origin/main").Run() == nil {
		return "main"
	}
	if exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "origin/master").Run() == nil {
		return "master"
	}
	return "main"
}

// GetCommitHash returns the current HEAD commit hash in repoPath.
func GetCommitHash(repoPath string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD in %s: %w", repoPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetCommitHashForRef returns the commit hash for a specific ref in repoPath.
func GetCommitHashForRef(repoPath, ref string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", ref).Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s in %s: %w", ref, repoPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}
