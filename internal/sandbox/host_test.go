//go:build !windows

package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// buildFakeAgent compiles testdata/fakeagent into a temp binary named `name`
// and returns the absolute path. The binary echoes parsed flags and env vars
// as NDJSON so tests can assert wiring without spawning a real agent.
func buildFakeAgent(t *testing.T, name string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), name)
	// The fakeagent main has a `//go:build ignore` tag so it is excluded from
	// normal builds. Passing the file path directly forces the build anyway.
	cmd := exec.Command("go", "build", "-o", bin, "testdata/fakeagent/main.go")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeagent: %v\n%s", err, out)
	}
	return bin
}

func TestNewHostBackend_MissingClaudeFails(t *testing.T) {
	// Claude is required at construction; an unresolved claude is a
	// startup error so users get a clear message instead of a cryptic
	// first-task failure.
	_, err := NewHostBackend(HostBackendConfig{
		ClaudeBinary: "/no/such/binary",
	})
	if err == nil {
		t.Fatal("expected error for missing claude")
	}
	if !strings.Contains(err.Error(), "claude") || !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention missing claude; got: %v", err)
	}
}

func TestNewHostBackend_MissingCodexIsTolerated(t *testing.T) {
	// Codex is best-effort for now (Launch rejects it anyway). Missing
	// codex must not block construction.
	bin := buildFakeAgent(t, "fakeagent")
	_, err := NewHostBackend(HostBackendConfig{
		ClaudeBinary: bin,
		CodexBinary:  "/no/such/binary",
	})
	if err != nil {
		t.Errorf("construction should succeed without codex; got: %v", err)
	}
}

// Codex-mode tests live in host_codex_test.go.

func TestNewHostBackend_UsesLookupWhenEmpty(t *testing.T) {
	// With explicit valid paths, construction succeeds.
	bin := buildFakeAgent(t, "fakeagent")
	if _, err := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostBackend_SupportsAppendSystemPrompt(t *testing.T) {
	t.Run("supported", func(t *testing.T) {
		bin := buildFakeAgent(t, "fakeagent")
		b, err := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		if !b.SupportsAppendSystemPrompt(Claude) {
			t.Error("expected --append-system-prompt to be detected")
		}
	})
	t.Run("not-supported", func(t *testing.T) {
		bin := buildFakeAgent(t, "fakeagent")
		b, err := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		// Poison the probe via env: the fakeagent omits the flag from --help when
		// FAKEAGENT_NO_APPEND=1 is set in the probe's environment.
		t.Setenv("FAKEAGENT_NO_APPEND", "1")
		if b.SupportsAppendSystemPrompt(Codex) {
			t.Error("expected probe to report no support when fakeagent hides the flag")
		}
	})
}

// launchAndDrain runs Launch with a minimal spec and returns the parsed NDJSON
// init record the fakeagent emits. Useful for asserting argv / env wiring.
func launchAndDrain(t *testing.T, b *HostBackend, spec ContainerSpec) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := b.Launch(ctx, spec)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	out, _ := io.ReadAll(h.Stdout())
	_, _ = io.ReadAll(h.Stderr())
	if _, err := h.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}

	// First NDJSON line is the init record.
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("decode init line: %v\nline: %s\nfull: %s", err, line, out)
	}
	return got
}

func TestHostBackend_Launch_Argv(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, err := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	spec := ContainerSpec{
		Name:    "wallfacer-test-argv",
		Env:     map[string]string{"WALLFACER_AGENT": "claude"},
		Cmd:     []string{"-p", "hello world", "--model", "sonnet-4-6"},
		WorkDir: t.TempDir(),
	}
	got := launchAndDrain(t, b, spec)

	if got["prompt"] != "hello world" {
		t.Errorf("prompt = %q; want %q", got["prompt"], "hello world")
	}
	if got["model"] != "sonnet-4-6" {
		t.Errorf("model = %q; want %q", got["model"], "sonnet-4-6")
	}
	if got["agent"] != "claude" {
		t.Errorf("agent = %q; want %q", got["agent"], "claude")
	}
}

func TestHostBackend_Launch_ResumeFlag(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})

	spec := ContainerSpec{
		Name:    "wallfacer-test-resume",
		Env:     map[string]string{"WALLFACER_AGENT": "claude"},
		Cmd:     []string{"-p", "continue", "--resume", "sess-42"},
		WorkDir: t.TempDir(),
	}
	got := launchAndDrain(t, b, spec)
	if got["resume"] != "sess-42" {
		t.Errorf("resume = %q; want sess-42", got["resume"])
	}
}

