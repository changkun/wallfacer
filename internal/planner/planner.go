// Package planner manages the planning sandbox container lifecycle.
// The planning sandbox is a long-lived workspace-scoped container that
// lets the chat agent read the full workspace and write to specs/.
// It uses the worker container pattern (create once, exec per round).
package planner

import (
	"context"
	"fmt"
	"sync"

	"changkun.de/x/wallfacer/internal/sandbox"
)

// Config holds the configuration for a Planner.
type Config struct {
	Command          string   // container runtime binary path (podman/docker)
	Image            string   // sandbox container image name
	Workspaces       []string // workspace directory paths
	EnvFile          string   // path to .env file for container
	Fingerprint      string   // workspace fingerprint for keying the container
	InstructionsPath string   // path to AGENTS.md / CLAUDE.md instructions file
	Network          string   // container network (empty defaults to "host")
	CPUs             string   // container CPU limit (e.g. "2.0")
	Memory           string   // container memory limit (e.g. "4g")
}

// Planner manages a singleton long-lived planning container for a workspace.
type Planner struct {
	mu               sync.Mutex
	command          string
	image            string
	workspaces       []string
	envFile          string
	fingerprint      string
	instructionsPath string
	network          string
	cpus             string
	memory           string

	worker *planningWorker // nil when no planning session is active
}

// New creates a Planner from the given configuration.
func New(cfg Config) *Planner {
	return &Planner{
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

// Start creates and starts the planning container if it is not already running.
func (p *Planner) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.worker != nil && p.worker.isAlive() {
		return nil
	}

	containerName := "wallfacer-plan-" + truncFingerprint(p.fingerprint)
	spec := p.buildContainerSpec(containerName, sandbox.Claude)

	w := newPlanningWorker(p.command, containerName, spec.BuildCreate(), spec.Entrypoint)
	if err := w.ensureRunning(ctx); err != nil {
		return fmt.Errorf("planner start: %w", err)
	}

	p.worker = w
	return nil
}

// Stop stops and removes the planning container.
func (p *Planner) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.worker != nil {
		p.worker.stop()
		p.worker = nil
	}
}

// IsRunning reports whether the planning container is alive.
func (p *Planner) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.worker != nil && p.worker.isAlive()
}

// Exec runs a command inside the planning container via podman/docker exec.
// Start must be called before Exec.
func (p *Planner) Exec(ctx context.Context, cmd []string) (sandbox.Handle, error) {
	p.mu.Lock()
	w := p.worker
	p.mu.Unlock()

	if w == nil {
		return nil, fmt.Errorf("planner: not started")
	}

	spec := p.buildContainerSpec(w.containerName, sandbox.Claude)
	return w.exec(ctx, cmd, spec.WorkDir)
}

// UpdateWorkspaces destroys the current planning container (if any) and
// stores new workspace configuration. A subsequent Start call will create
// a container with the updated mounts.
func (p *Planner) UpdateWorkspaces(workspaces []string, fingerprint string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.worker != nil {
		p.worker.stop()
		p.worker = nil
	}
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
