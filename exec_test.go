package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestParseExecConfig_SandboxRejectsInvalidRuntime(t *testing.T) {
	_, err := parseExecConfig([]string{"--sandbox", "llama"}, []string{"bash"})
	if err == nil {
		t.Fatal("expected error for invalid runtime")
	}
}

func TestResolveSandboxImageForExec_CodexFromWallfacer(t *testing.T) {
	got := resolveSandboxImageForExec("wallfacer:latest", "codex")
	if got != "wallfacer-codex:latest" {
		t.Fatalf("expected wallfacer-codex:latest, got %q", got)
	}
}

func TestResolveSandboxImageForExec_CodexKeepsUnrelatedImage(t *testing.T) {
	got := resolveSandboxImageForExec("ghcr.io/acme/custom:tag", "codex")
	if got != "ghcr.io/acme/custom:tag" {
		t.Fatalf("expected unchanged image, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// resolveContainerByPrefix
// ---------------------------------------------------------------------------

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

func TestResolveContainerByPrefixEmptyOutput(t *testing.T) {
	_, err := resolveContainerByPrefix("", "249e9c9c")
	if err == nil {
		t.Fatal("expected error for empty ps output, got nil")
	}
	if !strings.Contains(err.Error(), "no running container") {
		t.Fatalf("expected 'no running container' in error, got: %v", err)
	}
}

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
	if !strings.Contains(got, "-v claude-config:/home/claude/.claude") {
		t.Fatalf("expected claude config mount, got %q", got)
	}
	if !strings.Contains(got, "-v "+tmp+":/workspace/"+base+":z") {
		t.Fatalf("expected repository workspace mount, got %q", got)
	}
}

func TestBuildSandboxExecArgs_UsesCodexAuthWhenAvailable(t *testing.T) {
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
	expectedWorkspaceMount := "-v " + tmp + ":/workspace/" + filepath.Base(tmp) + ":z"
	if !strings.Contains(got, expectedWorkspaceMount) {
		t.Fatalf("expected workspace mount, got %q", got)
	}
	if !strings.Contains(got, "-v "+authDir+":/home/codex/.codex:z,ro") {
		t.Fatalf("expected codex auth mount, got %q", got)
	}
	if !strings.Contains(got, "-v "+tmp+":/workspace/"+base+":z") {
		t.Fatalf("expected repository workspace mount, got %q", got)
	}
}

func TestResolveSandboxImageForExec_ClonesDockerImageTagAndDigest(t *testing.T) {
	got := resolveSandboxImageForExec("ghcr.io/acme/wallfacer:latest@sha256:12345", "codex")
	if got != "ghcr.io/acme/wallfacer-codex:latest@sha256:12345" {
		t.Fatalf("expected converted digest image, got %q", got)
	}
}
