package runner

import "changkun.de/x/wallfacer/internal/sandbox"

// Test-only wrappers that call the Spec builders and return CLI args.

func (r *Runner) buildContainerArgs(
	containerName, taskID, prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
) []string {
	return r.buildContainerSpecForSandbox(containerName, taskID, prompt, sessionID, worktreeOverrides, boardDir, siblingMounts, modelOverride, sandbox.Claude).Build()
}

func (r *Runner) buildContainerArgsForSandbox(
	containerName, taskID, prompt, sessionID string,
	worktreeOverrides map[string]string,
	boardDir string,
	siblingMounts map[string]map[string]string,
	modelOverride string,
	sb sandbox.Type,
) []string {
	return r.buildContainerSpecForSandbox(containerName, taskID, prompt, sessionID, worktreeOverrides, boardDir, siblingMounts, modelOverride, sb).Build()
}

func (r *Runner) buildIdeationContainerArgs(containerName, prompt string, sb sandbox.Type) []string {
	return r.buildIdeationContainerSpec(containerName, prompt, sb).Build()
}
