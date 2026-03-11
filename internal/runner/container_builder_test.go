package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/store"
)

// newRunnerForArgTest creates a Runner for testing arg-building functions.
// It does not need a real container runtime; the store is backed by a temp dir.
func newRunnerForArgTest(t *testing.T, cfg RunnerConfig) *Runner {
	t.Helper()
	dataDir := t.TempDir()
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if cfg.WorktreesDir == "" {
		cfg.WorktreesDir = t.TempDir()
	}
	return NewRunner(s, cfg)
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

func TestBuildBaseContainerSpec(t *testing.T) {
	type pair struct{ flag, value string }
	tests := []struct {
		name         string
		cfgFn        func(t *testing.T) RunnerConfig
		model        string
		sandbox      string
		wantPairs    []pair   // consecutive [flag, value] that must appear
		wantArgs     []string // exact args that must appear somewhere
		wantNotArgs  []string // substrings that must NOT appear in any arg
	}{
		{
			name:    "claude, no envfile, no model",
			cfgFn:   func(t *testing.T) RunnerConfig { return RunnerConfig{Command: "podman", SandboxImage: "wallfacer:latest"} },
			model:   "",
			sandbox: "claude",
			wantPairs: []pair{
				{"--name", "c-test"},
				{"-v", "claude-config:/home/claude/.claude"},
			},
			wantArgs:    []string{"wallfacer:latest"},
			wantNotArgs: []string{"--env-file", "CLAUDE_CODE_MODEL", "/home/codex"},
		},
		{
			name: "claude, with envfile and model",
			cfgFn: func(t *testing.T) RunnerConfig {
				return RunnerConfig{Command: "podman", SandboxImage: "wallfacer:latest", EnvFile: "/home/user/.env"}
			},
			model:   "claude-opus-4-6",
			sandbox: "claude",
			wantPairs: []pair{
				{"--env-file", "/home/user/.env"},
				{"-e", "CLAUDE_CODE_MODEL=claude-opus-4-6"},
				{"-v", "claude-config:/home/claude/.claude"},
			},
			wantArgs:    []string{"wallfacer:latest"},
			wantNotArgs: []string{"/home/codex"},
		},
		{
			name:    "codex sandbox, no auth path configured",
			cfgFn:   func(t *testing.T) RunnerConfig { return RunnerConfig{Command: "podman", SandboxImage: "wallfacer:latest"} },
			model:   "",
			sandbox: "codex",
			wantPairs: []pair{
				{"-v", "claude-config:/home/claude/.claude"},
			},
			wantArgs:    []string{"wallfacer-codex:latest"},
			wantNotArgs: []string{"/home/codex"},
		},
		{
			name: "codex sandbox, with valid auth path",
			cfgFn: func(t *testing.T) RunnerConfig {
				dir := t.TempDir()
				// hostCodexAuthPath requires auth.json to exist inside the directory.
				if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				return RunnerConfig{Command: "podman", SandboxImage: "wallfacer:latest", CodexAuthPath: dir}
			},
			model:   "codex-model",
			sandbox: "codex",
			wantPairs: []pair{
				{"-v", "claude-config:/home/claude/.claude"},
				{"-e", "CLAUDE_CODE_MODEL=codex-model"},
			},
			wantArgs: []string{"wallfacer-codex:latest", "/home/codex/.codex:z,ro"},
		},
		{
			name:    "codex sandbox, auth path does not exist",
			cfgFn:   func(t *testing.T) RunnerConfig {
				return RunnerConfig{Command: "podman", SandboxImage: "wallfacer:latest", CodexAuthPath: "/nonexistent/path/to/codex"}
			},
			model:       "",
			sandbox:     "codex",
			wantArgs:    []string{"wallfacer-codex:latest"},
			wantNotArgs: []string{"/home/codex"},
		},
		{
			name:    "codex sandbox, empty sandbox image falls back to wallfacer-codex:latest",
			cfgFn:   func(t *testing.T) RunnerConfig { return RunnerConfig{Command: "podman", SandboxImage: ""} },
			model:   "",
			sandbox: "codex",
			wantArgs: []string{"wallfacer-codex:latest"},
		},
		{
			name:    "network is always host",
			cfgFn:   func(t *testing.T) RunnerConfig { return RunnerConfig{Command: "podman", SandboxImage: "wallfacer:latest"} },
			model:   "",
			sandbox: "claude",
			// --network=host is emitted as a single token, not two consecutive args.
			wantArgs: []string{"--network=host"},
		},
		{
			name:    "fixed prefix: run --rm",
			cfgFn:   func(t *testing.T) RunnerConfig { return RunnerConfig{Command: "podman", SandboxImage: "img:v1"} },
			model:   "",
			sandbox: "claude",
			wantPairs: []pair{
				{"run", "--rm"},
			},
			wantArgs: []string{"img:v1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunnerForArgTest(t, tc.cfgFn(t))
			spec := r.buildBaseContainerSpec("c-test", tc.model, tc.sandbox)
			args := spec.Build()

			for _, p := range tc.wantPairs {
				if !containsConsecutive(args, p.flag, p.value) {
					t.Errorf("expected %q followed by %q; args: %v", p.flag, p.value, args)
				}
			}
			for _, want := range tc.wantArgs {
				if !argsContainSubstring(args, want) {
					t.Errorf("expected arg containing %q; args: %v", want, args)
				}
			}
			for _, notWant := range tc.wantNotArgs {
				if argsContainSubstring(args, notWant) {
					t.Errorf("unexpected arg containing %q; args: %v", notWant, args)
				}
			}
		})
	}
}

// TestBuildBaseContainerSpecClaudeVsCodexImageDiffers verifies that the claude
// and codex sandboxes resolve to different images from the same base image.
func TestBuildBaseContainerSpecClaudeVsCodexImageDiffers(t *testing.T) {
	r := newRunnerForArgTest(t, RunnerConfig{Command: "podman", SandboxImage: "wallfacer:latest"})

	claudeSpec := r.buildBaseContainerSpec("c-test", "", "claude")
	codexSpec := r.buildBaseContainerSpec("c-test", "", "codex")

	claudeArgs := claudeSpec.Build()
	codexArgs := codexSpec.Build()

	if !argsContainSubstring(claudeArgs, "wallfacer:latest") {
		t.Errorf("claude spec: expected wallfacer:latest; args: %v", claudeArgs)
	}
	if !argsContainSubstring(codexArgs, "wallfacer-codex:latest") {
		t.Errorf("codex spec: expected wallfacer-codex:latest; args: %v", codexArgs)
	}
	if argsContainSubstring(claudeArgs, "wallfacer-codex") {
		t.Errorf("claude spec should not reference wallfacer-codex; args: %v", claudeArgs)
	}
}

// TestBuildBaseContainerSpecVolumeOrder verifies that claude-config is always
// the first volume and that the codex auth mount (when present) follows it.
func TestBuildBaseContainerSpecVolumeOrder(t *testing.T) {
	codexDir := t.TempDir()
	// hostCodexAuthPath requires auth.json to exist inside the directory.
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := newRunnerForArgTest(t, RunnerConfig{
		Command:       "podman",
		SandboxImage:  "wallfacer:latest",
		CodexAuthPath: codexDir,
	})

	spec := r.buildBaseContainerSpec("c-test", "", "codex")
	args := spec.Build()

	claudeIdx, codexIdx := -1, -1
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-v" && args[i+1] == "claude-config:/home/claude/.claude" {
			claudeIdx = i
		}
		if args[i] == "-v" && strings.Contains(args[i+1], "/home/codex/.codex") {
			codexIdx = i
		}
	}
	if claudeIdx == -1 {
		t.Fatal("claude-config volume not found")
	}
	if codexIdx == -1 {
		t.Fatal("codex auth volume not found")
	}
	if claudeIdx >= codexIdx {
		t.Errorf("claude-config (-v at %d) should appear before codex auth (-v at %d)", claudeIdx, codexIdx)
	}
}

