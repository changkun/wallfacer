// Package planner manages the planning sandbox container lifecycle.
// The planning sandbox is a long-lived workspace-scoped container that
// lets the chat agent read the full workspace and write to specs/.
// It delegates to a [sandbox.Backend] for container operations so that
// any backend (local, K8s) can serve the planning container.
package planner

import (
	"context"
	"fmt"
	"sync"

	"changkun.de/x/wallfacer/internal/sandbox"
)

// planningTaskID is a fixed synthetic task ID used as the worker key for
// the planning container. The LocalBackend's worker container logic keys
// on the "wallfacer.task.id" label — using a stable ID means the backend
// reuses the same worker container across Launch calls.
const planningTaskID = "planning-sandbox"

// Config holds the configuration for a Planner.
type Config struct {
	Backend          sandbox.Backend // container backend (local, K8s, etc.)
	Command          string          // container runtime binary path (for ContainerSpec.Runtime)
	Image            string          // sandbox container image name
	Workspaces       []string        // workspace directory paths
	EnvFile          string          // path to .env file for container
	Fingerprint      string          // workspace fingerprint for keying the container
	InstructionsPath string          // path to AGENTS.md / CLAUDE.md instructions file
	Network          string          // container network (empty defaults to "host")
	CPUs             string          // container CPU limit (e.g. "2.0")
	Memory           string          // container memory limit (e.g. "4g")
}

// Planner manages a singleton long-lived planning container for a workspace.
type Planner struct {
	mu               sync.Mutex
	backend          sandbox.Backend
	command          string
	image            string
	workspaces       []string
	envFile          string
	fingerprint      string
	instructionsPath string
	network          string
	cpus             string
	memory           string

	handle sandbox.Handle // non-nil when a planning invocation is active
	active bool           // true after Start, false after Stop
}

// New creates a Planner from the given configuration.
func New(cfg Config) *Planner {
	return &Planner{
		backend:          cfg.Backend,
		command:          cfg.Command,
		image:            cfg.Image,
		workspaces:       cfg.Workspaces,
		envFile:          cfg.EnvFile,
		fingerprint:      cfg.Fingerprint,
		instructionsPath: cfg.InstructionsPath,
		network:          cfg.Network,
		cpus:             cfg.CPUs,
		memory:           cfg.Memory,
	}
}

// Start marks the planner as active. The actual container is created lazily
// on the first Exec call via the backend's worker container mechanism.
func (p *Planner) Start(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = true
	return nil
}

// Stop stops the planning container and marks the planner as inactive.
func (p *Planner) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.handle != nil {
		_ = p.handle.Kill()
		p.handle = nil
	}
	// If the backend supports worker management, stop the planning worker.
	if wm, ok := p.backend.(sandbox.WorkerManager); ok {
		wm.StopTaskWorker(planningTaskID)
	}
	p.active = false
}

// IsRunning reports whether the planner has been started and not stopped.
func (p *Planner) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// Exec launches a command inside the planning container via the sandbox
// backend. The backend's worker container mechanism (when available)
// reuses the same container across calls using the stable planningTaskID.
func (p *Planner) Exec(ctx context.Context, cmd []string) (sandbox.Handle, error) {
	p.mu.Lock()
	if !p.active {
		p.mu.Unlock()
		return nil, fmt.Errorf("planner: not started")
	}
	if p.backend == nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("planner: no backend configured")
	}
	p.mu.Unlock()

	containerName := "wallfacer-plan-" + truncFingerprint(p.fingerprint)
	spec := p.buildContainerSpec(containerName, sandbox.Claude)
	spec.Cmd = cmd

	h, err := p.backend.Launch(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("planner exec: %w", err)
	}

	p.mu.Lock()
	p.handle = h
	p.mu.Unlock()

	return h, nil
}

// UpdateWorkspaces destroys the current planning container (if any) and
// stores new workspace configuration. A subsequent Start+Exec will create
// a container with the updated mounts.
func (p *Planner) UpdateWorkspaces(workspaces []string, fingerprint string) {
	p.Stop()

	p.mu.Lock()
	defer p.mu.Unlock()
	p.workspaces = workspaces
	p.fingerprint = fingerprint
}

// truncFingerprint returns the first 12 characters of a fingerprint string,
// or the full string if shorter.
func truncFingerprint(fp string) string {
	if len(fp) > 12 {
		return fp[:12]
	}
	return fp
}
