package testenv

import (
	"bytes"
	"context"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/harness"
)

// writeExecutable creates an executable file named name in dir holding script.
// On Windows the file gets the .exe extension exec.LookPath expects there; its
// content is never run, since only the lookup is under test on that platform.
func writeExecutable(t *testing.T, dir, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write the executable %s: %v", name, err)
	}
}

// presetGoEnv sets the go command's variables so runIsolated does not run go
// env, which keeps its diagnostics empty whatever PATH the test installs.
func presetGoEnv(t *testing.T) {
	t.Helper()
	for _, k := range goEnvVars {
		t.Setenv(k, "preset")
	}
}

func TestAgentCLIsNameEverySubprocessHarness(t *testing.T) {
	for _, id := range harness.All() {
		_, listed := AgentCLIs[id]
		switch {
		case harness.InProcess(id) && listed:
			t.Errorf("harness %s runs in process and launches no CLI, but AgentCLIs lists it", id)
		case !harness.InProcess(id) && !listed:
			t.Errorf("harness %s launches a CLI that AgentCLIs does not name, so tests can launch the real one; add the executable name the executor resolves for it", id)
		}
	}
	for id := range AgentCLIs {
		if !id.IsValid() {
			t.Errorf("AgentCLIs lists %s, which is not a registered harness", id)
		}
	}
}

// TestAgentCLIsNameWhatTheExecutorLaunches puts a stand-in under each listed
// name on PATH, launches every subprocess harness through a host backend with
// no explicit binary path, and checks each launch ran the stand-in under that
// harness's name. A CLI the executor resolves under any other name would pass
// the PATH filter and reach the real binary.
func TestAgentCLIsNameWhatTheExecutorLaunches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in CLIs are POSIX shell scripts")
	}
	bin, marks := t.TempDir(), t.TempDir()
	for _, name := range AgentCLIs {
		writeExecutable(t, bin, name, "#!/bin/sh\n: > '"+filepath.Join(marks, name)+"'\n")
	}
	t.Setenv("PATH", bin)
	b, err := executor.NewHostBackend(executor.HostBackendConfig{AgentNice: -1})
	if err != nil {
		t.Fatalf("build the host backend: %v", err)
	}
	for _, id := range slices.Sorted(maps.Keys(AgentCLIs)) {
		name := AgentCLIs[id]
		if err := launchAndReap(b, executor.ContainerSpec{
			Name:    "wallfacer-testenv-" + string(id),
			Env:     map[string]string{"WALLFACER_AGENT": string(id)},
			Cmd:     []string{"-p", "testenv"},
			WorkDir: t.TempDir(),
		}); err != nil {
			t.Errorf("launch the %s harness with %q on PATH: %v", id, name, err)
			continue
		}
		if _, err := os.Stat(filepath.Join(marks, name)); err != nil {
			t.Errorf("launching the %s harness did not run %q from PATH: %v", id, name, err)
		}
	}
}

// launchAndReap launches spec on b, drains both output streams to EOF and
// waits for the process, in the order the runner uses: Wait closes the pipes,
// so waiting first could cut the drain short.
func launchAndReap(b *executor.HostBackend, spec executor.ContainerSpec) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := b.Launch(ctx, spec)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, r := range []io.Reader{h.Stdout(), h.Stderr()} {
		wg.Go(func() {
			_, errs[i] = io.Copy(io.Discard, r)
		})
	}
	wg.Wait()
	if _, err := h.Wait(); err != nil {
		return err
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func TestRunIsolatedRemovesAgentCLIDirectoriesAndKeepsTheRest(t *testing.T) {
	restoreEnvAfter(t)
	presetGoEnv(t)
	keep := t.TempDir()
	writeExecutable(t, keep, "wallfacer-testenv-tool", "#!/bin/sh\nexit 0\n")
	// One directory per CLI name, so each name on its own must trigger the
	// removal; the kept directory sits between them to check the order.
	var agentDirs []string
	for _, name := range AgentCLIs {
		dir := t.TempDir()
		writeExecutable(t, dir, name, "#!/bin/sh\nexit 0\n")
		agentDirs = append(agentDirs, dir)
	}
	rest := filepath.SplitList(os.Getenv("PATH"))
	entries := append([]string{agentDirs[0], keep}, agentDirs[1:]...)
	t.Setenv("PATH", strings.Join(append(entries, rest...), string(os.PathListSeparator)))

	var gotPath string
	var found []string
	var toolErr error
	var stderr bytes.Buffer
	code := runIsolated(func() int {
		gotPath = os.Getenv("PATH")
		found = AgentCLIsOnPath()
		_, toolErr = exec.LookPath("wallfacer-testenv-tool")
		return 0
	}, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; diagnostics: %s", code, stderr.String())
	}
	if want := strings.Join(append([]string{keep}, rest...), string(os.PathListSeparator)); gotPath != want {
		t.Errorf("PATH inside the run = %q, want %q", gotPath, want)
	}
	if len(found) > 0 {
		t.Errorf("agent CLIs resolve inside the run: %v", found)
	}
	if toolErr != nil {
		t.Errorf("an executable outside the agent CLI directories no longer resolves: %v", toolErr)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected diagnostics: %s", stderr.String())
	}
}

func TestRunIsolatedReportsAToolRemovedWithAnAgentCLI(t *testing.T) {
	restoreEnvAfter(t)
	presetGoEnv(t)
	dir := t.TempDir()
	writeExecutable(t, dir, AgentCLIs[harness.Claude], "#!/bin/sh\nexit 0\n")
	writeExecutable(t, dir, "git", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	var gitErr error
	var stderr bytes.Buffer
	code := runIsolated(func() int {
		_, gitErr = exec.LookPath("git")
		return 0
	}, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; diagnostics: %s", code, stderr.String())
	}
	if gitErr == nil {
		t.Error("git still resolves although its only directory also holds an agent CLI")
	}
	if !strings.Contains(stderr.String(), "also hides git") {
		t.Errorf("diagnostics do not report the hidden git: %q", stderr.String())
	}
}
