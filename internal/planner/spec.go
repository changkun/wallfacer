package planner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"changkun.de/x/wallfacer/internal/pkg/sanitize"
	"changkun.de/x/wallfacer/internal/sandbox"
)

// buildContainerSpec creates a ContainerSpec for the planning sandbox.
// Workspaces are mounted read-only; each workspace's specs/ subdirectory
// is mounted read-write on top (container runtimes apply later mounts
// over earlier ones, so specs/ is writable while the rest is read-only).
func (p *Planner) buildContainerSpec(containerName string, sb sandbox.Type) sandbox.ContainerSpec {
	spec := sandbox.ContainerSpec{
		Runtime: p.command,
		Name:    containerName,
		Image:   p.image,
		Labels: map[string]string{
			"wallfacer.task.id":       planningTaskID,
			"wallfacer.task.activity": "planning",
		},
		// The host backend (and the container-based backends' entrypoint
		// script) dispatch to the right CLI based on WALLFACER_AGENT. The
		// planner is Claude-only today; the parameter is still threaded
		// through so a future Codex planner variant slots in without
		// touching this call site.
		Env: map[string]string{"WALLFACER_AGENT": string(sb)},
	}

	if p.envFile != "" {
		spec.EnvFile = p.envFile
	}

	// claude-config named volume for agent configuration persistence.
	spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
		Host:      "claude-config",
		Container: "/home/agent/.claude",
		Named:     true,
	})

	var basenames []string
	for _, ws := range p.workspaces {
		ws = strings.TrimSpace(ws)
		if ws == "" {
			continue
		}
		basename := sanitize.Basename(ws)
		basenames = append(basenames, basename)

		// Mount the workspace read-only.
		spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
			Host:      ws,
			Container: "/workspace/" + basename,
			Options:   mountOpts("z", "ro"),
		})

		// Mount specs/ read-write on top of the read-only workspace.
		specsDir := filepath.Join(ws, "specs")
		if info, err := os.Stat(specsDir); err == nil && info.IsDir() {
			spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
				Host:      specsDir,
				Container: "/workspace/" + basename + "/specs",
				Options:   mountOpts("z"),
			})
		}
	}

	// Instructions file (CLAUDE.md / AGENTS.md) mounted read-only.
	spec.Volumes = p.appendInstructionsMount(spec.Volumes, sb, basenames)

	// Working directory. Container backends resolve /workspace/<basename>
	// through the volume mounts above; the host backend has no mounts, so
	// the WorkDir must already be a real host path. When the backend is a
	// HostBackend we also drop the mounts and entrypoint — they are
	// container-only concerns that HostBackend.Launch ignores anyway, but
	// clearing them here keeps diagnostics clean.
	hostMode := p.isHostBackend()
	switch {
	case hostMode:
		spec.Volumes = nil
		spec.Entrypoint = ""
		spec.WorkDir = p.hostWorkDir()
	case len(basenames) == 1:
		spec.WorkDir = "/workspace/" + basenames[0]
		spec.Entrypoint = "/usr/local/bin/entrypoint.sh"
	case len(basenames) > 0:
		spec.WorkDir = "/workspace"
		spec.Entrypoint = "/usr/local/bin/entrypoint.sh"
	default:
		spec.Entrypoint = "/usr/local/bin/entrypoint.sh"
	}

	// Resource limits and network from config.
	spec.Network = p.network
	spec.CPUs = p.cpus
	spec.Memory = p.memory

	return spec
}

// isHostBackend reports whether the configured sandbox backend runs the
// agent CLI as a host process (no container mounts). Checked via a
// type-assertion against *sandbox.HostBackend to avoid leaking backend
// knowledge into the rest of the planner.
func (p *Planner) isHostBackend() bool {
	_, ok := p.backend.(*sandbox.HostBackend)
	return ok
}

// hostWorkDir returns the first configured workspace as an absolute
// host path, to be used as the planner process's CWD in host mode. When
// no workspace is configured we fall back to an empty string and let
// the host backend inherit its own CWD.
func (p *Planner) hostWorkDir() string {
	for _, ws := range p.workspaces {
		if ws = strings.TrimSpace(ws); ws != "" {
			return ws
		}
	}
	return ""
}

// appendInstructionsMount adds the workspace AGENTS.md / CLAUDE.md file
// as a read-only mount, following the same pattern as the runner.
func (p *Planner) appendInstructionsMount(volumes []sandbox.VolumeMount, sb sandbox.Type, basenames []string) []sandbox.VolumeMount {
	if p.instructionsPath == "" {
		return volumes
	}
	if _, err := os.Stat(p.instructionsPath); err != nil {
		return volumes
	}
	filename := "CLAUDE.md"
	if sb == sandbox.Codex {
		filename = "AGENTS.md"
	}
	containerPath := "/workspace/" + filename
	if len(basenames) == 1 {
		containerPath = "/workspace/" + basenames[0] + "/" + filename
	}
	return append(volumes, sandbox.VolumeMount{
		Host:      p.instructionsPath,
		Container: containerPath,
		Options:   mountOpts("z", "ro"),
	})
}

// mountOpts returns volume mount options appropriate for the host OS.
// The "z" SELinux relabeling option is only included on Linux.
func mountOpts(opts ...string) string {
	if runtime.GOOS != "linux" {
		filtered := make([]string, 0, len(opts))
		for _, o := range opts {
			if o != "z" {
				filtered = append(filtered, o)
			}
		}
		return strings.Join(filtered, ",")
	}
	return strings.Join(opts, ",")
}
