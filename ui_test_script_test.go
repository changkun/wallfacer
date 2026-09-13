package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUITestMakeTargetStartsWithBash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the browser job runs on Linux")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Dir(source)
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(root, "frontend/scripts/ui-shots/ui-test.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// Execute the real target and script preamble without building the app or
	// installing a browser. Dash reproduces Ubuntu's /bin/sh on macOS too.
	preamble, _, found := strings.Cut(string(script), "\ncd ")
	if !found {
		t.Fatal("UI script has no repository setup boundary")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "frontend/scripts/ui-shots/ui-test.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(preamble+"\nprintf 'script started\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), makefile, 0o644); err != nil {
		t.Fatal(err)
	}
	if dash, err := exec.LookPath("dash"); err == nil {
		if err := os.Symlink(dash, filepath.Join(dir, "sh")); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("make", "ui-test")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "script started") {
		t.Fatalf("make ui-test could not start its script: %v\n%s", err, out)
	}
}

func TestUITestSeedsAgentBinariesWithoutAnInstalledAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the browser job runs on Linux")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Dir(source)
	body, err := os.ReadFile(filepath.Join(root, "frontend/scripts/ui-shots/ui-test.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, seed, found := strings.Cut(string(body), "echo \"==> Seeding deterministic demo data\"")
	if !found {
		t.Fatal("UI harness has no seed step")
	}
	seed, _, found = strings.Cut(seed, "echo \"==> Ensuring playwright sandbox")
	if !found {
		t.Fatal("UI harness has no browser setup boundary")
	}
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cmd := exec.Command("bash", "-e", "-c", seed)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "HOME_DIR="+home, "DATA="+filepath.Join(dir, "data"), "WS="+filepath.Join(dir, "ws"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed UI fixture: %v\n%s", err, out)
	}
	env, err := os.ReadFile(filepath.Join(home, ".wallfacer/.env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"WALLFACER_HOST_CLAUDE_BINARY=", "WALLFACER_HOST_CODEX_BINARY="} {
		var binary string
		for line := range strings.SplitSeq(string(env), "\n") {
			if value, ok := strings.CutPrefix(line, key); ok {
				binary = value
			}
		}
		info, err := os.Stat(binary)
		if err != nil || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("seeded %s has no executable fixture: %q (%v)", key, binary, err)
		}
	}
}
