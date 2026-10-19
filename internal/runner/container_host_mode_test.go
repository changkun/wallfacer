package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/harness"
)

// newHostModeRunner creates a Runner for testing buildContainerSpecForSandbox
// without needing a real HostBackend (which requires a resolvable claude/codex
// binary).
func newHostModeRunner(t *testing.T, cfg RunnerConfig) *Runner {
	t.Helper()
	return newRunnerForArgTest(t, cfg)
}

func TestBuildContainerSpec_HostMode_WorkDirIsHostPath(t *testing.T) {
	workspace := t.TempDir()
	worktree := t.TempDir()
	r := newHostModeRunner(t, RunnerConfig{
		Workspaces:   []string{workspace},
		WorktreesDir: t.TempDir(),
	})

	// With a worktree override, CWD must use the worktree path.
	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "do work", "",
		map[string]string{workspace: worktree},
		"", "", harness.Claude,
	)

	if spec.WorkDir != worktree {
		t.Errorf("WorkDir = %q; want %q (worktree override)", spec.WorkDir, worktree)
	}
	if strings.HasPrefix(spec.WorkDir, "/workspace") {
		t.Errorf("host-mode WorkDir leaked container path: %q", spec.WorkDir)
	}
}

// TestBuildContainerSpec_HostMode_WorkDirStaysInTaskWorktreeAfterRepoint
// covers a task whose worktree belongs to a folder the workspace no longer
// points at (the workspace was re-pointed after the task started). The agent
// must work in the task's worktree, never in the live workspace folder, where
// its edits would bypass the task branch.
func TestBuildContainerSpec_HostMode_WorkDirStaysInTaskWorktreeAfterRepoint(t *testing.T) {
	oldFolder := t.TempDir()
	worktree := t.TempDir()
	repointed := t.TempDir()
	r := newHostModeRunner(t, RunnerConfig{
		Workspaces:   []string{repointed},
		WorktreesDir: t.TempDir(),
	})

	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "do work", "",
		map[string]string{oldFolder: worktree},
		"", "", harness.Claude,
	)

	if spec.WorkDir != worktree {
		t.Errorf("WorkDir = %q; want the task worktree %q, not the live folder %q", spec.WorkDir, worktree, repointed)
	}
}

// TestAgentWorkDir_SeveralWorktrees covers a stored task with worktrees in
// several folders (written when a workspace could span several folders): the
// agent works in the worktree for the current workspace folder when the task
// has one, otherwise in the first folder's worktree in sorted order. A task
// without worktrees works in the workspace folder.
func TestAgentWorkDir_SeveralWorktrees(t *testing.T) {
	worktrees := map[string]string{
		"/repos/api": "/wt/api",
		"/repos/web": "/wt/web",
	}
	cases := []struct {
		name      string
		folder    string
		worktrees map[string]string
		want      string
	}{
		{"current folder has a worktree", "/repos/web", worktrees, "/wt/web"},
		{"current folder has no worktree", "/repos/other", worktrees, "/wt/api"},
		{"no workspace open", "", worktrees, "/wt/api"},
		{"task has no worktree", "/repos/web", nil, "/repos/web"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ws []string
			if tc.folder != "" {
				ws = []string{tc.folder}
			}
			r := newHostModeRunner(t, RunnerConfig{Workspaces: ws, WorktreesDir: t.TempDir()})
			if got := r.agentWorkDir(tc.worktrees); got != tc.want {
				t.Errorf("agentWorkDir = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBuildContainerSpec_HostMode_BoardViaEnv verifies that the board manifest
// reaches the agent through WALLFACER_BOARD_JSON and that nothing else is
// written next to it: other tasks' worktrees are not exposed to the agent.
func TestBuildContainerSpec_HostMode_BoardViaEnv(t *testing.T) {
	workspace := t.TempDir()
	boardDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(boardDir, "board.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newHostModeRunner(t, RunnerConfig{
		Workspaces:   []string{workspace},
		WorktreesDir: t.TempDir(),
	})

	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "do work", "",
		nil, boardDir, "", harness.Claude,
	)

	if got := spec.Env["WALLFACER_BOARD_JSON"]; got != filepath.Join(boardDir, "board.json") {
		t.Errorf("WALLFACER_BOARD_JSON = %q; want %q", got, filepath.Join(boardDir, "board.json"))
	}
	if _, ok := spec.Env["WALLFACER_SIBLING_WORKTREES_JSON"]; ok {
		t.Errorf("WALLFACER_SIBLING_WORKTREES_JSON is set; other tasks' worktrees must not be exposed: %+v", spec.Env)
	}
	entries, err := os.ReadDir(boardDir)
	if err != nil {
		t.Fatalf("read board dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("board dir holds %d entries, want only board.json", len(entries))
	}
}

func TestBuildContainerSpec_HostMode_CmdContainsPromptAndResume(t *testing.T) {
	workspace := t.TempDir()
	r := newHostModeRunner(t, RunnerConfig{
		Workspaces:   []string{workspace},
		WorktreesDir: t.TempDir(),
	})

	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "the task", "sess-42",
		nil, "", "", harness.Claude,
	)

	// Expect -p <prompt> and --resume <sessionID> in Cmd.
	joined := strings.Join(spec.Cmd, " ")
	if !strings.Contains(joined, "-p the task") {
		t.Errorf("Cmd missing -p: %v", spec.Cmd)
	}
	if !strings.Contains(joined, "--resume sess-42") {
		t.Errorf("Cmd missing --resume: %v", spec.Cmd)
	}
}
