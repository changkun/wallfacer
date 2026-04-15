package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestParseExecConfig_TaskMode verifies that a bare UUID prefix is parsed as
// task-mode configuration.
func TestParseExecConfig_TaskMode(t *testing.T) {
	cfg, err := parseExecConfig([]string{"249e9c9c"}, []string{"bash"})
	if err != nil {
		t.Fatalf("parseExecConfig returned error: %v", err)
	}
	if cfg.mode != execModeTask {
		t.Fatalf("expected task mode, got %v", cfg.mode)
	}
	if cfg.prefix != "249e9c9c" {
		t.Fatalf("expected prefix 249e9c9c, got %q", cfg.prefix)
	}
}

// TestParseExecConfig_SandboxMode verifies that --sandbox flag switches to
// sandbox mode with the specified sandbox type.
func TestParseExecConfig_SandboxMode(t *testing.T) {
	cfg, err := parseExecConfig([]string{"--sandbox", "codex"}, []string{"bash"})
	if err != nil {
		t.Fatalf("parseExecConfig returned error: %v", err)
	}
	if cfg.mode != execModeSandbox {
		t.Fatalf("expected sandbox mode, got %v", cfg.mode)
	}
	if cfg.sandbox != "codex" {
		t.Fatalf("expected sandbox codex, got %q", cfg.sandbox)
	}
}

// TestParseExecConfig_SandboxModeAllowsCommand verifies that extra positional
// arguments after --sandbox <type> are treated as the in-container command.
func TestParseExecConfig_SandboxModeAllowsCommand(t *testing.T) {
	cfg, err := parseExecConfig([]string{"--sandbox", "claude", "sh", "-c", "echo", "hi"}, []string{"bash"})
	if err != nil {
		t.Fatalf("parseExecConfig returned error: %v", err)
	}
	if cfg.mode != execModeSandbox {
		t.Fatalf("expected sandbox mode, got %v", cfg.mode)
	}
	if cfg.sandbox != "claude" {
		t.Fatalf("expected sandbox claude, got %q", cfg.sandbox)
	}
	want := []string{"sh", "-c", "echo", "hi"}
	if len(cfg.command) != len(want) {
		t.Fatalf("expected command %v, got %v", want, cfg.command)
	}
	for i, wantCommand := range want {
		if cfg.command[i] != wantCommand {
			t.Fatalf("command[%d] = %q, want %q", i, cfg.command[i], wantCommand)
		}
	}
}

// TestParseExecConfig_SandboxRejectsInvalidRuntime verifies that an unrecognized
// sandbox type (e.g. "llama") is rejected with an error.
func TestParseExecConfig_SandboxRejectsInvalidRuntime(t *testing.T) {
	_, err := parseExecConfig([]string{"--sandbox", "llama"}, []string{"bash"})
	if err == nil {
		t.Fatal("expected error for invalid runtime")
	}
}

// ---------------------------------------------------------------------------
// resolveContainerByPrefix
// ---------------------------------------------------------------------------