func TestHostBackend_Launch_EnvMerge(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})

	envFile := filepath.Join(t.TempDir(), ".env")
	// A=1 from file; B=2 from file but overridden by spec.Env to 3; C=4 only in spec.Env.
	if err := os.WriteFile(envFile, []byte("FAKEAGENT_A=1\nFAKEAGENT_B=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec := ContainerSpec{
		Name:    "wallfacer-test-envmerge",
		EnvFile: envFile,
		Env: map[string]string{
			"WALLFACER_AGENT": "claude",
			"FAKEAGENT_B":     "3",
			"FAKEAGENT_C":     "4",
		},
		Cmd:     []string{"-p", "hi"},
		WorkDir: t.TempDir(),
	}
	got := launchAndDrain(t, b, spec)

	echo, _ := got["env_echo"].(map[string]any)
	if echo["FAKEAGENT_A"] != "1" {
		t.Errorf("A = %v; want 1", echo["FAKEAGENT_A"])
	}
	if echo["FAKEAGENT_B"] != "3" {
		t.Errorf("B = %v; want 3 (spec.Env should override env file)", echo["FAKEAGENT_B"])
	}
	if echo["FAKEAGENT_C"] != "4" {
		t.Errorf("C = %v; want 4", echo["FAKEAGENT_C"])
	}
}

func TestHostBackend_Launch_WorkDirIsUsed(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})

	wd := t.TempDir()
	// t.TempDir returns an evaluated path on darwin/linux, but symlinks under
	// /var → /private/var can still bite. Resolve once for the assertion.
	resolved, err := filepath.EvalSymlinks(wd)
	if err != nil {
		resolved = wd
	}

	spec := ContainerSpec{
		Name:    "wallfacer-test-workdir",
		Env:     map[string]string{"WALLFACER_AGENT": "claude"},
		Cmd:     []string{"-p", "hi"},
		WorkDir: wd,
	}
	got := launchAndDrain(t, b, spec)
	cwd, _ := got["cwd"].(string)
	resolvedCwd, _ := filepath.EvalSymlinks(cwd)
	if resolvedCwd != resolved {
		t.Errorf("cwd = %q (resolved %q); want %q", cwd, resolvedCwd, resolved)
	}
}

func TestHostBackend_Launch_RejectsContainerPath(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})

	spec := ContainerSpec{
		Name:    "wallfacer-test-reject",
		Env:     map[string]string{"WALLFACER_AGENT": "claude"},
		Cmd:     []string{"-p", "hi"},
		WorkDir: "/workspace/myrepo",
	}
	_, err := b.Launch(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error for container path WorkDir")
	}
	if !strings.Contains(err.Error(), "container path") {
		t.Errorf("error should mention container path; got: %v", err)
	}
}

func TestHostBackend_Launch_MissingAgentEnv(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})

	_, err := b.Launch(context.Background(), ContainerSpec{
		Name:    "wallfacer-test-noagent",
		Cmd:     []string{"-p", "hi"},
		WorkDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "WALLFACER_AGENT") {
		t.Errorf("expected WALLFACER_AGENT error; got: %v", err)
	}
}

