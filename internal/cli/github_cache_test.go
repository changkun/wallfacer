package cli

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// logLines returns the non-empty lines a text slog handler wrote to buf.
func logLines(buf *bytes.Buffer) []string {
	var out []string
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// TestRemoveGitHubTokenCache_RemovesOnceAndLogsOnce seeds the directory the way
// the token store left it (a credential file per principal and a stray temp
// file) and checks that the first start removes it with one log line and the
// second start logs nothing.
func TestRemoveGitHubTokenCache_RemovesOnceAndLogsOnce(t *testing.T) {
	configDir := t.TempDir()
	dir := filepath.Join(configDir, githubTokenCacheDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"github-0123abcd.json", ".github-token-1.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"access_token":"ghu_x"}`), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	removeGitHubTokenCache(configDir, log)
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("token cache still present after the first start: %v", err)
	}
	lines := logLines(&buf)
	if len(lines) != 1 {
		t.Fatalf("first start logged %d lines, want 1: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "level=INFO") || !strings.Contains(lines[0], dir) {
		t.Errorf("first start log line = %q, want an INFO line naming %s", lines[0], dir)
	}

	buf.Reset()
	removeGitHubTokenCache(configDir, log)
	if lines := logLines(&buf); len(lines) != 0 {
		t.Fatalf("second start logged %d lines, want none: %q", len(lines), lines)
	}
}

// TestInitServerRemovesGitHubTokenCache boots the run tree over a config dir
// that carries a token cache and checks that startup removed it. HOME,
// XDG_CONFIG_HOME and AppData point at a temp dir so the run tree's shared
// token store is not the developer's own sign-in file.
func TestInitServerRemovesGitHubTokenCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("AppData", home)
	t.Setenv("AUTH_REDIRECT_URL", "")

	configDir := t.TempDir()
	cache := filepath.Join(configDir, githubTokenCacheDir)
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cache, "github-0123abcd.json"), []byte(`{"access_token":"ghu_x"}`), 0o600); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      ":0",
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))
	t.Cleanup(sc.Shutdown)

	if _, err := os.Lstat(cache); !os.IsNotExist(err) {
		t.Fatalf("token cache still present after startup: %v", err)
	}
}

// TestRemoveGitHubTokenCache_NoDirectoryLogsNothing covers an instance that
// never held a GitHub token: there is nothing to remove and nothing to say.
func TestRemoveGitHubTokenCache_NoDirectoryLogsNothing(t *testing.T) {
	configDir := t.TempDir()
	var buf bytes.Buffer
	removeGitHubTokenCache(configDir, slog.New(slog.NewTextHandler(&buf, nil)))
	if lines := logLines(&buf); len(lines) != 0 {
		t.Fatalf("logged %d lines with no cache directory, want none: %q", len(lines), lines)
	}
}

// TestRemoveGitHubTokenCache_EmptyConfigDirTouchesNothing checks that an empty
// config dir does not resolve to a github directory under the working
// directory and delete it.
func TestRemoveGitHubTokenCache_EmptyConfigDirTouchesNothing(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.Mkdir(githubTokenCacheDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var buf bytes.Buffer
	removeGitHubTokenCache("", slog.New(slog.NewTextHandler(&buf, nil)))
	if _, err := os.Lstat(filepath.Join(cwd, githubTokenCacheDir)); err != nil {
		t.Fatalf("a github directory under the working directory was removed: %v", err)
	}
	if lines := logLines(&buf); len(lines) != 0 {
		t.Fatalf("logged %d lines for an empty config dir, want none: %q", len(lines), lines)
	}
}

// TestRemoveGitHubTokenCache_FailureIsLoggedNotFatal makes the removal fail
// and checks that the failure is reported as a warning and the call returns,
// so startup continues.
func TestRemoveGitHubTokenCache_FailureIsLoggedNotFatal(t *testing.T) {
	configDir := t.TempDir()
	dir := filepath.Join(configDir, githubTokenCacheDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	orig := removeAll
	removeAll = func(string) error { return errors.New("remove refused") }
	t.Cleanup(func() { removeAll = orig })

	var buf bytes.Buffer
	removeGitHubTokenCache(configDir, slog.New(slog.NewTextHandler(&buf, nil)))
	lines := logLines(&buf)
	if len(lines) != 1 || !strings.Contains(lines[0], "level=WARN") || !strings.Contains(lines[0], "remove refused") {
		t.Fatalf("log = %q, want one WARN line carrying the error", lines)
	}
	if _, err := os.Lstat(dir); err != nil {
		t.Fatalf("directory should remain after a failed removal: %v", err)
	}
}
