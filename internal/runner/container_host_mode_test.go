package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/sandbox"
)

// newHostModeRunner creates a Runner with hostMode forced on for testing
// buildContainerSpecForSandbox's host-mode branch without needing a real
// HostBackend (which requires a resolvable claude/codex binary).
func newHostModeRunner(t *testing.T, cfg RunnerConfig) *Runner {
	t.Helper()
	r := newRunnerForArgTest(t, cfg)
	r.hostMode = true
	return r
}

func TestBuildContainerSpec_HostMode_NoMounts(t *testing.T) {
	workDir := t.TempDir()
	r := newHostModeRunner(t, RunnerConfig{
		Command:      "echo",
		SandboxImage: "test:latest",
		Workspaces:   []string{workDir},
		WorktreesDir: t.TempDir(),
	})

	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "do work", "",
		nil, "", nil, "", sandbox.Claude,
	)

	if len(spec.Volumes) != 0 {
		t.Errorf("expected no volumes in host mode, got %d: %+v", len(spec.Volumes), spec.Volumes)
	}
	// Check no /workspace/* container paths slipped through anywhere.
	for _, v := range spec.Volumes {
		if strings.HasPrefix(v.Container, "/workspace") {
			t.Errorf("host mode leaked container path: %q", v.Container)
		}
	}
}

func TestBuildContainerSpec_HostMode_WorkDirIsHostPath(t *testing.T) {
	workspace := t.TempDir()
	worktree := t.TempDir()
	r := newHostModeRunner(t, RunnerConfig{
		Command:      "echo",
		SandboxImage: "test:latest",
		Workspaces:   []string{workspace},
		WorktreesDir: t.TempDir(),
	})

	// With a worktree override, CWD must use the worktree path.
	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "do work", "",
		map[string]string{workspace: worktree},
		"", nil, "", sandbox.Claude,
	)

	if spec.WorkDir != worktree {
		t.Errorf("WorkDir = %q; want %q (worktree override)", spec.WorkDir, worktree)
	}
	if strings.HasPrefix(spec.WorkDir, "/workspace") {
		t.Errorf("host-mode WorkDir leaked container path: %q", spec.WorkDir)
	}
}

func TestBuildContainerSpec_HostMode_InstructionsEnv(t *testing.T) {
	workspace := t.TempDir()
	instr := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(instr, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newHostModeRunner(t, RunnerConfig{
		Command:          "echo",
		SandboxImage:     "test:latest",
		Workspaces:       []string{workspace},
		WorktreesDir:     t.TempDir(),
		InstructionsPath: instr,
	})

	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "do work", "",
		nil, "", nil, "", sandbox.Claude,
	)

	if spec.Env["WALLFACER_INSTRUCTIONS_PATH"] != instr {
		t.Errorf("WALLFACER_INSTRUCTIONS_PATH = %q; want %q", spec.Env["WALLFACER_INSTRUCTIONS_PATH"], instr)
	}
}

func TestBuildContainerSpec_HostMode_BoardAndSiblingsViaEnv(t *testing.T) {
	workspace := t.TempDir()
	boardDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(boardDir, "board.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newHostModeRunner(t, RunnerConfig{
		Command:      "echo",
		SandboxImage: "test:latest",
		Workspaces:   []string{workspace},
		WorktreesDir: t.TempDir(),
	})

	siblings := map[string]map[string]string{
		"aaaa1111": {"/repo/a": "/host/worktree-a"},
		"bbbb2222": {"/repo/b": "/host/worktree-b"},
	}

	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "do work", "",
		nil, boardDir, siblings, "", sandbox.Claude,
	)

	if got := spec.Env["WALLFACER_BOARD_JSON"]; got != filepath.Join(boardDir, "board.json") {
		t.Errorf("WALLFACER_BOARD_JSON = %q; want %q", got, filepath.Join(boardDir, "board.json"))
	}

	manifestPath := spec.Env["WALLFACER_SIBLING_WORKTREES_JSON"]
	if manifestPath == "" {
		t.Fatal("WALLFACER_SIBLING_WORKTREES_JSON not set when siblings are present")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var got map[string]map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if got["aaaa1111"]["/repo/a"] != "/host/worktree-a" {
		t.Errorf("manifest content wrong: %+v", got)
	}
}

func TestBuildContainerSpec_HostMode_NoSiblingsSkipsManifest(t *testing.T) {
	workspace := t.TempDir()
	boardDir := t.TempDir()
	r := newHostModeRunner(t, RunnerConfig{
		Command:      "echo",
		SandboxImage: "test:latest",
		Workspaces:   []string{workspace},
		WorktreesDir: t.TempDir(),
	})

	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "do work", "",
		nil, boardDir, nil, "", sandbox.Claude,
	)

	if _, ok := spec.Env["WALLFACER_SIBLING_WORKTREES_JSON"]; ok {
		t.Errorf("manifest env var should not be set when no siblings exist; got %+v", spec.Env)
	}
	// No manifest file should be written either.
	if _, err := os.Stat(filepath.Join(boardDir, "sibling_worktrees.json")); err == nil {
		t.Error("manifest file should not be written when no siblings exist")
	}
}

func TestBuildContainerSpec_HostMode_EntrypointCleared(t *testing.T) {
	workspace := t.TempDir()
	r := newHostModeRunner(t, RunnerConfig{
		Command:      "echo",
		SandboxImage: "test:latest",
		Workspaces:   []string{workspace},
		WorktreesDir: t.TempDir(),
	})

	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "do work", "",
		nil, "", nil, "", sandbox.Claude,
	)

	if spec.Entrypoint != "" {
		t.Errorf("host-mode Entrypoint = %q; want empty (no dispatcher in host mode)", spec.Entrypoint)
	}
}

func TestBuildContainerSpec_HostMode_CmdContainsPromptAndResume(t *testing.T) {
	workspace := t.TempDir()
	r := newHostModeRunner(t, RunnerConfig{
		Command:      "echo",
		SandboxImage: "test:latest",
		Workspaces:   []string{workspace},
		WorktreesDir: t.TempDir(),
	})

	spec := r.buildContainerSpecForSandbox(
		"wallfacer-host", "task-1", "the task", "sess-42",
		nil, "", nil, "", sandbox.Claude,
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

func TestBuildContainerSpec_LocalMode_StillGetsContainerPaths(t *testing.T) {
	// Regression guard: the default container path must still produce
	// /workspace/* paths (host-mode branch must not leak into container mode).
	workspace := t.TempDir()
	r := newRunnerForArgTest(t, RunnerConfig{
		Command:      "podman",
		SandboxImage: "test:latest",
		Workspaces:   []string{workspace},
		WorktreesDir: t.TempDir(),
	})

	spec := r.buildContainerSpecForSandbox(
		"wallfacer-local", "task-1", "do work", "",
		nil, "", nil, "", sandbox.Claude,
	)

	found := false
	for _, v := range spec.Volumes {
		if strings.HasPrefix(v.Container, "/workspace/") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("local mode should produce /workspace/* volume mounts; got %+v", spec.Volumes)
	}
	if spec.WorkDir == "" || !strings.HasPrefix(spec.WorkDir, "/workspace") {
		t.Errorf("local-mode WorkDir should be a container path; got %q", spec.WorkDir)
	}
}