// TestResolveContainerByPrefixExactMatch verifies single-container resolution
// when the prefix matches the trailing UUID portion of the container name.
func TestResolveContainerByPrefixExactMatch(t *testing.T) {
	psOutput := "wallfacer-add-dark-mode-249e9c9c\n"
	got, err := resolveContainerByPrefix(psOutput, "249e9c9c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wallfacer-add-dark-mode-249e9c9c" {
		t.Fatalf("expected container name %q, got %q", "wallfacer-add-dark-mode-249e9c9c", got)
	}
}

// TestResolveContainerByPrefixSubstringMatch verifies that the prefix is
// matched as a substring within the container name, not just a trailing suffix.
func TestResolveContainerByPrefixSubstringMatch(t *testing.T) {
	// The prefix appears in the middle of the container name (slug portion).
	psOutput := "wallfacer-fix-foo-bar-abcd1234\nwallfacer-other-task-99887766\n"
	got, err := resolveContainerByPrefix(psOutput, "abcd1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wallfacer-fix-foo-bar-abcd1234" {
		t.Fatalf("expected %q, got %q", "wallfacer-fix-foo-bar-abcd1234", got)
	}
}

// TestResolveContainerByPrefixNoMatch verifies that a prefix matching no
// container produces a descriptive error.
func TestResolveContainerByPrefixNoMatch(t *testing.T) {
	psOutput := "wallfacer-add-dark-mode-249e9c9c\nwallfacer-fix-login-abcdef12\n"
	_, err := resolveContainerByPrefix(psOutput, "deadbeef")
	if err == nil {
		t.Fatal("expected error for no-match case, got nil")
	}
	if !strings.Contains(err.Error(), "no running container") {
		t.Fatalf("expected 'no running container' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "deadbeef") {
		t.Fatalf("expected prefix %q in error message, got: %v", "deadbeef", err)
	}
}

// TestResolveContainerByPrefixAmbiguous verifies that a prefix matching multiple
// containers produces an error listing all candidates.
func TestResolveContainerByPrefixAmbiguous(t *testing.T) {
	// Two containers whose names both contain the prefix.
	psOutput := "wallfacer-task-a-249e9c9c\nwallfacer-task-b-249e9c9c\n"
	_, err := resolveContainerByPrefix(psOutput, "249e9c9c")
	if err == nil {
		t.Fatal("expected error for ambiguous match, got nil")
	}
	if !strings.Contains(err.Error(), "multiple containers") {
		t.Fatalf("expected 'multiple containers' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "wallfacer-task-a-249e9c9c") {
		t.Fatalf("expected first candidate listed in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "wallfacer-task-b-249e9c9c") {
		t.Fatalf("expected second candidate listed in error, got: %v", err)
	}
}

// TestResolveContainerByPrefixEmptyOutput verifies that empty ps output
// (no running containers) produces a "no running container" error.
func TestResolveContainerByPrefixEmptyOutput(t *testing.T) {
	_, err := resolveContainerByPrefix("", "249e9c9c")
	if err == nil {
		t.Fatal("expected error for empty ps output, got nil")
	}
	if !strings.Contains(err.Error(), "no running container") {
		t.Fatalf("expected 'no running container' in error, got: %v", err)
	}
}

// TestResolveContainerByPrefixBlankLines verifies that blank lines in ps
// output are silently skipped without causing false matches.
func TestResolveContainerByPrefixBlankLines(t *testing.T) {
	// Blank lines in ps output must be ignored.
	psOutput := "\n\nwallfacer-fix-auth-aabbccdd\n\n"
	got, err := resolveContainerByPrefix(psOutput, "aabbccdd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wallfacer-fix-auth-aabbccdd" {
		t.Fatalf("expected %q, got %q", "wallfacer-fix-auth-aabbccdd", got)
	}
}

// TestResolveContainerByPrefixMultipleContainersOneMatch verifies that only
// the matching container is returned when several are running.
func TestResolveContainerByPrefixMultipleContainersOneMatch(t *testing.T) {
	// Several containers are running but only one matches the prefix.
	psOutput := strings.Join([]string{
		"wallfacer-add-feature-11223344",
		"wallfacer-fix-bug-55667788",
		"wallfacer-refactor-db-99aabbcc",
		"unrelated-container-xyz",
	}, "\n")
	got, err := resolveContainerByPrefix(psOutput, "55667788")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wallfacer-fix-bug-55667788" {
		t.Fatalf("expected %q, got %q", "wallfacer-fix-bug-55667788", got)
	}
}

// TestBuildSandboxExecArgs_UsesDefaultWorkspaceMount verifies that sandbox
// exec args include the env-file, Claude config volume, and workspace bind mount.
func TestBuildSandboxExecArgs_UsesDefaultWorkspaceMount(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("CLAUDE_CODE_OAUTH_TOKEN=x"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("HOME", tmp)
	t.Chdir(tmp)
	base := filepath.Base(tmp)

	args, err := buildSandboxExecArgs("/usr/bin/podman", tmp, "claude", []string{"bash"})
	if err != nil {
		t.Fatalf("buildSandboxExecArgs: %v", err)
	}

	got := strings.Join(args, " ")
	if !strings.Contains(got, "--env-file "+filepath.Join(tmp, ".env")) {
		t.Fatalf("expected env-file arg, got %q", got)
	}
	if !strings.Contains(got, "-v claude-config:/home/agent/.claude") {
		t.Fatalf("expected claude config mount, got %q", got)
	}
	if !strings.Contains(got, "-e WALLFACER_AGENT=claude") {
		t.Fatalf("expected WALLFACER_AGENT=claude env, got %q", got)
	}
	if !strings.Contains(got, "--mount type=bind,src="+tmp+",dst=/workspace/"+base+",z") {
		t.Fatalf("expected repository workspace mount, got %q", got)
	}
}

// TestBuildSandboxExecArgs_UsesCodexAuthWhenAvailable verifies that Codex
// sandbox exec args include a read-only bind mount for ~/.codex/auth.json
// when the auth file exists on the host.
func TestBuildSandboxExecArgs_UsesCodexAuthWhenAvailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	tmp := t.TempDir()
	authDir := filepath.Join(tmp, ".codex")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir codex: %v", err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), []byte(`{"access_token":"abc"}`), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("CLAUDE_CODE_OAUTH_TOKEN=x"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("HOME", tmp)
	t.Chdir(tmp)
	base := filepath.Base(tmp)

	args, err := buildSandboxExecArgs("/usr/bin/podman", tmp, "codex", []string{"echo", "hi"})
	if err != nil {
		t.Fatalf("buildSandboxExecArgs: %v", err)
	}

	got := strings.Join(args, " ")
	expectedWorkspaceMount := "--mount type=bind,src=" + tmp + ",dst=/workspace/" + filepath.Base(tmp) + ",z"
	if !strings.Contains(got, expectedWorkspaceMount) {
		t.Fatalf("expected workspace mount, got %q", got)
	}
	authFile := filepath.Join(authDir, "auth.json")
	if !strings.Contains(got, "--mount type=bind,src="+authFile+",dst=/home/agent/.codex/auth.json,readonly,z") {
		t.Fatalf("expected codex auth.json mount, got %q", got)
	}
	if !strings.Contains(got, "-e WALLFACER_AGENT=codex") {
		t.Fatalf("expected WALLFACER_AGENT=codex env, got %q", got)
	}
	if !strings.Contains(got, "--mount type=bind,src="+tmp+",dst=/workspace/"+base+",z") {
		t.Fatalf("expected repository workspace mount, got %q", got)
	}
}

// TestParseExecConfig_MissingPrefix verifies that no positional args in task
// mode returns an error.
func TestParseExecConfig_MissingPrefix(t *testing.T) {
	_, err := parseExecConfig(nil, []string{"bash"})
	if err == nil {
		t.Fatal("expected error for missing prefix")
	}
	if !strings.Contains(err.Error(), "missing task-id-prefix") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestParseExecConfig_PrefixTooShort verifies that a prefix shorter than 8
// characters is rejected.
func TestParseExecConfig_PrefixTooShort(t *testing.T) {
	_, err := parseExecConfig([]string{"abc"}, []string{"bash"})
	if err == nil {
		t.Fatal("expected error for short prefix")
	}
	if !strings.Contains(err.Error(), "at least 8 characters") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestParseExecConfig_SandboxMissingValue verifies that --sandbox without a
// value returns an error.
func TestParseExecConfig_SandboxMissingValue(t *testing.T) {
	_, err := parseExecConfig([]string{"--sandbox"}, []string{"bash"})
	if err == nil {
		t.Fatal("expected error for missing sandbox value")
	}
	if !strings.Contains(err.Error(), "missing sandbox value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBuildSandboxExecArgs_NoEnvFile verifies that sandbox exec args omit the
// --env-file flag when the env file does not exist.
func TestBuildSandboxExecArgs_NoEnvFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Chdir(tmp)

	args, err := buildSandboxExecArgs("/usr/bin/podman", tmp, "claude", []string{"bash"})
	if err != nil {
		t.Fatalf("buildSandboxExecArgs: %v", err)
	}

	got := strings.Join(args, " ")
	if strings.Contains(got, "--env-file") {
		t.Fatalf("expected no --env-file when file missing, got %q", got)
	}
}
