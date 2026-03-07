package runner

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// ContainerInfo represents a single sandbox container returned by ListContainers.
type ContainerInfo struct {
	ID        string `json:"id"`         // short container ID
	Name      string `json:"name"`       // full container name (e.g. wallfacer-<slug>-<uuid8>)
	TaskID    string `json:"task_id"`    // task UUID from label, empty if not a task container
	TaskTitle string `json:"task_title"` // task title populated by the handler from the store
	Image     string `json:"image"`      // image name
	State     string `json:"state"`      // running | exited | paused | …
	Status    string `json:"status"`     // human-readable status (e.g. "Up 5 minutes")
	CreatedAt int64  `json:"created_at"` // unix timestamp
}

// containerJSON is used to unmarshal `podman/docker ps --format json` output.
// Podman and Docker use different JSON formats:
//   - Podman outputs a JSON array; Docker outputs one JSON object per line (NDJSON).
//   - Podman's "Names" is []string; Docker's "Names" is a single string.
//   - Podman's "Created" is int64 (unix timestamp); Docker's "CreatedAt" is a string.
//
// We use json.RawMessage for Names and any for Created to handle both.
type containerJSON struct {
	ID        string            `json:"Id"`
	Names     json.RawMessage   `json:"Names"`
	Image     string            `json:"Image"`
	State     string            `json:"State"`
	Status    string            `json:"Status"`
	Created   any               `json:"Created"`
	CreatedAt string            `json:"CreatedAt"`          // Docker uses CreatedAt (string) instead of Created
	Labels    map[string]string `json:"Labels"`             // task metadata labels (wallfacer.task.id, etc.)
}

// name extracts the container name from the Names field, handling both
// Podman ([]string) and Docker (string) formats.
func (c *containerJSON) name() string {
	if c.Names == nil {
		return ""
	}
	// Try []string first (Podman format).
	var names []string
	if json.Unmarshal(c.Names, &names) == nil && len(names) > 0 {
		return strings.TrimPrefix(names[0], "/")
	}
	// Try single string (Docker format).
	var name string
	if json.Unmarshal(c.Names, &name) == nil {
		return strings.TrimPrefix(name, "/")
	}
	return ""
}

// createdUnix returns the creation time as a unix timestamp.
// Podman provides Created as a numeric unix timestamp; Docker provides it
// as a float or string. We handle both gracefully.
func (c *containerJSON) createdUnix() int64 {
	// Podman: Created is a JSON number (int64 or float64).
	if c.Created != nil {
		switch v := c.Created.(type) {
		case float64:
			return int64(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return n
			}
		}
	}
	return 0
}

// parseContainerList parses the JSON output of `ps --format json`, handling
// both Podman (JSON array) and Docker (NDJSON, one object per line) formats.
func parseContainerList(out []byte) ([]containerJSON, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	// Podman: JSON array.
	if trimmed[0] == '[' {
		var containers []containerJSON
		if err := json.Unmarshal(out, &containers); err != nil {
			return nil, fmt.Errorf("parse container list (array): %w", err)
		}
		return containers, nil
	}

	// Docker: NDJSON (one JSON object per line).
	var containers []containerJSON
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var c containerJSON
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("parse container list (ndjson line): %w", err)
		}
		containers = append(containers, c)
	}
	return containers, nil
}

// ListContainers runs `<runtime> ps -a --filter name=wallfacer --format json`
// and returns structured info for each matching container.
// Supports both Podman and Docker JSON output formats.
func (r *Runner) ListContainers() ([]ContainerInfo, error) {
	out, err := exec.Command(r.command, "ps", "-a",
		"--filter", "name=wallfacer",
		"--format", "json",
	).Output()
	if err != nil {
		return nil, err
	}

	raw, err := parseContainerList(out)
	if err != nil {
		return nil, err
	}

	result := make([]ContainerInfo, 0, len(raw))
	for _, c := range raw {
		name := c.name()

		// Primary: extract task UUID from the wallfacer.task.id label.
		// This works regardless of the container name format.
		taskID := ""
		if c.Labels != nil {
			taskID = c.Labels["wallfacer.task.id"]
		}
		// Fallback for containers created without labels (old format wallfacer-<uuid>):
		// try stripping the "wallfacer-" prefix and check if the remainder is a UUID.
		if taskID == "" {
			candidate := strings.TrimPrefix(name, "wallfacer-")
			if candidate != name && isUUID(candidate) {
				taskID = candidate
			}
		}

		result = append(result, ContainerInfo{
			ID:        c.ID,
			Name:      name,
			TaskID:    taskID,
			Image:     c.Image,
			State:     c.State,
			Status:    c.Status,
			CreatedAt: c.createdUnix(),
		})
	}
	return result, nil
}

