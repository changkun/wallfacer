package cli

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestCheckRunArgs verifies that `wallfacer run` accepts flags only and
// refuses folder arguments with a message pointing at the workspace picker.
func TestCheckRunArgs(t *testing.T) {
	if err := checkRunArgs(nil); err != nil {
		t.Fatalf("checkRunArgs(no args) = %v, want nil", err)
	}
	for _, args := range [][]string{{"/repos/api"}, {"/repos/api", "/repos/web"}} {
		err := checkRunArgs(args)
		if err == nil {
			t.Fatalf("checkRunArgs(%v) = nil, want a refusal", args)
		}
		for _, want := range []string{"takes no folder arguments", args[len(args)-1], "workspace picker"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("checkRunArgs(%v) = %q, want it to contain %q", args, err, want)
			}
		}
	}
}

// TestRunServer_RefusesFolderArguments runs `wallfacer run /a /b` in a child
// process and checks it exits with status 2 and the refusal on stderr before
// starting anything.
func TestRunServer_RefusesFolderArguments(t *testing.T) {
	if os.Getenv("WALLFACER_TEST_RUN_FOLDER_ARGS") == "1" {
		RunServer(t.TempDir(), []string{"/repos/api", "/repos/web"}, nil, nil)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunServer_RefusesFolderArguments$")
	cmd.Env = append(os.Environ(), "WALLFACER_TEST_RUN_FOLDER_ARGS=1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("wallfacer run with folder arguments: err = %v, want exit status 2; stderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "wallfacer run takes no folder arguments (got /repos/api /repos/web)") {
		t.Errorf("stderr does not carry the refusal:\n%s", stderr.String())
	}
}
