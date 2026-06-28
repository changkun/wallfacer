//go:build !windows

package executor

import (
	"os"
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the agent in its own process group (Setpgid) so the
// whole tool subtree it spawns — builds, test runners, ripgrep — can be priority
// throttled and reaped together. It also rewires context cancellation to
// terminate the group rather than only the leader, which would otherwise orphan
// the (CPU-heavy) tool children.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return terminateGroupKill(cmd) }
}

// terminateGroupSignal sends sig to the agent's process group (negative pid
// targets the group, whose leader pid equals the agent pid under Setpgid).
// Falls back to signalling the leader alone if the group send fails.
func terminateGroupSignal(cmd *exec.Cmd, sig os.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	if ssig, ok := sig.(syscall.Signal); ok {
		if err := syscall.Kill(-cmd.Process.Pid, ssig); err == nil {
			return nil
		}
	}
	return cmd.Process.Signal(sig)
}

// terminateGroupKill hard-kills the agent's process group, falling back to the
// leader. Used for the SIGKILL escalation and for context-cancel teardown.
func terminateGroupKill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}
