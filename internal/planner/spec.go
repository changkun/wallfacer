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
	}

	if p.envFile != "" {
		spec.EnvFile = p.envFile
	}

	// claude-config named volume for agent configuration persistence.
	spec.Volumes = append(spec.Volumes, sandbox.VolumeMount{
		Host:      "claude-config",
		Container: "/home/claude/.claude",
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

	// Working directory.
	if len(basenames) == 1 {
		spec.WorkDir = "/workspace/" + basenames[0]
	} else if len(basenames) > 0 {
		spec.WorkDir = "/workspace"
	}

	// Entrypoint for exec calls.
	spec.Entrypoint = "/usr/local/bin/entrypoint.sh"

	// Resource limits and network from config.
	spec.Network = p.network
	spec.CPUs = p.cpus
	spec.Memory = p.memory

	return spec
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
