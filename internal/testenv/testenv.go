// Package testenv runs a package's tests with the per-user directories pointed
// at a fresh temporary directory and the coding-agent CLIs hidden from PATH, so
// tests never touch the user's real sign-in or configuration and never launch
// a real agent.
//
// Code under test resolves per-user locations from the environment: the latere
// sign-in token at <os.UserConfigDir>/latere/token.json, which is shared with
// the latere command line; ~/.wallfacer, where the runner keeps user-authored
// agents and flows; and whatever the shells a test spawns write under HOME. On
// macOS os.UserConfigDir ignores XDG_CONFIG_HOME and resolves under HOME, so
// overriding a single variable inside one test is not enough. RunIsolated
// overrides all of them once per test process, before any test runs, and every
// child process inherits the result.
//
// Code under test also resolves the agent CLIs from PATH: a runner or handler
// built without an explicit binary path looks up claude, codex and the others
// there, and several handlers start an agent run on their own. On a developer
// machine the real CLI can authenticate from the operating system's credential
// store whatever HOME is, so such a test would run real prompts, with
// permission prompts disabled, on the developer's account. RunIsolated removes
// every PATH directory that holds one of AgentCLIs, which reproduces a CI
// runner, where none is installed. A test that needs an agent passes an
// explicit path to a fake or puts a fake on PATH itself.
package testenv

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/harness"
)

// AgentCLIs maps every harness that runs as a subprocess to the executable
// name the executor resolves on PATH when no explicit binary path is
// configured. The executor keeps these names as literals, so the map is kept
// truthful by tests: one fails when a registered subprocess harness has no
// entry, another launches each harness through the executor and checks that
// it ran the executable under the listed name.
var AgentCLIs = map[harness.ID]string{
	harness.Claude:   "claude",
	harness.Codex:    "codex",
	harness.Cursor:   "cursor-agent",
	harness.OpenCode: "opencode",
	harness.Pi:       "pi",
}

// AgentCLIsOnPath returns, sorted, the path exec.LookPath resolves for each
// name in AgentCLIs. Inside a test process set up by RunIsolated it returns
// none; guard tests assert that, so a change that lets a test reach a real
// agent CLI fails instead of launching it.
func AgentCLIsOnPath() []string {
	var found []string
	for _, name := range AgentCLIs {
		if path, err := exec.LookPath(name); err == nil {
			found = append(found, path)
		}
	}
	slices.Sort(found)
	return found
}

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
// temporary directory and with every PATH directory that holds one of
// AgentCLIs removed, removes the temporary directory, and returns the exit
// code to pass to os.Exit. It runs no test when the isolation cannot be
// established. Call it from TestMain:
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
	if err := hideAgentCLIs(stderr); err != nil {
		fmt.Fprintf(stderr, "testenv: %v; no test ran\n", err)
		return 1
	}
	return run()
}

// pathTools are executables the tests run through PATH. Removing a directory
// removes everything in it, so hideAgentCLIs reports when that leaves one of
// these unresolvable: tests that run it would otherwise fail without saying
// why.
var pathTools = []string{"git", "go"}

// hideAgentCLIs removes from PATH every directory that holds one of AgentCLIs,
// keeping the remaining entries in order, then confirms exec.LookPath resolves
// none of the CLIs. A directory is removed whole because a PATH lookup cannot
// skip a single file in it.
func hideAgentCLIs(stderr io.Writer) error {
	var kept, removed []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if holdsAgentCLI(dir) {
			removed = append(removed, dir)
		} else {
			kept = append(kept, dir)
		}
	}
	if len(removed) > 0 {
		var resolvable []string
		for _, tool := range pathTools {
			if _, err := exec.LookPath(tool); err == nil {
				resolvable = append(resolvable, tool)
			}
		}
		if err := os.Setenv("PATH", strings.Join(kept, string(os.PathListSeparator))); err != nil {
			return fmt.Errorf("set PATH: %w", err)
		}
		for _, tool := range resolvable {
			if _, err := exec.LookPath(tool); err != nil {
				fmt.Fprintf(stderr, "testenv: removing %s from PATH to hide the agent CLIs also hides %s; tests that run it fail\n", strings.Join(removed, string(os.PathListSeparator)), tool)
			}
		}
	}
	if found := AgentCLIsOnPath(); len(found) > 0 {
		return fmt.Errorf("agent CLIs still resolve from PATH: %s", strings.Join(found, ", "))
	}
	return nil
}

// holdsAgentCLI reports whether dir contains an executable named in AgentCLIs.
// The probe path is joined by hand: given a path with a separator,
// exec.LookPath checks that one file, adding the PATHEXT extensions on
// Windows, instead of searching PATH, and filepath.Join would reduce
// "./claude" to "claude".
func holdsAgentCLI(dir string) bool {
	if dir == "" {
		dir = "." // an empty PATH entry names the current directory
	}
	for _, name := range AgentCLIs {
		if _, err := exec.LookPath(dir + string(filepath.Separator) + name); err == nil {
			return true
		}
	}
	return false
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
