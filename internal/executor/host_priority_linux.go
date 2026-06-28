//go:build linux

package executor

import "golang.org/x/sys/unix"

// applyAgentPriority lowers the scheduling priority of the agent's process group
// on Linux so the agent and its tool children (builds, test runners, ripgrep)
// yield CPU to the foreground (the wallfacer server and the user's apps).
// nice is applied to the group (PRIO_PGRP), so children forked under the group
// inherit it. Best-effort: nice == 0 leaves priority unchanged.
func applyAgentPriority(pid, nice int) {
	if nice == 0 {
		return
	}
	_ = unix.Setpriority(unix.PRIO_PGRP, pid, nice)
}
