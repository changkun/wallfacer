package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
)

// newRunnerForArgTest creates a Runner for testing arg-building functions.
// It does not need a real container runtime; the store is backed by a temp dir.
func newRunnerForArgTest(t *testing.T, cfg RunnerConfig) *Runner {
	t.Helper()
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if cfg.WorktreesDir == "" {
		cfg.WorktreesDir = t.TempDir()
	}
	r := NewRunner(s, cfg)
	t.Cleanup(func() { r.Shutdown() })
	return r
}

// argsContainSubstring returns true if any element of args contains sub.
func argsContainSubstring(args []string, sub string) bool {
	for _, a := range args {
		if strings.Contains(a, sub) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// buildBaseContainerSpec — table-driven parity tests
// ---------------------------------------------------------------------------

// TestResolveEnvFileFallback covers the env-file resolution guard that keeps
// long-idle scheduled tasks alive when the configured env file path has
// vanished by launch time (e.g. a mktemp ENV_FILE under /var/folders that
// macOS's tmp-reaper purges after a few idle days). Without the fallback,
// buildBaseContainerSpec hands podman a dead --env-file path and the task dies
// with an opaque exit 125.
func TestResolveEnvFileFallback(t *testing.T) {
	// A real, on-disk default config env file (the survivor).
	defaultEnv := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(defaultEnv, []byte("CLAUDE_CODE_OAUTH_TOKEN=tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A configured override path that exists now but is removed below to
	// simulate the tmp-reaper deleting it out from under a running server.
	reaped := filepath.Join(t.TempDir(), "tmp.reaped")
	if err := os.WriteFile(reaped, []byte("X=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A path that never exists (neither configured nor default usable).
	gone := filepath.Join(t.TempDir(), "gone.env")

	tests := []struct {
		name       string
		envFile    string
		defaultEnv string
		setup      func()
		want       string
	}{
		{
			name:       "configured missing, default present -> falls back to default",
			envFile:    reaped,
			defaultEnv: defaultEnv,
			setup:      func() { _ = os.Remove(reaped) },
			want:       defaultEnv,
		},
		{
			name:       "configured present -> used verbatim, default ignored",
			envFile:    defaultEnv,
			defaultEnv: filepath.Join(t.TempDir(), "other.env"),
			want:       defaultEnv,
		},
		{
			name:       "configured missing, no usable default -> passes through unchanged",
			envFile:    gone,
			defaultEnv: "",
			want:       gone,
		},
		{
			name:       "empty config -> empty (no --env-file)",
			envFile:    "",
			defaultEnv: defaultEnv,
			want:       "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup()
			}
			r := newRunnerForArgTest(t, RunnerConfig{
				Command:        "podman",
				SandboxImage:   "sandbox-agents:latest",
				EnvFile:        tc.envFile,
				DefaultEnvFile: tc.defaultEnv,
			})

			if got := r.resolveEnvFile(); got != tc.want {
				t.Errorf("resolveEnvFile() = %q; want %q", got, tc.want)
			}

			// The spec built for every activity must carry the resolved path.
			spec := r.buildBaseContainerSpec("c-test", "", "claude")
			if spec.EnvFile != tc.want {
				t.Errorf("spec.EnvFile = %q; want %q", spec.EnvFile, tc.want)
			}
		})
	}
}

// TestBuildBaseContainerSpecClaudeVsCodexAgentEnv verifies that both
// claude and codex sandboxes share the unified sandbox-agents image and
// differ only in the WALLFACER_AGENT env var that the entrypoint reads
// to dispatch to the right CLI.
func TestBuildBaseContainerSpecClaudeVsCodexAgentEnv(t *testing.T) {
	r := newRunnerForArgTest(t, RunnerConfig{Command: "podman", SandboxImage: "sandbox-agents:latest"})

	claudeSpec := r.buildBaseContainerSpec("c-test", "", "claude")
	codexSpec := r.buildBaseContainerSpec("c-test", "", "codex")

	if claudeSpec.Env["WALLFACER_AGENT"] != "claude" {
		t.Errorf("claude spec: WALLFACER_AGENT = %q, want claude", claudeSpec.Env["WALLFACER_AGENT"])
	}
	if codexSpec.Env["WALLFACER_AGENT"] != "codex" {
		t.Errorf("codex spec: WALLFACER_AGENT = %q, want codex", codexSpec.Env["WALLFACER_AGENT"])
	}
}

// ---------------------------------------------------------------------------
// containerGitPointerFile — unit tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// buildIdeationContainerArgs — table-driven parity tests
// ---------------------------------------------------------------------------

// TestBuildIdeationContainerArgsSingleWorkspaceReadOnly verifies that the
// single workspace mount uses ":z,ro" (read-only) and the workdir points into
// that workspace.

// TestBuildIdeationContainerArgsSingleWorkspaceInstructionsInsideRepo verifies
// that for a single workspace, the instructions file (CLAUDE.md / AGENTS.md)
// is mounted inside the repo directory rather than at /workspace/ root, so the
// agent stays anchored to the repo.

// TestBuildIdeationContainerArgsClaudeVsCodex verifies that running ideation
// with claude vs codex sandboxes produces the correct sandbox image while
// keeping all other flags identical.

// ---------------------------------------------------------------------------
// buildAgentCmd — unit tests
// ---------------------------------------------------------------------------

func TestBuildAgentCmd(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		model    string
		wantArgs []string
		wantPair []string // [flag, value] consecutive pair
	}{
		{
			name:     "no model",
			prompt:   "do something",
			model:    "",
			wantArgs: []string{"-p", "do something", "--verbose", "--output-format", "stream-json"},
		},
		{
			name:   "with model",
			prompt: "do something",
			model:  "claude-opus-4-6",
			wantArgs: []string{
				"-p", "do something", "--verbose", "--output-format", "stream-json",
				"--model", "claude-opus-4-6",
			},
		},
		{
			name:   "verbose appears before output-format",
			prompt: "p",
			model:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildAgentCmd(tc.prompt, tc.model)
			if tc.wantArgs != nil {
				for i, want := range tc.wantArgs {
					if i >= len(got) || got[i] != want {
						t.Errorf("arg[%d]: got %q, want %q; full: %v", i, got[i], want, got)
					}
				}
				if len(got) != len(tc.wantArgs) {
					t.Errorf("len mismatch: got %d args, want %d; args: %v", len(got), len(tc.wantArgs), got)
				}
			}
			// --verbose must appear before --output-format.
			verboseIdx, outfmtIdx := -1, -1
			for i, a := range got {
				if a == "--verbose" {
					verboseIdx = i
				}
				if a == "--output-format" {
					outfmtIdx = i
				}
			}
			if verboseIdx == -1 || outfmtIdx == -1 {
				t.Fatalf("--verbose or --output-format not found; args: %v", got)
			}
			if verboseIdx > outfmtIdx {
				t.Errorf("--verbose (%d) must appear before --output-format (%d); args: %v", verboseIdx, outfmtIdx, got)
			}
		})
	}
}

// TestBuildAgentCmdModelInjectionConsistency verifies that buildAgentCmd
// injects --model identically for claude and codex sandboxes (the model
// string itself differs; the injection pattern does not).
func TestBuildAgentCmdModelInjectionConsistency(t *testing.T) {
	claudeCmd := buildAgentCmd("prompt", "claude-opus-4-6")
	codexCmd := buildAgentCmd("prompt", "codex-model-v1")

	for _, args := range [][]string{claudeCmd, codexCmd} {
		if !containsConsecutive(args, "-p", "prompt") {
			t.Errorf("expected -p prompt; args: %v", args)
		}
		if !argsContainSubstring(args, "--verbose") {
			t.Errorf("expected --verbose; args: %v", args)
		}
		if !containsConsecutive(args, "--output-format", "stream-json") {
			t.Errorf("expected --output-format stream-json; args: %v", args)
		}
	}
	if !containsConsecutive(claudeCmd, "--model", "claude-opus-4-6") {
		t.Errorf("claude cmd: expected --model claude-opus-4-6; args: %v", claudeCmd)
	}
	if !containsConsecutive(codexCmd, "--model", "codex-model-v1") {
		t.Errorf("codex cmd: expected --model codex-model-v1; args: %v", codexCmd)
	}
}

// ---------------------------------------------------------------------------
// workdirForBasenames — unit tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// appendInstructionsMount — unit tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Commit and title invocation patterns via buildBaseContainerSpec + buildAgentCmd
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// ContainerSpec CPU/memory resource limit flags
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// executor.RunArgs call-path: verify args received by the mock
// ---------------------------------------------------------------------------
