//go:build darwin

package executor

import "golang.org/x/sys/unix"

// XNU background-task-policy constants. golang.org/x/sys/unix does not export
// them, so they are defined here from <sys/resource.h>.
const (
	prioDarwinProcess = 4      // PRIO_DARWIN_PROCESS: target another pid's task policy
	prioDarwinBG      = 0x1000 // PRIO_DARWIN_BG: enable background throttling
)

// applyAgentPriority throttles the agent process on macOS. Plain nice is a weak
// signal here, so it enables the XNU background-task policy (the syscall behind
// `taskpolicy -b`): CPU and I/O are throttled and the process is steered onto
// efficiency cores. The policy is inherited by descendants spawned afterward, so
// the agent's build/test/ripgrep children are throttled too — which is the actual
// CPU sink. Best-effort: nice == 0 disables; on EPERM (some XNU versions only
// allow a process to background itself) it falls back to plain group nice.
func applyAgentPriority(pid, nice int) {
	if nice == 0 {
		return
	}
	if err := unix.Setpriority(prioDarwinProcess, pid, prioDarwinBG); err == nil {
		return
	}
	_ = unix.Setpriority(unix.PRIO_PGRP, pid, nice)
}
