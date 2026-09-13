package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallerReleaseLookup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer requires a POSIX shell")
	}
	for _, failLookup := range []bool{false, true} {
		t.Run(map[bool]string{false: "large response", true: "partial response failure"}[failLookup], func(t *testing.T) {
			dir := t.TempDir()
			payload := "[\n{\"tag_name\": \"v9.8.7\"},\n" + strings.Repeat("{\"body\": \"release notes\"},\n", 100000) + "{\"tag_name\": \"v1.0.0\"}\n]\n"
			for name, body := range map[string]string{
				"releases.json": payload,
				"curl": `#!/bin/sh
case "$*" in
  *api.github.com*)
    cat "$INSTALL_TEST_PAYLOAD" || { echo 'curl: (23) Failure writing output to destination' >&2; exit 23; }
    if [ "$INSTALL_TEST_FAIL" = true ]; then exit 22; fi
    ;;
  *)
    while [ "$#" -gt 0 ]; do
      if [ "$1" = -o ]; then printf 'fake binary' > "$2"; exit 0; fi
      shift
    done
    exit 2
    ;;
esac
`,
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			_, source, _, _ := runtime.Caller(0)
			cmd := exec.CommandContext(t.Context(), "sh", filepath.Join(filepath.Dir(source), "install.sh"))
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"WALLFACER_VERSION=", "WALLFACER_INSTALL_DIR="+filepath.Join(dir, "bin"),
				"INSTALL_TEST_PAYLOAD="+filepath.Join(dir, "releases.json"),
				"INSTALL_TEST_FAIL="+map[bool]string{false: "false", true: "true"}[failLookup])
			out, err := cmd.CombinedOutput()
			if failLookup {
				if err == nil || strings.Contains(string(out), "Installed wallfacer") {
					t.Fatalf("failed lookup installed a binary: %v\n%s", err, out)
				}
				return
			}
			if err != nil || strings.Contains(string(out), "curl:") {
				t.Fatalf("successful install emitted an error: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "Resolved latest version: v9.8.7") || !strings.Contains(string(out), "Settings") {
				t.Fatalf("missing version or credential setup guidance:\n%s", out)
			}
			body, err := os.ReadFile(filepath.Join(dir, "bin", "wallfacer"))
			if err != nil || string(body) != "fake binary" {
				t.Fatalf("installed binary = %q, %v", body, err)
			}
		})
	}
}
