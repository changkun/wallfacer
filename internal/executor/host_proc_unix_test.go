//go:build !windows

package executor

import (
	"bufio"
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestConfigureProcessGroup_GroupKillReapsChildren proves the load-bearing
// teardown property: an agent put in its own process group, then group-killed,
// takes its tool subprocesses down with it (no orphans). Without Setpgid + the
// negative-pid group kill, the inner `sleep` would survive the leader.
func TestConfigureProcessGroup_GroupKillReapsChildren(t *testing.T) {
	// sh spawns a background sleep (a stand-in for a build/test tool child),
	// prints its pid, then waits — so the child is a group member, not the leader.
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "sleep 30 & echo $!; wait")
	configureProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The leader is its own group leader (Setpgid took effect).
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err != nil || pgid != cmd.Process.Pid {
		t.Fatalf("Getpgid = %d, err %v; want own group (%d)", pgid, err, cmd.Process.Pid)
	}

	// Read the child sleep pid.
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	childPid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parse child pid %q: %v", line, err)
	}

	// Group-kill the leader; the child must be reaped too.
	if err := terminateGroupKill(cmd); err != nil {
		t.Fatalf("terminateGroupKill: %v", err)
	}
	_ = cmd.Wait()

	if alive := processAlive(t, childPid); alive {
		t.Errorf("child pid %d still alive after group kill; it was orphaned", childPid)
	}
}

// TestApplyAgentPriority_Linux asserts the configured niceness lands on the
// leader. Linux only: macOS uses the background-task policy, which does not
// change the readable nice value, so there is nothing to assert there.
func TestApplyAgentPriority_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("priority readback asserted on linux only; GOOS=%s", runtime.GOOS)
	}
	cmd := exec.CommandContext(context.Background(), "sleep", "30")
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = terminateGroupKill(cmd); _ = cmd.Wait() })

	applyAgentPriority(cmd.Process.Pid, 10)
	got, err := unix.Getpriority(unix.PRIO_PROCESS, cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Getpriority: %v", err)
	}
	// Getpriority returns the nice value mapped to 0..40 (20 - nice) on Linux's
	// glibc; x/sys returns the raw kernel value (20 - nice). Accept either the
	// raw nice (10) or the mapped form (10) — both resolve to nice 10.
	if got != 10 {
		t.Errorf("leader priority = %d, want nice 10", got)
	}

	// nice 0 (disabled) must leave priority unchanged.
	cmd2 := exec.CommandContext(context.Background(), "sleep", "30")
	configureProcessGroup(cmd2)
	if err := cmd2.Start(); err != nil {
		t.Fatalf("start cmd2: %v", err)
	}
	t.Cleanup(func() { _ = terminateGroupKill(cmd2); _ = cmd2.Wait() })
	before, _ := unix.Getpriority(unix.PRIO_PROCESS, cmd2.Process.Pid)
	applyAgentPriority(cmd2.Process.Pid, 0)
	after, _ := unix.Getpriority(unix.PRIO_PROCESS, cmd2.Process.Pid)
	if before != after {
		t.Errorf("nice 0 changed priority: %d -> %d", before, after)
	}
}

// processAlive reports whether pid is still a live process (signal 0 probe).
func processAlive(t *testing.T, pid int) bool {
	t.Helper()
	// Give the kernel a moment to tear the group down.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return false // ESRCH: gone
		}
		time.Sleep(20 * time.Millisecond)
	}
	return syscall.Kill(pid, 0) == nil
}
