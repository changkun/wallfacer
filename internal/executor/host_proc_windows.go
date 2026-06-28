//go:build windows

package executor

import (
	"os"
	"os/exec"
)

// configureProcessGroup is a no-op on Windows: there are no POSIX process groups
// to set, and CommandContext's default cancellation (kill the process) stands.
func configureProcessGroup(_ *exec.Cmd) {}

// terminateGroupSignal signals the leader; Windows has no group send. The caller
// treats a Signal error as the cue to escalate to a hard kill (Windows accepts
// only Kill for arbitrary signals).
func terminateGroupSignal(cmd *exec.Cmd, sig os.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(sig)
}

// terminateGroupKill hard-kills the leader.
func terminateGroupKill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