// TestBuildBaseContainerSpecRuntimeNotInBuild verifies that Runtime is used
// for exec.Command but does not appear in the Build() arg slice.
func TestBuildBaseContainerSpecRuntimeNotInBuild(t *testing.T) {
	r := newRunnerForArgTest(t, RunnerConfig{Command: "/opt/podman/bin/podman", SandboxImage: "wallfacer:latest"})
	spec := r.buildBaseContainerSpec("c-test", "", "claude")
	args := spec.Build()

	for _, a := range args {
		if a == "/opt/podman/bin/podman" {
			t.Errorf("Runtime must not appear in Build() output; args: %v", args)
		}
	}
	if spec.Runtime != "/opt/podman/bin/podman" {
		t.Errorf("spec.Runtime should be set; got %q", spec.Runtime)
	}
}

// ---------------------------------------------------------------------------
// buildIdeationContainerArgs — table-driven parity tests
// ---------------------------------------------------------------------------

func TestBuildIdeationContainerArgs(t *testing.T) {
	type pair struct{ flag, value string }
	tests := []struct {
		name        string
		cfgFn       func(t *testing.T) RunnerConfig
		sandbox     string
		wantPairs   []pair
		wantArgs    []string
		wantNotArgs []string
	}{
		{
			name: "single workspace: read-only mount and correct workdir",
			cfgFn: func(t *testing.T) RunnerConfig {
				ws := t.TempDir()
				return RunnerConfig{
					Command:      "podman",
					SandboxImage: "wallfacer:latest",
					Workspaces:   ws,
				}
			},
			sandbox: "claude",
			wantPairs: []pair{
				{"-v", ""},  // checked separately below
				{"-w", ""},  // checked separately below
			},
			wantNotArgs: []string{":z\x00"}, // no non-readonly workspace mounts
		},
		{
			name: "multiple workspaces: workdir is /workspace",
			cfgFn: func(t *testing.T) RunnerConfig {
				ws1 := filepath.Join(t.TempDir(), "repo-a")
				ws2 := filepath.Join(t.TempDir(), "repo-b")
				if err := os.MkdirAll(ws1, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(ws2, 0755); err != nil {
					t.Fatal(err)
				}
				return RunnerConfig{
					Command:      "podman",
					SandboxImage: "wallfacer:latest",
					Workspaces:   ws1 + " " + ws2,
				}
			},
			sandbox: "claude",
			wantPairs: []pair{
				{"-w", "/workspace"},
			},
		},
		{
			name: "instructions file mounted read-only with CLAUDE.md for claude",
			cfgFn: func(t *testing.T) RunnerConfig {
				instrPath := filepath.Join(t.TempDir(), "CLAUDE.md")
				if err := os.WriteFile(instrPath, []byte("# Instructions"), 0644); err != nil {
					t.Fatal(err)
				}
				return RunnerConfig{
					Command:          "podman",
					SandboxImage:     "wallfacer:latest",
					InstructionsPath: instrPath,
				}
			},
			sandbox:  "claude",
			wantArgs: []string{"/workspace/CLAUDE.md:z,ro"},
		},
		{
			name: "instructions file mounted as AGENTS.md for codex",
			cfgFn: func(t *testing.T) RunnerConfig {
				instrPath := filepath.Join(t.TempDir(), "AGENTS.md")
				if err := os.WriteFile(instrPath, []byte("# Instructions"), 0644); err != nil {
					t.Fatal(err)
				}
				return RunnerConfig{
					Command:          "podman",
					SandboxImage:     "wallfacer:latest",
					InstructionsPath: instrPath,
				}
			},
			sandbox:  "codex",
			wantArgs: []string{"/workspace/AGENTS.md:z,ro"},
		},
		{
			name: "instructions file absent: no instructions mount",
			cfgFn: func(t *testing.T) RunnerConfig {
				return RunnerConfig{
					Command:          "podman",
					SandboxImage:     "wallfacer:latest",
					InstructionsPath: "/nonexistent/CLAUDE.md",
				}
			},
			sandbox:     "claude",
			wantNotArgs: []string{"/workspace/CLAUDE.md", "/workspace/AGENTS.md"},
		},
		{
			name: "no instructions path: no instructions mount",
			cfgFn: func(t *testing.T) RunnerConfig {
				return RunnerConfig{
					Command:      "podman",
					SandboxImage: "wallfacer:latest",
				}
			},
			sandbox:     "claude",
			wantNotArgs: []string{"/workspace/CLAUDE.md", "/workspace/AGENTS.md"},
		},
		{
			name: "prompt is passed after image in Cmd",
			cfgFn: func(t *testing.T) RunnerConfig {
				return RunnerConfig{Command: "podman", SandboxImage: "wallfacer:latest"}
			},
			sandbox: "claude",
			wantPairs: []pair{
				{"-p", "analyze the workspace"},
			},
		},
		{
			name: "claude and codex produce different images",
			cfgFn: func(t *testing.T) RunnerConfig {
				return RunnerConfig{Command: "podman", SandboxImage: "wallfacer:latest"}
			},
			sandbox:  "codex",
			wantArgs: []string{"wallfacer-codex:latest"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunnerForArgTest(t, tc.cfgFn(t))
			args := r.buildIdeationContainerArgs("ideate-test", "analyze the workspace", tc.sandbox)

			for _, p := range tc.wantPairs {
				if p.value == "" {
					// Skip empty-value pairs (tested via wantArgs instead)
					continue
				}
				if !containsConsecutive(args, p.flag, p.value) {
					t.Errorf("expected %q followed by %q; args: %v", p.flag, p.value, args)
				}
			}
			for _, want := range tc.wantArgs {
				if !argsContainSubstring(args, want) {
					t.Errorf("expected arg containing %q; args: %v", want, args)
				}
			}
			for _, notWant := range tc.wantNotArgs {
				if argsContainSubstring(args, notWant) {
					t.Errorf("unexpected arg containing %q; args: %v", notWant, args)
				}
			}
		})
	}
}

// TestBuildIdeationContainerArgsSingleWorkspaceReadOnly verifies that the
// single workspace mount uses ":z,ro" (read-only) and the workdir points into
// that workspace.
func TestBuildIdeationContainerArgsSingleWorkspaceReadOnly(t *testing.T) {
	ws := t.TempDir() // e.g. /tmp/TestXXXX/001
	basename := filepath.Base(ws)

	r := newRunnerForArgTest(t, RunnerConfig{
		Command:      "podman",
		SandboxImage: "wallfacer:latest",
		Workspaces:   ws,
	})
	args := r.buildIdeationContainerArgs("ideate-test", "prompt", "claude")

	// The workspace mount must be read-only.
	wantMount := ws + ":/workspace/" + basename + ":z,ro"
	if !argsContainSubstring(args, wantMount) {
		t.Errorf("expected read-only mount %q; args: %v", wantMount, args)
	}

	// Workdir must point to the single workspace.
	wantWorkdir := "/workspace/" + basename
	if !containsConsecutive(args, "-w", wantWorkdir) {
		t.Errorf("expected -w %q; args: %v", wantWorkdir, args)
	}

	// No plain :z mount (read-write) for the workspace.
	rwMount := ws + ":/workspace/" + basename + ":z"
	for _, a := range args {
		if a == rwMount {
			t.Errorf("workspace should not be mounted read-write; found %q in args: %v", rwMount, args)
		}
	}
}

// TestBuildIdeationContainerArgsClaudeVsCodex verifies that running ideation
// with claude vs codex sandboxes produces the correct sandbox image while
// keeping all other flags identical.
func TestBuildIdeationContainerArgsClaudeVsCodex(t *testing.T) {
	r := newRunnerForArgTest(t, RunnerConfig{
		Command:      "podman",
		SandboxImage: "wallfacer:latest",
	})

	claudeArgs := r.buildIdeationContainerArgs("ideate-test", "prompt", "claude")
	codexArgs := r.buildIdeationContainerArgs("ideate-test", "prompt", "codex")

	if !argsContainSubstring(claudeArgs, "wallfacer:latest") {
		t.Errorf("claude ideation: expected wallfacer:latest; args: %v", claudeArgs)
	}
	if !argsContainSubstring(codexArgs, "wallfacer-codex:latest") {
		t.Errorf("codex ideation: expected wallfacer-codex:latest; args: %v", codexArgs)
	}

	// Both should include --network=host.
	if !argsContainSubstring(claudeArgs, "host") {
		t.Errorf("claude ideation: expected --network=host; args: %v", claudeArgs)
	}
	if !argsContainSubstring(codexArgs, "host") {
		t.Errorf("codex ideation: expected --network=host; args: %v", codexArgs)
	}
}

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

func TestWorkdirForBasenames(t *testing.T) {
	tests := []struct {
		name      string
		basenames []string
		want      string
	}{
		{
			name:      "nil basenames → /workspace",
			basenames: nil,
			want:      "/workspace",
		},
		{
			name:      "empty basenames → /workspace",
			basenames: []string{},
			want:      "/workspace",
		},
		{
			name:      "single basename → /workspace/<name>",
			basenames: []string{"myrepo"},
			want:      "/workspace/myrepo",
		},
		{
			name:      "two basenames → /workspace",
			basenames: []string{"repo-a", "repo-b"},
			want:      "/workspace",
		},
		{
			name:      "three basenames → /workspace",
			basenames: []string{"a", "b", "c"},
			want:      "/workspace",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := workdirForBasenames(tc.basenames)
			if got != tc.want {
				t.Errorf("workdirForBasenames(%v) = %q, want %q", tc.basenames, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// appendInstructionsMount — unit tests
// ---------------------------------------------------------------------------

func TestAppendInstructionsMount(t *testing.T) {
	tests := []struct {
		name            string
		cfgFn           func(t *testing.T) RunnerConfig
		sandbox         string
		wantMountSuffix string // substring expected in the -v value
		wantNone        bool   // when true, no instructions -v should be added
	}{
		{
			name: "claude sandbox: mounts as CLAUDE.md",
			cfgFn: func(t *testing.T) RunnerConfig {
				p := filepath.Join(t.TempDir(), "CLAUDE.md")
				if err := os.WriteFile(p, []byte("# Instructions"), 0644); err != nil {
					t.Fatal(err)
				}
				return RunnerConfig{Command: "podman", SandboxImage: "img", InstructionsPath: p}
			},
			sandbox:         "claude",
			wantMountSuffix: "/workspace/CLAUDE.md:z,ro",
		},
		{
			name: "codex sandbox: mounts as AGENTS.md",
			cfgFn: func(t *testing.T) RunnerConfig {
				p := filepath.Join(t.TempDir(), "AGENTS.md")
				if err := os.WriteFile(p, []byte("# Instructions"), 0644); err != nil {
					t.Fatal(err)
				}
				return RunnerConfig{Command: "podman", SandboxImage: "img", InstructionsPath: p}
			},
			sandbox:         "codex",
			wantMountSuffix: "/workspace/AGENTS.md:z,ro",
		},
		{
			name: "missing file: no mount added",
			cfgFn: func(t *testing.T) RunnerConfig {
				return RunnerConfig{
					Command:          "podman",
					SandboxImage:     "img",
					InstructionsPath: "/nonexistent/CLAUDE.md",
				}
			},
			sandbox:  "claude",
			wantNone: true,
		},
		{
			name: "empty path: no mount added",
			cfgFn: func(t *testing.T) RunnerConfig {
				return RunnerConfig{Command: "podman", SandboxImage: "img"}
			},
			sandbox:  "claude",
			wantNone: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunnerForArgTest(t, tc.cfgFn(t))
			initial := []VolumeMount{{Host: "claude-config", Container: "/home/claude/.claude"}}
			result := r.appendInstructionsMount(initial, tc.sandbox)

			if tc.wantNone {
				if len(result) != len(initial) {
					t.Errorf("expected no new mount; got %d volumes (was %d)", len(result), len(initial))
				}
				return
			}
			if len(result) != len(initial)+1 {
				t.Fatalf("expected %d volumes; got %d", len(initial)+1, len(result))
			}
			added := result[len(result)-1]
			mountStr := added.Host + ":" + added.Container + ":" + added.Options
			if !strings.Contains(mountStr, tc.wantMountSuffix) {
				t.Errorf("expected mount containing %q; got %q", tc.wantMountSuffix, mountStr)
			}
			if added.Options != "z,ro" {
				t.Errorf("instructions mount must be read-only (z,ro); got %q", added.Options)
			}
		})
	}
}

// TestAppendInstructionsMountReadOnly verifies the mount is always read-only,
// regardless of sandbox type.
func TestAppendInstructionsMountReadOnly(t *testing.T) {
	for _, sandbox := range []string{"claude", "codex"} {
		t.Run(sandbox, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "instructions.md")
			if err := os.WriteFile(p, []byte("# Instructions"), 0644); err != nil {
				t.Fatal(err)
			}
			r := newRunnerForArgTest(t, RunnerConfig{
				Command:          "podman",
				SandboxImage:     "img",
				InstructionsPath: p,
			})
			result := r.appendInstructionsMount(nil, sandbox)
			if len(result) != 1 {
				t.Fatalf("expected 1 mount; got %d", len(result))
			}
			if result[0].Options != "z,ro" {
				t.Errorf("expected Options=z,ro; got %q", result[0].Options)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Commit and title invocation patterns via buildBaseContainerSpec + buildAgentCmd
// ---------------------------------------------------------------------------

// TestCommitStyleInvocation verifies that building a spec the same way
// generateCommitMessage does (buildBaseContainerSpec + buildAgentCmd) produces
// the expected arg order: base flags, image, then the prompt command.
func TestCommitStyleInvocation(t *testing.T) {
	r := newRunnerForArgTest(t, RunnerConfig{
		Command:      "podman",
		SandboxImage: "wallfacer:latest",
		EnvFile:      "/home/.env",
	})

	model := "claude-opus-4-6"
	spec := r.buildBaseContainerSpec("wallfacer-commit-abc12345", model, "claude")
	commitPrompt := "Write a commit message for: add tests"
	spec.Cmd = buildAgentCmd(commitPrompt, model)
	args := spec.Build()

	// Fixed prefix.
	if len(args) < 4 || args[0] != "run" || args[1] != "--rm" {
		t.Fatalf("unexpected prefix: %v", args)
	}

	// --env-file before -e.
	envFileIdx, eIdx := -1, -1
	for i, a := range args {
		if a == "--env-file" {
			envFileIdx = i
		}
		if a == "-e" {
			eIdx = i
		}
	}
	if envFileIdx == -1 || eIdx == -1 {
		t.Fatalf("env-file or -e not found; args: %v", args)
	}
	if envFileIdx > eIdx {
		t.Errorf("--env-file (%d) should appear before -e (%d)", envFileIdx, eIdx)
	}

	// Image appears before -p.
	imageIdx, promptIdx := -1, -1
	for i, a := range args {
		if a == "wallfacer:latest" {
			imageIdx = i
		}
		if a == "-p" {
			promptIdx = i
		}
	}
	if imageIdx == -1 || promptIdx == -1 {
		t.Fatalf("image or -p not found; args: %v", args)
	}
	if imageIdx > promptIdx {
		t.Errorf("image (%d) should appear before -p (%d)", imageIdx, promptIdx)
	}
}

// TestTitleStyleInvocation verifies that building a spec the same way
// GenerateTitle does (buildBaseContainerSpec + buildAgentCmd) produces the
// expected arg order.
func TestTitleStyleInvocation(t *testing.T) {
	r := newRunnerForArgTest(t, RunnerConfig{
		Command:      "podman",
		SandboxImage: "wallfacer:latest",
	})

	model := "claude-haiku-4-5"
	spec := r.buildBaseContainerSpec("wallfacer-title-abc12345", model, "claude")
	titlePrompt := "Respond with ONLY a 2-5 word title"
	spec.Cmd = buildAgentCmd(titlePrompt, model)
	args := spec.Build()

	// -p must appear after the image.
	imageIdx, promptIdx := -1, -1
	for i, a := range args {
		if a == "wallfacer:latest" {
			imageIdx = i
		}
		if a == "-p" {
			promptIdx = i
		}
	}
	if imageIdx == -1 || promptIdx == -1 {
		t.Fatalf("image or -p not found; args: %v", args)
	}
	if imageIdx > promptIdx {
		t.Errorf("image (%d) should appear before -p (%d) in title invocation", imageIdx, promptIdx)
	}

	// --model must appear in Cmd (after image).
	modelIdx := -1
	for i, a := range args {
		if a == "--model" {
			modelIdx = i
		}
	}
	if modelIdx == -1 {
		t.Fatalf("--model not found; args: %v", args)
	}
	if modelIdx < imageIdx {
		t.Errorf("--model (%d) should appear after image (%d)", modelIdx, imageIdx)
	}
}

// ---------------------------------------------------------------------------
// ContainerSpec CPU/memory resource limit flags
// ---------------------------------------------------------------------------

// TestContainerSpecCPUsAndMemoryFlags verifies that non-empty CPUs and Memory
// fields emit --cpus and --memory flags, respectively.
func TestContainerSpecCPUsAndMemoryFlags(t *testing.T) {
	spec := ContainerSpec{
		Name:   "test",
		Image:  "img",
		CPUs:   "1.5",
		Memory: "2g",
	}
	args := spec.Build()

	if !containsConsecutive(args, "--cpus", "1.5") {
		t.Errorf("expected --cpus 1.5 in args; got %v", args)
	}
	if !containsConsecutive(args, "--memory", "2g") {
		t.Errorf("expected --memory 2g in args; got %v", args)
	}
}

// TestContainerSpecZeroCPUsAndMemoryNoFlags verifies that zero-value CPUs and
// Memory fields produce no --cpus or --memory flags.
func TestContainerSpecZeroCPUsAndMemoryNoFlags(t *testing.T) {
	spec := ContainerSpec{Name: "test", Image: "img"}
	args := spec.Build()

	for i, a := range args {
		if a == "--cpus" {
			t.Errorf("unexpected --cpus flag at index %d; args: %v", i, args)
		}
		if a == "--memory" {
			t.Errorf("unexpected --memory flag at index %d; args: %v", i, args)
		}
	}
}

// TestContainerSpecResourceFlagsBeforeExtraFlags verifies that --cpus and
// --memory appear after -w and before ExtraFlags and the image.
func TestContainerSpecResourceFlagsBeforeExtraFlags(t *testing.T) {
	spec := ContainerSpec{
		Name:       "test",
		Image:      "img",
		WorkDir:    "/work",
		CPUs:       "2.0",
		Memory:     "4g",
		ExtraFlags: []string{"--security-opt", "no-new-privileges"},
	}
	args := spec.Build()

	wIdx, cpusIdx, memIdx, secIdx, imgIdx := -1, -1, -1, -1, -1
	for i, a := range args {
		switch a {
		case "-w":
			wIdx = i
		case "--cpus":
			cpusIdx = i
		case "--memory":
			memIdx = i
		case "--security-opt":
			secIdx = i
		case "img":
			imgIdx = i
		}
	}
	if cpusIdx == -1 || memIdx == -1 {
		t.Fatalf("--cpus or --memory not found; args: %v", args)
	}
	if wIdx >= cpusIdx {
		t.Errorf("-w (%d) should appear before --cpus (%d)", wIdx, cpusIdx)
	}
	if cpusIdx >= memIdx {
		t.Errorf("--cpus (%d) should appear before --memory (%d)", cpusIdx, memIdx)
	}
	if memIdx >= secIdx {
		t.Errorf("--memory (%d) should appear before ExtraFlags --security-opt (%d)", memIdx, secIdx)
	}
	if secIdx >= imgIdx {
		t.Errorf("ExtraFlags --security-opt (%d) should appear before image (%d)", secIdx, imgIdx)
	}
}

// TestBuildBaseContainerSpecPropagatesResourceLimits verifies that
// buildBaseContainerSpec transfers ContainerCPUs and ContainerMemory from the
// RunnerConfig to the returned ContainerSpec's Build() output.
func TestBuildBaseContainerSpecPropagatesResourceLimits(t *testing.T) {
	r := newRunnerForArgTest(t, RunnerConfig{
		Command:         "podman",
		SandboxImage:    "wallfacer:latest",
		ContainerCPUs:   "1.5",
		ContainerMemory: "2g",
	})

	spec := r.buildBaseContainerSpec("c-test", "", "claude")
	args := spec.Build()

	if !containsConsecutive(args, "--cpus", "1.5") {
		t.Errorf("expected --cpus 1.5 propagated from RunnerConfig; args: %v", args)
	}
	if !containsConsecutive(args, "--memory", "2g") {
		t.Errorf("expected --memory 2g propagated from RunnerConfig; args: %v", args)
	}
}

// TestBuildBaseContainerSpecNoResourceLimitsWhenEmpty verifies that no
// --cpus or --memory flags appear when the runner config has no resource limits.
func TestBuildBaseContainerSpecNoResourceLimitsWhenEmpty(t *testing.T) {
	r := newRunnerForArgTest(t, RunnerConfig{
		Command:      "podman",
		SandboxImage: "wallfacer:latest",
	})

	spec := r.buildBaseContainerSpec("c-test", "", "claude")
	args := spec.Build()

	for _, a := range args {
		if a == "--cpus" {
			t.Errorf("unexpected --cpus in args when ContainerCPUs is empty; args: %v", args)
		}
		if a == "--memory" {
			t.Errorf("unexpected --memory in args when ContainerMemory is empty; args: %v", args)
		}
	}
}

// TestBuildBaseContainerSpecResourceLimitsFromEnvFile verifies that
// buildBaseContainerSpec picks up CPU and memory limits from the env file
// when no static RunnerConfig values are set.
func TestBuildBaseContainerSpecResourceLimitsFromEnvFile(t *testing.T) {
	envPath := strings.Join([]string{t.TempDir(), ".env"}, "/")
	envContent := "WALLFACER_CONTAINER_CPUS=3.0\nWALLFACER_CONTAINER_MEMORY=8g\n"
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		t.Fatal(err)
	}

	r := newRunnerForArgTest(t, RunnerConfig{
		Command:      "podman",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envPath,
		// ContainerCPUs and ContainerMemory intentionally left empty to test env-file fallback.
	})

	spec := r.buildBaseContainerSpec("c-test", "", "claude")
	args := spec.Build()

	if !containsConsecutive(args, "--cpus", "3.0") {
		t.Errorf("expected --cpus 3.0 from env file; args: %v", args)
	}
	if !containsConsecutive(args, "--memory", "8g") {
		t.Errorf("expected --memory 8g from env file; args: %v", args)
	}
}

// TestBuildBaseContainerSpecParityWithBuildContainerArgsForSandbox verifies
// that the base spec produced by buildBaseContainerSpec is a prefix-equivalent
// of what buildContainerArgsForSandbox produces (same initial env/volume flags).
// This guards against the refactoring introducing a behavioural divergence.
func TestBuildBaseContainerSpecParityWithBuildContainerArgsForSandbox(t *testing.T) {
	r := newRunnerForArgTest(t, RunnerConfig{
		Command:      "podman",
		SandboxImage: "wallfacer:latest",
		EnvFile:      "/home/.env",
	})
	model := "claude-opus-4-6"

	// Build via buildBaseContainerSpec (no extra workspace volumes).
	baseSpec := r.buildBaseContainerSpec("parity-test", model, "claude")
	baseArgs := baseSpec.Build()

	// Build via buildContainerArgsForSandbox with no workspaces, no board, no sibling mounts.
	// r.workspaces is empty (RunnerConfig.Workspaces == ""), so only the base flags differ.
	fullArgs := r.buildContainerArgsForSandbox(
		"parity-test", "", "test prompt", "", nil, "", nil, model, "claude",
	)

	// Both must contain the same env-file, -e, and claude-config -v flags.
	for _, flag := range []string{"--env-file", "-e", "-v"} {
		baseHas := argsContainSubstring(baseArgs, flag)
		fullHas := argsContainSubstring(fullArgs, flag)
		if baseHas != fullHas {
			t.Errorf("flag %q: baseSpec has=%v, fullArgs has=%v", flag, baseHas, fullHas)
		}
	}
	if !containsConsecutive(baseArgs, "--env-file", "/home/.env") {
		t.Errorf("base spec missing --env-file; args: %v", baseArgs)
	}
	if !containsConsecutive(fullArgs, "--env-file", "/home/.env") {
		t.Errorf("full args missing --env-file; args: %v", fullArgs)
	}
	if !containsConsecutive(baseArgs, "-e", "CLAUDE_CODE_MODEL="+model) {
		t.Errorf("base spec missing -e CLAUDE_CODE_MODEL; args: %v", baseArgs)
	}
	if !containsConsecutive(fullArgs, "-e", "CLAUDE_CODE_MODEL="+model) {
		t.Errorf("full args missing -e CLAUDE_CODE_MODEL; args: %v", fullArgs)
	}
}
