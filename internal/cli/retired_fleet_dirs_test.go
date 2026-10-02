package cli

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/logger"
)

// seedRetiredFile writes a definition file into configDir/<dir>/ the way the
// removed agent and fleet stores left them, and returns its path.
func seedRetiredFile(t *testing.T, configDir, dir, name string, body []byte, mode os.FileMode) string {
	t.Helper()
	d := filepath.Join(configDir, dir)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(d, name)
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	return path
}

// TestWarnRetiredFleetDirs_FlowsFileWarnsOnceAndReadsNothing seeds a fleet
// definition in flows/ with no read permission and checks that one start logs
// exactly one warning naming the directory, that the file is not read (an
// open would fail and be reported), and that it is left on disk unchanged.
func TestWarnRetiredFleetDirs_FlowsFileWarnsOnceAndReadsNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads files whatever their permissions, so an unreadable file cannot be made")
	}
	configDir := t.TempDir()
	body := []byte("slug: review-fleet\nname: Review\nsteps:\n  - agent_slug: impl\n")
	path := seedRetiredFile(t, configDir, "flows", "review-fleet.yaml", body, 0o000)
	t.Cleanup(func() {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Errorf("restore permissions: %v", err)
		}
	})

	var buf bytes.Buffer
	warnRetiredFleetDirs(configDir, slog.New(slog.NewTextHandler(&buf, nil)))

	lines := logLines(&buf)
	if len(lines) != 1 {
		t.Fatalf("logged %d lines, want 1: %q", len(lines), lines)
	}
	dir := filepath.Join(configDir, "flows")
	if !strings.Contains(lines[0], "level=WARN") || !strings.Contains(lines[0], dir) || !strings.Contains(lines[0], "no longer used") {
		t.Errorf("log line = %q, want a WARN line naming %s and saying its contents are no longer used", lines[0], dir)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("definition file is gone: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("definition file changed: %q, want %q", got, body)
	}
}

// TestWarnRetiredFleetDirs_BothDirectories checks that agents/ and flows/
// each get their own warning when both hold a file.
func TestWarnRetiredFleetDirs_BothDirectories(t *testing.T) {
	configDir := t.TempDir()
	seedRetiredFile(t, configDir, "agents", "impl-codex.yaml", []byte("slug: impl-codex\n"), 0o600)
	seedRetiredFile(t, configDir, "flows", "pair.yml", []byte("slug: pair\n"), 0o600)

	var buf bytes.Buffer
	warnRetiredFleetDirs(configDir, slog.New(slog.NewTextHandler(&buf, nil)))

	lines := logLines(&buf)
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2: %q", len(lines), lines)
	}
	for i, name := range []string{"agents", "flows"} {
		if dir := filepath.Join(configDir, name); !strings.Contains(lines[i], dir) {
			t.Errorf("line %d = %q, want it to name %s", i, lines[i], dir)
		}
	}
}

// TestWarnRetiredFleetDirs_NothingToSay covers the quiet cases: no
// directories, empty directories, and a directory holding only a
// subdirectory. None of them stores a definition, so nothing is logged.
func TestWarnRetiredFleetDirs_NothingToSay(t *testing.T) {
	configDir := t.TempDir()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	warnRetiredFleetDirs(configDir, log)
	if err := os.MkdirAll(filepath.Join(configDir, "agents"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "flows", "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	warnRetiredFleetDirs(configDir, log)

	if lines := logLines(&buf); len(lines) != 0 {
		t.Fatalf("logged %d lines with no definition files, want none: %q", len(lines), lines)
	}
}

// TestWarnRetiredFleetDirs_EmptyConfigDir checks that an empty config dir does
// not resolve the directories under the working directory.
func TestWarnRetiredFleetDirs_EmptyConfigDir(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	seedRetiredFile(t, cwd, "flows", "pair.yaml", []byte("slug: pair\n"), 0o600)

	var buf bytes.Buffer
	warnRetiredFleetDirs("", slog.New(slog.NewTextHandler(&buf, nil)))
	if lines := logLines(&buf); len(lines) != 0 {
		t.Fatalf("logged %d lines for an empty config dir, want none: %q", len(lines), lines)
	}
}

// TestInitServerWarnsAboutRetiredFlowsDir boots the run tree over a config
// dir whose flows/ holds a definition and checks that startup logs the
// warning exactly once and leaves the file in place. The server's local log
// handler writes to os.Stdout, so the test points it at a temp file for the
// duration of the boot and rebinds the package loggers afterwards. HOME,
// XDG_CONFIG_HOME and AppData point at a temp dir so the run tree's shared
// token store is not the developer's own sign-in file.
func TestInitServerWarnsAboutRetiredFlowsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("AppData", home)
	t.Setenv("AUTH_REDIRECT_URL", "")

	configDir := t.TempDir()
	body := []byte("slug: pair\n")
	path := seedRetiredFile(t, configDir, "flows", "pair.yaml", body, 0o600)
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	out, err := os.Create(filepath.Join(t.TempDir(), "stdout.log"))
	if err != nil {
		t.Fatalf("create log capture: %v", err)
	}
	stdout := os.Stdout
	os.Stdout = out
	t.Cleanup(func() {
		os.Stdout = stdout
		logger.Init("text")
		if err := out.Close(); err != nil {
			t.Errorf("close log capture: %v", err)
		}
	})

	sc := initServer(configDir, ServerConfig{
		LogFormat: "json",
		Addr:      ":0",
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))
	sc.Shutdown()

	captured, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatalf("read log capture: %v", err)
	}
	dir := filepath.Join(configDir, "flows")
	var hits []string
	for line := range strings.SplitSeq(string(captured), "\n") {
		if strings.Contains(line, "retired agent and fleet directory") {
			hits = append(hits, line)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("startup logged %d retired-directory lines, want 1: %q", len(hits), hits)
	}
	if !strings.Contains(hits[0], dir) {
		t.Errorf("warning %q does not name %s", hits[0], dir)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, body) {
		t.Errorf("definition file after startup = %q, %v; want it unchanged on disk", got, err)
	}
}
