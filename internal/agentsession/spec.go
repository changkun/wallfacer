package agentsession

import (
	"strings"

	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/harness"
)

// buildSpec creates a launch spec for the planning agent process. The
// planner runs the agent CLI as a host process in the first configured
// workspace; specs/ is a normal writable subdirectory of that cwd.
func (p *Runtime) buildSpec(name string, sb harness.ID) executor.ContainerSpec {
	spec := executor.ContainerSpec{
		Name: name,
		Labels: map[string]string{
			"wallfacer.task.id":       agentSessionTaskID,
			"wallfacer.task.activity": "agent-session",
		},
		// The host backend dispatches to the right CLI based on
		// WALLFACER_AGENT. The planner is Claude-only today; the parameter
		// is threaded through so a future Codex planner variant slots in
		// without touching this call site.
		Env:     map[string]string{"WALLFACER_AGENT": string(sb)},
		WorkDir: p.hostWorkDir(),
	}
	if p.envFile != "" {
		spec.EnvFile = p.envFile
	}
	return spec
}

// hostWorkDir returns the first configured workspace as an absolute host
// path, used as the planner process's CWD. Empty when no workspace is
// configured (the host backend then inherits its own CWD).
func (p *Runtime) hostWorkDir() string {
	for _, ws := range p.workspaces {
		if ws = strings.TrimSpace(ws); ws != "" {
			return ws
		}
	}
	return ""
}
