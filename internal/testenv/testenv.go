// Package testenv runs a package's tests with the per-user directories pointed
// at a fresh temporary directory, so tests never touch the user's real sign-in
// or configuration.
//
// Code under test resolves per-user locations from the environment: the latere
// sign-in token at <os.UserConfigDir>/latere/token.json, which is shared with
// the latere command line; ~/.wallfacer, where the runner keeps user-authored
// agents and flows; and whatever the agent CLIs and shells a test spawns write
// under HOME. On macOS os.UserConfigDir ignores XDG_CONFIG_HOME and resolves
// under HOME, so overriding a single variable inside one test is not enough.
// RunIsolated overrides all of them once per test process, before any test
// runs, and every child process inherits the result.
package testenv

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// goEnvVars are the go command's per-user locations. They are pinned to the
// values they resolve to under the real home, so tests that run `go build` or
// `go list` keep the developer's build cache, module cache and go env file
// instead of starting from empty ones under the isolated home.
var goEnvVars = []string{"GOCACHE", "GOMODCACHE", "GOPATH", "GOENV"}

// goEnvTimeout bounds the `go env` call so a stalled go command delays the
// test binary's start by at most this long instead of hanging it.
const goEnvTimeout = 30 * time.Second

// RunIsolated runs the tests in m with HOME, USERPROFILE, XDG_CONFIG_HOME,
// XDG_DATA_HOME, XDG_CACHE_HOME, AppData and LocalAppData pointed into a fresh
// temporary directory, removes the directory, and returns the exit code to
// pass to os.Exit. It runs no test when the isolation cannot be established.
// Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testenv.RunIsolated(m)) }
func RunIsolated(m *testing.M) int {
	return runIsolated(m.Run, os.Stderr)
}

func runIsolated(run func() int, stderr io.Writer) int {
	pinGoEnv(stderr)
	root, err := os.MkdirTemp("", "wallfacer-test-home-*")
	if err != nil {
		fmt.Fprintf(stderr, "testenv: create the isolated home: %v\n", err)
		return 1
	}
	defer func() {
		// A leftover directory is reported rather than failing the run; the
		// repository's tempdir gate is the leak detector.
		if err := os.RemoveAll(root); err != nil {
			fmt.Fprintf(stderr, "testenv: remove the isolated home %s: %v\n", root, err)
		}
	}()
	if err := isolate(root); err != nil {
		fmt.Fprintf(stderr, "testenv: %v; no test ran\n", err)
		return 1
	}
	return run()
}

// isolate points every per-user directory variable into root, then confirms
// that the standard library resolves the home, config and cache directories
// inside it.
func isolate(root string) error {
	vars := [][2]string{
		{"HOME", root},
		{"USERPROFILE", root},
		{"XDG_CONFIG_HOME", filepath.Join(root, ".config")},
		{"XDG_DATA_HOME", filepath.Join(root, ".local", "share")},
		{"XDG_CACHE_HOME", filepath.Join(root, ".cache")},
		{"AppData", filepath.Join(root, "AppData", "Roaming")},
		{"LocalAppData", filepath.Join(root, "AppData", "Local")},
	}
	for _, kv := range vars {
		if err := os.Setenv(kv[0], kv[1]); err != nil {
			return fmt.Errorf("set %s: %w", kv[0], err)
		}
	}
	resolvers := []struct {
		name    string
		resolve func() (string, error)
	}{
		{"home", os.UserHomeDir},
		{"config", os.UserConfigDir},
		{"cache", os.UserCacheDir},
	}
	for _, r := range resolvers {
		dir, err := r.resolve()
		if err != nil {
			return fmt.Errorf("resolve the %s directory: %w", r.name, err)
		}
		if !Within(dir, root) {
			return fmt.Errorf("the %s directory resolves to %s, outside the isolated home %s", r.name, dir, root)
		}
	}
	return nil
}

// pinGoEnv sets each unset variable in goEnvVars to the value `go env` reports
// for it. It must run before isolate changes HOME, since the defaults derive
// from it. A failure is reported and leaves the variables unset, which costs
// tests that run the go command an empty cache but isolates nothing less.
func pinGoEnv(stderr io.Writer) {
	var unset []string
	for _, k := range goEnvVars {
		if os.Getenv(k) == "" {
			unset = append(unset, k)
		}
	}
	if len(unset) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), goEnvTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", append([]string{"env"}, unset...)...).Output()
	if err != nil {
		fmt.Fprintf(stderr, "testenv: go env: %v; the go command in tests uses caches under the isolated home\n", err)
		return
	}
	values := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	if len(values) != len(unset) {
		fmt.Fprintf(stderr, "testenv: go env printed %d values for %d variables; the go command in tests uses caches under the isolated home\n", len(values), len(unset))
		return
	}
	for i, k := range unset {
		v := strings.TrimSpace(values[i])
		if v == "" {
			continue
		}
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintf(stderr, "testenv: set %s: %v\n", k, err)
		}
	}
}

// Within reports whether path is dir or lies below it.
func Within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
