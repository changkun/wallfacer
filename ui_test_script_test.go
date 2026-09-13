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