func TestHostBackend_AppendSystemPrompt_Supported(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})

	instr := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(instr, []byte("workspace instructions here"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec := ContainerSpec{
		Name: "wallfacer-test-append-ok",
		Env: map[string]string{
			"WALLFACER_AGENT":             "claude",
			"WALLFACER_INSTRUCTIONS_PATH": instr,
			// Disable fast mode so --append-system-prompt only carries the
			// instructions path (not the /fast shortcut the launcher also
			// appends when fast mode is active).
			"WALLFACER_SANDBOX_FAST": "false",
		},
		Cmd:     []string{"-p", "run"},
		WorkDir: t.TempDir(),
	}
	got := launchAndDrain(t, b, spec)
	if got["append"] != instr {
		t.Errorf("fakeagent saw append = %q; want %q (flag should be forwarded)", got["append"], instr)
	}
	// In supported mode the prompt itself must not be prepended.
	if got["prompt"] != "run" {
		t.Errorf("prompt should be unmodified when flag is supported; got %q", got["prompt"])
	}
}

func TestHostBackend_AppendSystemPrompt_Fallback(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})

	// Force the probe to report "not supported" by pre-seeding the cache.
	b.probeMu.Lock()
	b.probedOnce[Claude] = true
	b.probedSupport[Claude] = false
	b.probeMu.Unlock()

	instr := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(instr, []byte("PREAMBLE"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec := ContainerSpec{
		Name: "wallfacer-test-append-fallback",
		Env: map[string]string{
			"WALLFACER_AGENT":             "claude",
			"WALLFACER_INSTRUCTIONS_PATH": instr,
			// Disable fast mode so the --append-system-prompt flag is NOT
			// added by the launcher itself; this isolates the fallback
			// behaviour we're asserting.
			"WALLFACER_SANDBOX_FAST": "false",
		},
		Cmd:     []string{"-p", "the-task"},
		WorkDir: t.TempDir(),
	}
	got := launchAndDrain(t, b, spec)

	prompt, _ := got["prompt"].(string)
	if !strings.HasPrefix(prompt, "PREAMBLE") {
		t.Errorf("prompt should start with preamble; got %q", prompt)
	}
	if !strings.HasSuffix(prompt, "the-task") {
		t.Errorf("prompt should end with original task; got %q", prompt)
	}
	if got["append"] != "" {
		t.Errorf("append flag should not be passed in fallback mode; got %q", got["append"])
	}
}

func TestHostBackend_Kill_Escalates(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})

	spec := ContainerSpec{
		Name: "wallfacer-test-kill",
		Env: map[string]string{
			"WALLFACER_AGENT": "claude",
			"FAKEAGENT_SLEEP": "10",
		},
		Cmd:     []string{"-p", "wait"},
		WorkDir: t.TempDir(),
	}
	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	// Drain output in the background so pipes don't block.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.ReadAll(h.Stdout()) }()
	go func() { defer wg.Done(); _, _ = io.ReadAll(h.Stderr()) }()

	// Give the child a moment to install its sleep.
	time.Sleep(100 * time.Millisecond)

	if err := h.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}

	done := make(chan int, 1)
	go func() {
		code, _ := h.Wait()
		done <- code
	}()

	select {
	case <-done:
		// success
	case <-time.After(8 * time.Second):
		t.Fatal("Wait did not return within 8s after Kill")
	}
	wg.Wait()

	// After Wait, the handle should be deregistered from List.
	infos, _ := b.List(context.Background())
	for _, info := range infos {
		if info.Name == spec.Name {
			t.Errorf("handle %q still in List after Wait", spec.Name)
		}
	}
}

func TestHostBackend_List(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})

	// Use a sleeping fakeagent so List catches it mid-flight.
	spec := ContainerSpec{
		Name: "wallfacer-list-a",
		Env: map[string]string{
			"WALLFACER_AGENT": "claude",
			"FAKEAGENT_SLEEP": "3",
		},
		Labels:  map[string]string{"wallfacer.task.id": "task-abc"},
		Cmd:     []string{"-p", "x"},
		WorkDir: t.TempDir(),
	}
	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	// Drain in the background so the pipe doesn't backpressure the child.
	go func() { _, _ = io.ReadAll(h.Stdout()) }()
	go func() { _, _ = io.ReadAll(h.Stderr()) }()

	time.Sleep(150 * time.Millisecond)

	infos, err := b.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(infos), infos)
	}
	info := infos[0]
	if info.Name != spec.Name {
		t.Errorf("name = %q; want %q", info.Name, spec.Name)
	}
	if info.TaskID != "task-abc" {
		t.Errorf("task id = %q; want task-abc (from labels)", info.TaskID)
	}
	if info.Image != "host" {
		t.Errorf("image = %q; want host", info.Image)
	}
	if !strings.HasPrefix(info.Status, "Host PID") {
		t.Errorf("status = %q; want prefix 'Host PID'", info.Status)
	}

	_ = h.Kill()
	_, _ = h.Wait()
}

func TestParseEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\nA=1\nB=\"quoted value\"\n\nC='single'\nD=no_quotes\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := parseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"A": "1",
		"B": "quoted value",
		"C": "single",
		"D": "no_quotes",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q; want %q", k, got[k], v)
		}
	}
}

func TestSetEnv(t *testing.T) {
	env := []string{"A=1", "B=2", "C=3"}
	env = setEnv(env, "B", "two")
	if env[1] != "B=two" {
		t.Errorf("B override failed: %v", env)
	}
	env = setEnv(env, "D", "4")
	if env[len(env)-1] != "D=4" {
		t.Errorf("D append failed: %v", env)
	}
}

func TestPrependToPromptFlag(t *testing.T) {
	argv := []string{"-p", "task", "--model", "x"}
	got := prependToPromptFlag(argv, "PREAMBLE")
	if !strings.HasPrefix(got[1], "PREAMBLE") || !strings.HasSuffix(got[1], "task") {
		t.Errorf("prepend failed: %v", got)
	}

	// No -p flag: argv returned unchanged.
	argv2 := []string{"--foo", "bar"}
	got2 := prependToPromptFlag(argv2, "nope")
	if got2[0] != "--foo" || got2[1] != "bar" {
		t.Errorf("no-p-flag case modified argv: %v", got2)
	}
}
