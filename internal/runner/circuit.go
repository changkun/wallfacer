package runner

// isContainerRuntimeError reports whether err signals a container runtime
// failure (the daemon or binary is unavailable) rather than a normal task
// exit from the agent process inside the container.
//
// Specifically it matches:
//   - Exit code 125: the container engine (Docker/Podman) itself failed.
//   - Non-exit errors with "connection refused" (daemon not running).
//   - Non-exit errors with "no such file or directory" (binary not found).
//
// Normal agent exit codes (1–124) are intentionally NOT matched.
func isContainerRuntimeError(err error) bool {
	if err == nil {
		return false
	}

	// Check for a process exit code via the ExitCode() interface.
	// cmd.Run() returns *exec.ExitError directly (not wrapped), so a direct
	// type assertion is sufficient. We define a local interface to avoid
	// importing os/exec in this file.
	type exitCoder interface{ ExitCode() int }
	if e, ok := err.(exitCoder); ok {
		// Exit code 125 = container engine error (not agent error).
		return e.ExitCode() == 125
	}

	// Non-exit error: check the message for daemon/binary unavailability.
	msg := err.Error()
	lower := make([]byte, len(msg))
	for i := 0; i < len(msg); i++ {
		c := msg[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		lower[i] = c
	}
	lmsg := string(lower)
	return containsSubstring(lmsg, "connection refused") ||
		containsSubstring(lmsg, "no such file or directory")
}

// containsSubstring is a simple substring check to keep imports minimal.
func containsSubstring(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
