package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExec_Helper(t *testing.T) {
	if os.Getenv("WALLFACER_EXEC_HELPER") != "1" {
		return
	}

	configDir := os.Getenv("WALLFACER_EXEC_CONFIG")
	t.Setenv("CONTAINER_CMD", os.Getenv("WALLFACER_EXEC_RUNTIME"))

	switch os.Getenv("WALLFACER_EXEC_MODE") {
	case "task":
		runExec(configDir, []string{os.Getenv("WALLFACER_EXEC_PREFIX"), "bash"})
	case "sandbox":
		runExec(configDir, []string{"--sandbox", os.Getenv("WALLFACER_EXEC_SANDBOX"), "bash"})
	default:
		panic("WALLFACER_EXEC_MODE must be task or sandbox")
	}
}

func TestRunExec_TaskMode_Subprocess(t *testing.T) {
	tmp := t.TempDir()
	runtime := filepath.Join(tmp, "podman")
	marker := filepath.Join(tmp, "task.args")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"ps\" ]; then\n" +
		"\techo \"wallfacer-task-12345678\" \n" +
		"\texit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"exec\" ]; then\n" +
		"\techo \"$*\" > \"$WALLFACER_EXEC_MARKER\"\n" +
		"\texit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(runtime, []byte(script), 0o755); err != nil {
		t.Fatalf("write runtime script: %v", err)
	}

	cmd := exec.Command(
		os.Args[0],
		"-test.run=TestRunExec_Helper",
		"-test.count=1",
	)
	cmd.Env = append(os.Environ(),
		"WALLFACER_EXEC_HELPER=1",
		"WALLFACER_EXEC_MODE=task",
		"WALLFACER_EXEC_RUNTIME="+runtime,
		"WALLFACER_EXEC_CONFIG="+tmp,
		"WALLFACER_EXEC_PREFIX=12345678",
		"WALLFACER_EXEC_MARKER="+marker,
		"CONTAINER_CMD="+runtime,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run task mode helper: %v, output: %s", err, string(out))
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(got), "exec -it wallfacer-task-12345678 bash") {
		t.Fatalf("expected task-mode container exec arguments, got %q", string(got))
	}
}

func TestRunExec_SandboxMode_Subprocess(t *testing.T) {
	tmp := t.TempDir()
	runtime := filepath.Join(tmp, "podman")
	marker := filepath.Join(tmp, "sandbox.args")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"run\" ]; then\n" +
		"\techo \"$*\" > \"$WALLFACER_EXEC_MARKER\"\n" +
		"\texit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(runtime, []byte(script), 0o755); err != nil {
		t.Fatalf("write runtime script: %v", err)
	}

	cmd := exec.Command(
		os.Args[0],
		"-test.run=TestRunExec_Helper",
		"-test.count=1",
	)
	cmd.Env = append(os.Environ(),
		"WALLFACER_EXEC_HELPER=1",
		"WALLFACER_EXEC_MODE=sandbox",
		"WALLFACER_EXEC_RUNTIME="+runtime,
		"WALLFACER_EXEC_CONFIG="+tmp,
		"WALLFACER_EXEC_SANDBOX=codex",
		"WALLFACER_EXEC_MARKER="+marker,
		"CONTAINER_CMD="+runtime,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run sandbox mode helper: %v, output: %s", err, string(out))
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(got), "run --rm -it --network=host") {
		t.Fatalf("expected sandbox run arguments, got %q", string(got))
	}
}
