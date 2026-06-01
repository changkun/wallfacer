package runner

import "changkun.de/x/wallfacer/internal/harness"

// Test-only wrappers that call the ContainerSpec builders and flatten the result
// into CLI argument slices via Build(). These exist in a non-_test.go file with
// the "runner" package so they can access unexported methods, but the _test.go
// suffix ensures they are only compiled during testing.

func (r *Runner) buildContainerArgs(
	containerName, taskID, prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
) []string {
	return r.buildContainerSpecForSandbox(containerName, taskID, prompt, sessionID, worktreeOverrides, boardDir, siblingMounts, modelOverride, harness.Claude).Build()
}

func (r *Runner) buildContainerArgsForSandbox(
	containerName, taskID, prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
	sb harness.ID,
) []string {
	return r.buildContainerSpecForSandbox(containerName, taskID, prompt, sessionID, worktreeOverrides, boardDir, siblingMounts, modelOverride, sb).Build()
}
