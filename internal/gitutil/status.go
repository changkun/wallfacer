package gitutil

import (
	"path/filepath"
	"strconv"

	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// WorkspaceGitStatus holds the git state for a single workspace directory.
// It is serialized to JSON for the UI's git status panel.
type WorkspaceGitStatus struct {
	Path            string `json:"path"`
	Name            string `json:"name"`              // basename of Path, for display
	IsGitRepo       bool   `json:"is_git_repo"`       // false for non-git directories
	Branch          string `json:"branch,omitempty"`   // currently checked-out branch; empty for detached HEAD
	RemoteURL       string `json:"remote_url,omitempty"`
	HasRemote       bool   `json:"has_remote"`         // true if the current branch has an upstream tracking branch
	AheadCount      int    `json:"ahead_count"`        // local commits not yet pushed to upstream
	BehindCount     int    `json:"behind_count"`       // upstream commits not yet pulled
	MainBranch      string `json:"main_branch,omitempty"` // remote default branch (e.g. "main")
	BehindMainCount int    `json:"behind_main_count"`  // commits behind the remote's default branch
}

// WorkspaceStatus inspects a directory and returns its git status.
func WorkspaceStatus(path string) WorkspaceGitStatus {
	s := WorkspaceGitStatus{
		Path: path,
		Name: filepath.Base(path),
	}

	if err := cmdexec.Git(path, "rev-parse", "--git-dir").Run(); err != nil {
		return s
	}
	s.IsGitRepo = true

	if out, err := cmdexec.Git(path, "branch", "--show-current").Output(); err == nil {
		s.Branch = out
	}

	if out, err := cmdexec.Git(path, "remote", "get-url", "origin").Output(); err == nil {
		s.RemoteURL = out
	}

	// Check for a remote tracking branch (@{u} = upstream). If none is
	// configured, ahead/behind counts are meaningless so return early.
	if err := cmdexec.Git(path, "rev-parse", "--abbrev-ref", "@{u}").Run(); err != nil {
		return s
	}
	s.HasRemote = true

	// Count commits ahead of upstream (local commits not yet pushed).
	if out, err := cmdexec.Git(path, "rev-list", "--count", "@{u}..HEAD").Output(); err == nil {
		n, _ := strconv.Atoi(out)
		s.AheadCount = n
	}

	// Count commits behind upstream (remote commits not yet pulled).
	if out, err := cmdexec.Git(path, "rev-list", "--count", "HEAD..@{u}").Output(); err == nil {
		n, _ := strconv.Atoi(out)
		s.BehindCount = n
	}

	// Determine the remote's default branch (e.g. "main") and how many
	// commits the current branch is behind it. Skipped when already on
	// the main branch itself, since behind-main would always be 0.
	mainBranch := RemoteDefaultBranch(path)
	s.MainBranch = mainBranch
	if s.Branch != "" && s.Branch != mainBranch {
		if out, err := cmdexec.Git(path, "rev-list", "--count", "HEAD..origin/"+mainBranch).Output(); err == nil {
			n, _ := strconv.Atoi(out)
			s.BehindMainCount = n
		}
	}

	return s
}
