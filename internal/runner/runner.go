package runner

import (
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// ContainerInfo represents a single sandbox container returned by ListContainers.
type ContainerInfo struct {
	ID        string `json:"id"`       // short container ID
	Name      string `json:"name"`     // full container name (e.g. wallfacer-<uuid>)
	TaskID    string `json:"task_id"`  // task UUID extracted from name, empty if not a task container
	Image     string `json:"image"`    // image name
	State     string `json:"state"`    // running | exited | paused | …
	Status    string `json:"status"`   // human-readable status (e.g. "Up 5 minutes")
	CreatedAt int64  `json:"created_at"` // unix timestamp
}

// podmanContainer is used to unmarshal `podman/docker ps --format json` output.
type podmanContainer struct {
	ID      string   `json:"Id"`
	Names   []string `json:"Names"`
	Image   string   `json:"Image"`
	State   string   `json:"State"`
	Status  string   `json:"Status"`
	Created int64    `json:"Created"`
}

// ListContainers runs `<runtime> ps -a --filter name=wallfacer --format json`
// and returns structured info for each matching container.
func (r *Runner) ListContainers() ([]ContainerInfo, error) {
	out, err := exec.Command(r.command, "ps", "-a",
		"--filter", "name=wallfacer",
		"--format", "json",
	).Output()
	if err != nil {
		return nil, err
	}

	var raw []podmanContainer
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}

	result := make([]ContainerInfo, 0, len(raw))
	for _, c := range raw {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/") // docker prefixes with "/"
		}
		taskID := strings.TrimPrefix(name, "wallfacer-")
		if taskID == name {
			taskID = "" // name didn't have the prefix → not a task container
		}
		result = append(result, ContainerInfo{
			ID:        c.ID,
			Name:      name,
			TaskID:    taskID,
			Image:     c.Image,
			State:     c.State,
			Status:    c.Status,
			CreatedAt: c.Created,
		})
	}
	return result, nil
}

const (
	maxRebaseRetries   = 3
	defaultTaskTimeout = 15 * time.Minute
)

// RunnerConfig holds all configuration needed to construct a Runner.
type RunnerConfig struct {
	Command          string
	SandboxImage     string
	EnvFile          string
	Workspaces       string // space-separated workspace paths
	WorktreesDir     string
	InstructionsPath string
}

// Runner orchestrates Claude Code container execution for tasks.
// It manages worktree isolation, container lifecycle, and the commit pipeline.
type Runner struct {
	store            *store.Store
	command          string
	sandboxImage     string
	envFile          string
	workspaces       string
	worktreesDir     string
	instructionsPath string
}

// NewRunner constructs a Runner from the given store and config.
func NewRunner(s *store.Store, cfg RunnerConfig) *Runner {
	return &Runner{
		store:            s,
		command:          cfg.Command,
		sandboxImage:     cfg.SandboxImage,
		envFile:          cfg.EnvFile,
		workspaces:       cfg.Workspaces,
		worktreesDir:     cfg.WorktreesDir,
		instructionsPath: cfg.InstructionsPath,
	}
}

// Command returns the container runtime binary path (podman/docker).
func (r *Runner) Command() string {
	return r.command
}

// Workspaces returns the list of configured workspace paths.
func (r *Runner) Workspaces() []string {
	if r.workspaces == "" {
		return nil
	}
	return strings.Fields(r.workspaces)
}

// KillContainer sends a kill signal to the running container for a task.
// Safe to call when no container is running — errors are silently ignored.
func (r *Runner) KillContainer(taskID uuid.UUID) {
	containerName := "wallfacer-" + taskID.String()
	exec.Command(r.command, "kill", containerName).Run()
}
