package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleasePublicationCanBeRetriedAfterCreation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the release job runs on Linux")
	}
	_, source, _, _ := runtime.Caller(0)
	body, err := os.ReadFile(filepath.Join(filepath.Dir(source), ".github/workflows/release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, step, ok := strings.Cut(string(body), "      - name: Create GitHub release\n")
	if !ok {
		t.Fatal("release publish step is missing")
	}
	_, script, ok := strings.Cut(step, "        run: |\n")
	if !ok {
		t.Fatal("release publish script is missing")
	}
	var lines []string
	for line := range strings.SplitSeq(script, "\n") {
		if line != "" && !strings.HasPrefix(line, "          ") {
			break
		}
		lines = append(lines, strings.TrimPrefix(line, "          "))
	}
	dir := t.TempDir()
	for _, sub := range []string{"dist", "evidence"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"dist/wallfacer":               "binary",
		"evidence/release-evidence.md": "smoke passed\n",
		"gh": `#!/bin/sh
set -eu
printf '%s\n' "$1 $2" >> "$RELEASE_TEST_TRACE"
case "$1 $2" in
  'release view') test -f "$RELEASE_TEST_STATE" ;;
  'release create') test ! -f "$RELEASE_TEST_STATE"; touch "$RELEASE_TEST_STATE" ;;
  'release edit'|'release upload') test -f "$RELEASE_TEST_STATE" ;;
  *) exit 2 ;;
esac
`,
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("release notes\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), "bash", "-c", strings.Join(lines, "\n"))
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "TAG=v0.1.0", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
			"RELEASE_TEST_STATE="+filepath.Join(dir, "state"), "RELEASE_TEST_TRACE="+filepath.Join(dir, "trace"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("release publication failed: %v\n%s", err, out)
		}
	}
	trace, err := os.ReadFile(filepath.Join(dir, "trace"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(trace), "release create") != 1 || !strings.Contains(string(trace), "release edit") || !strings.Contains(string(trace), "release upload") {
		t.Fatalf("retry did not update the existing release and its artifacts:\n%s", trace)
	}
}