// isUUID returns true if s looks like a standard UUID (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ContainerName returns the active container name for a task.
// It first checks the in-memory map populated when a container is launched,
// then falls back to scanning the container list and matching by task ID label.
// Returns an empty string if no container is found.
func (r *Runner) ContainerName(taskID uuid.UUID) string {
	if v, ok := r.containerNames.Load(taskID.String()); ok {
		return v.(string)
	}
	// Fallback: search all wallfacer containers by label.
	containers, err := r.ListContainers()
	if err != nil {
		return ""
	}
	for _, c := range containers {
		if c.TaskID == taskID.String() {
			return c.Name
		}
	}
	return ""
}

const (
	maxRebaseRetries   = 3
	defaultTaskTimeout = 60 * time.Minute
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

// Runner orchestrates agent container execution for tasks.
// It manages worktree isolation, container lifecycle, and the commit pipeline.
type Runner struct {
	store               *store.Store
	command             string
	sandboxImage        string
	envFile             string
	workspaces          string
	worktreesDir        string
	instructionsPath    string
	repoMu              sync.Map       // per-repo *sync.Mutex for serializing rebase+merge
	containerNames      sync.Map       // taskID (string) → container name (string)
	refineContainerNames sync.Map      // taskID (string) → refinement container name (string)
	backgroundWg        sync.WaitGroup // tracks fire-and-forget background goroutines
}

// WaitBackground blocks until all fire-and-forget background goroutines
// (RunBackground, oversight generation, etc.) have completed. Intended for
// use in tests to avoid cleanup races with goroutines that write to
// temporary directories.
func (r *Runner) WaitBackground() {
	r.backgroundWg.Wait()
}

// RunBackground launches Run in a background goroutine tracked by backgroundWg.
// Callers (handlers, autopilot) should use this instead of a bare "go r.Run(...)"
// so that WaitBackground can drain all outstanding work — particularly useful
// in tests to prevent cleanup races with temp-dir removal.
func (r *Runner) RunBackground(taskID uuid.UUID, prompt, sessionID string, resumedFromWaiting bool) {
	r.backgroundWg.Add(1)
	go func() {
		defer r.backgroundWg.Done()
		r.Run(taskID, prompt, sessionID, resumedFromWaiting)
	}()
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

// EnvFile returns the path to the env file used for containers.
func (r *Runner) EnvFile() string {
	return r.envFile
}

// Workspaces returns the list of configured workspace paths.
func (r *Runner) Workspaces() []string {
	if r.workspaces == "" {
		return nil
	}
	return strings.Fields(r.workspaces)
}

// repoLock returns a per-repo mutex, creating one on first access.
// Used to serialize rebase+merge operations on the same repository.
func (r *Runner) repoLock(repoPath string) *sync.Mutex {
	v, _ := r.repoMu.LoadOrStore(repoPath, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// RefineContainerName returns the active refinement container name for a task.
// Returns an empty string if no refinement container is running.
func (r *Runner) RefineContainerName(taskID uuid.UUID) string {
	if v, ok := r.refineContainerNames.Load(taskID.String()); ok {
		return v.(string)
	}
	return ""
}

// KillContainer sends a kill signal to the running container for a task.
// Safe to call when no container is running — errors are silently ignored.
func (r *Runner) KillContainer(taskID uuid.UUID) {
	name := r.ContainerName(taskID)
	if name == "" {
		return
	}
	exec.Command(r.command, "kill", name).Run()
}

// KillRefineContainer sends a kill signal to the running refinement container.
// Safe to call when no refinement container is running — errors are silently ignored.
func (r *Runner) KillRefineContainer(taskID uuid.UUID) {
	name := r.RefineContainerName(taskID)
	if name == "" {
		return
	}
	exec.Command(r.command, "kill", name).Run()
}
