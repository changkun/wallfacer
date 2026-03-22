package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// runFakeCmd handles the re-exec mode where the test binary acts as a fake
// container command. Called from TestMain when WALLFACER_FAKE_MODE is set.
//
// Supported modes:
//   - "simple": write WALLFACER_FAKE_DIR/output.txt to stdout, exit with WALLFACER_FAKE_EXIT
//   - "stateful": counter-based; skip lifecycle commands (rm/kill/inspect/ps);
//     output out<N>.txt (or last.txt), increment counter
//   - "stderr": like simple but also writes stderr.txt to stderr; skips rm/kill
func runFakeCmd(mode string) {
	dir := os.Getenv("WALLFACER_FAKE_DIR")
	exitCode, _ := strconv.Atoi(os.Getenv("WALLFACER_FAKE_EXIT"))

	switch mode {
	case "simple":
		data, _ := os.ReadFile(filepath.Join(dir, "output.txt"))
		os.Stdout.Write(data)
		os.Exit(exitCode)

	case "stateful":
		// Skip container lifecycle subcommands without advancing the counter.
		if len(os.Args) > 1 {
			switch os.Args[1] {
			case "rm", "kill", "inspect", "ps":
				os.Exit(0)
			}
		}

		counterFile := filepath.Join(dir, "counter")
		countData, _ := os.ReadFile(counterFile)
		count, _ := strconv.Atoi(strings.TrimSpace(string(countData)))

		outFile := filepath.Join(dir, fmt.Sprintf("out%d.txt", count))
		if _, err := os.Stat(outFile); os.IsNotExist(err) {
			outFile = filepath.Join(dir, "last.txt")
		}
		data, _ := os.ReadFile(outFile)
		os.Stdout.Write(data)
		_ = os.WriteFile(counterFile, []byte(strconv.Itoa(count+1)), 0644)
		os.Exit(exitCode)

	case "stderr":
		// Skip container lifecycle subcommands.
		if len(os.Args) > 1 {
			switch os.Args[1] {
			case "rm", "kill":
				os.Exit(0)
			}
		}

		stdout, _ := os.ReadFile(filepath.Join(dir, "stdout.txt"))
		stderr, _ := os.ReadFile(filepath.Join(dir, "stderr.txt"))
		os.Stdout.Write(stdout)
		os.Stderr.Write(stderr)
		os.Exit(exitCode)
	}
}

// testBinary returns the path to the current test binary (os.Args[0]).
func testBinary() string { return os.Args[0] }

// fakeCmdScript creates a fake container command that writes output to stdout
// and exits with exitCode. The test binary is re-executed in "simple" mode.
func fakeCmdScript(t *testing.T, output string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "output.txt"), []byte(output), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WALLFACER_FAKE_MODE", "simple")
	t.Setenv("WALLFACER_FAKE_DIR", dir)
	t.Setenv("WALLFACER_FAKE_EXIT", strconv.Itoa(exitCode))
	return testBinary()
}

// fakeStatefulCmd creates a fake container command that returns different JSON
// outputs on successive invocations. Container lifecycle calls (rm, kill,
// inspect, ps) are skipped without advancing the counter.
func fakeStatefulCmd(t *testing.T, outputs []string) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "counter"), []byte("0"), 0644); err != nil {
		t.Fatal(err)
	}

	for i, o := range outputs {
		p := filepath.Join(dir, fmt.Sprintf("out%d.txt", i))
		if err := os.WriteFile(p, []byte(o), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// last.txt is the fallback when the counter exceeds the number of outputs.
	if err := os.WriteFile(filepath.Join(dir, "last.txt"), []byte(outputs[len(outputs)-1]), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WALLFACER_FAKE_MODE", "stateful")
	t.Setenv("WALLFACER_FAKE_DIR", dir)
	t.Setenv("WALLFACER_FAKE_EXIT", "0")
	return testBinary()
}

// fakeCmdScriptWithStderr creates a fake container command that writes stdout
// and stderr, then exits with exitCode. Lifecycle calls (rm, kill) are skipped.
func fakeCmdScriptWithStderr(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "stdout.txt"), []byte(stdout), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stderr.txt"), []byte(stderr), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WALLFACER_FAKE_MODE", "stderr")
	t.Setenv("WALLFACER_FAKE_DIR", dir)
	t.Setenv("WALLFACER_FAKE_EXIT", strconv.Itoa(exitCode))
	return testBinary()
}
