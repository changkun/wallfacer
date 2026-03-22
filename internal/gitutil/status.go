package gitutil

import (
	"path/filepath"
	"strconv"

	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// WorkspaceGitStatus holds the git state for a single workspace directory.
type WorkspaceGitStatus struct {
	Path            string `json:"path"`
	Name            string `json:"name"`
	IsGitRepo       bool   `json:"is_git_repo"`
	Branch          string `json:"branch,omitempty"`
	RemoteURL       string `json:"remote_url,omitempty"`
	HasRemote       bool   `json:"has_remote"`
	AheadCount      int    `json:"ahead_count"`
	BehindCount     int    `json:"behind_count"`
	MainBranch      string `json:"main_branch,omitempty"`
	BehindMainCount int    `json:"behind_main_count"`
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

	// Does it have a remote tracking branch?
	if err := cmdexec.Git(path, "rev-parse", "--abbrev-ref", "@{u}").Run(); err != nil {
		return s
	}
	s.HasRemote = true

	if out, err := cmdexec.Git(path, "rev-list", "--count", "@{u}..HEAD").Output(); err == nil {
		n, _ := strconv.Atoi(out)
		s.AheadCount = n
	}

	if out, err := cmdexec.Git(path, "rev-list", "--count", "HEAD..@{u}").Output(); err == nil {
		n, _ := strconv.Atoi(out)
		s.BehindCount = n
	}

	// Determine the remote's default branch and how far behind we are.
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
