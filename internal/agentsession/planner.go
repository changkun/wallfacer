// Package agentsession manages the agent session lifecycle. The runtime runs
// the chat agent as a host process scoped to the workspace, letting it
// read the full workspace and write to specs/. It delegates launches to
// an [executor.Backend] so the same code serves the host backend today
// and cloud backends later.
package agentsession

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/pkg/livelog"
)

// planningTaskID is a fixed synthetic task ID stamped onto every planning
// launch via the "wallfacer.task.id" label, so the process monitor and
// usage attribution can tell planning runs apart from task runs.
const planningTaskID = "planning-sandbox"

// Config holds the configuration for a Planner.
type Config struct {
	Backend     executor.Backend // execution backend (host; cloud later)
	Command     string           // legacy runtime binary path; unused on the host backend
	Workspaces  []string         // workspace directory paths
	EnvFile     string           // path to .env file for the agent process
	Fingerprint string           // workspace fingerprint for keying the planning workspace
	ConfigDir   string           // base config directory (~/.wallfacer/) for conversation persistence
}

// Planner manages the singleton planning agent process for a workspace.
type Planner struct {
	mu          sync.Mutex
	backend     executor.Backend
	command     string
	workspaces  []string
	envFile     string
	fingerprint string

	handle       executor.Handle // non-nil when a planning invocation is active
	active       bool            // true after Start, false after Stop
	busy         bool            // true while a chat exec is in flight
	busyThreadID string          // thread ID of the in-flight exec (empty when !busy)
	liveLog      *livelog.Log    // live output buffer for the current exec (nil when idle)
	threads      *ThreadManager  // multi-thread chat persistence (nil if configDir empty)

	configDir string // root config directory; kept so UpdateWorkspaces can open a new ThreadManager
}

// New creates a Planner from the given configuration. If ConfigDir and
// Fingerprint are set, a [ThreadManager] is created for multi-thread
// chat persistence; on first load, any legacy single-thread layout is
// migrated to "Chat 1".
func New(cfg Config) *Planner {
	p := &Planner{
		backend:     cfg.Backend,
		command:     cfg.Command,
		workspaces:  cfg.Workspaces,
		envFile:     cfg.EnvFile,
		fingerprint: cfg.Fingerprint,
		configDir:   cfg.ConfigDir,
	}
	if cfg.ConfigDir != "" && cfg.Fingerprint != "" {
		tm, err := NewThreadManager(filepath.Join(cfg.ConfigDir, "planning", cfg.Fingerprint))
		if err == nil {
			p.threads = tm
		}
	}
	return p
}

// Threads returns the multi-thread chat manager, or nil if thread
// persistence is not configured.
func (p *Planner) Threads() *ThreadManager {
	return p.threads
}

// ActiveConversation returns the [ConversationStore] for the currently
// active thread, or nil when no thread exists or thread storage is not
// configured. Handlers should prefer looking up a store by explicit
// thread ID via Threads().Store(id).
func (p *Planner) ActiveConversation() *ConversationStore {
	if p.threads == nil {
		return nil
	}
	id := p.threads.ActiveID()
	if id == "" {
		return nil
	}
	s, err := p.threads.Store(id)
	if err != nil {
		return nil
	}
	return s
}

// Start marks the planner as active. The agent process is spawned lazily
// on the first Exec call.
func (p *Planner) Start(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = true
	return nil
}

// Stop kills the planning agent process and marks the planner as inactive.
func (p *Planner) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.handle != nil {
		_ = p.handle.Kill()
		p.handle = nil
	}
	p.active = false
}

// IsRunning reports whether the planner has been started and not stopped.
func (p *Planner) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// Exec launches a command as a planning agent process via the execution
// backend. Each call spawns a fresh process tagged with the stable
// planningTaskID for monitor and usage attribution.
func (p *Planner) Exec(ctx context.Context, cmd []string) (executor.Handle, error) {
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

	name := "wallfacer-plan-" + truncFingerprint(p.fingerprint)
	spec := p.buildSpec(name, harness.Claude)
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

// IsTaskLocked reports whether any task-mode thread currently has an
// in-flight turn pinned to taskID. Returns (true, threadID) when locked,
// (false, "") otherwise.
func (p *Planner) IsTaskLocked(taskID string) (bool, string) {
	p.mu.Lock()
	busy := p.busy
	threadID := p.busyThreadID
	threads := p.threads
	p.mu.Unlock()

	if !busy || threadID == "" || threads == nil {
		return false, ""
	}
	cs, err := threads.Store(threadID)
	if err != nil {
		return false, ""
	}
	sess, err := cs.LoadSession()
	if err != nil || sess.FocusedTask != taskID {
		return false, ""
	}
	return true, threadID
}

// IsBusy reports whether a chat exec is currently in flight.
func (p *Planner) IsBusy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.busy
}

// BusyThreadID returns the thread ID of the in-flight exec, or the empty
// string if nothing is running.
func (p *Planner) BusyThreadID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.busy {
		return ""
	}
	return p.busyThreadID
}

// SetBusy marks the planner as busy (exec in flight) and records the
// thread ID that owns the exec. Pass an empty threadID when clearing.
func (p *Planner) SetBusy(b bool, threadID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.busy = b
	if b {
		p.busyThreadID = threadID
	} else {
		p.busyThreadID = ""
	}
}

// StartLiveLog creates a new live log buffer for the current exec.
// Returns the log so the caller can tee stdout into it.
func (p *Planner) StartLiveLog() *livelog.Log {
	l := livelog.New()
	p.mu.Lock()
	prev := p.liveLog
	p.liveLog = l
	p.mu.Unlock()
	// Seal any previous live log so its readers (e.g. SSE consumers of the
	// prior turn) receive io.EOF instead of hanging until the client
	// disconnects. The stale-session retry path starts a second live log
	// without closing the first.
	if prev != nil {
		prev.Close()
	}
	return l
}

// CloseLiveLog closes and removes the current live log.
func (p *Planner) CloseLiveLog() {
	p.mu.Lock()
	l := p.liveLog
	p.liveLog = nil
	p.mu.Unlock()
	if l != nil {
		l.Close()
	}
}

// LogReader returns a reader for the current exec's live log, scoped to
// the thread that owns the exec. Pass an empty threadID to read
// regardless of thread (used by callers that don't yet track threads).
// Returns nil if no exec is in flight, or if threadID is non-empty and
// does not match the thread that owns the exec.
func (p *Planner) LogReader(threadID string) *livelog.Reader {
	p.mu.Lock()
	l := p.liveLog
	owner := p.busyThreadID
	p.mu.Unlock()
	if l == nil {
		return nil
	}
	if threadID != "" && owner != "" && threadID != owner {
		return nil
	}
	return l.NewReader()
}

// Interrupt kills the current exec handle and clears the busy flag,
// but does NOT clear the session ID so --resume still works on the
// next message. Also closes the live log so SSE consumers see EOF.
func (p *Planner) Interrupt() error {
	p.mu.Lock()
	if !p.busy {
		p.mu.Unlock()
		return fmt.Errorf("planner: not busy")
	}
	h := p.handle
	l := p.liveLog
	p.busy = false
	p.liveLog = nil
	p.mu.Unlock()

	if h != nil {
		_ = h.Kill()
	}
	if l != nil {
		l.Close()
	}
	return nil
}

// UpdateWorkspaces stops the current planning agent process (if any),
// stores new workspace configuration, and re-opens the thread manager
// rooted at the new fingerprint's planning directory so thread CRUD,
// messages, and undo target the right workspace group after a switch.
// A subsequent Start+Exec spawns a fresh process in the updated workspace.
func (p *Planner) UpdateWorkspaces(workspaces []string, fingerprint string) {
	p.Stop()

	p.mu.Lock()
	defer p.mu.Unlock()
	p.workspaces = workspaces
	p.fingerprint = fingerprint
	if p.configDir != "" && fingerprint != "" {
		tm, err := NewThreadManager(filepath.Join(p.configDir, "planning", fingerprint))
		if err == nil {
			p.threads = tm
		}
	}
}

// truncFingerprint returns the first 12 characters of a fingerprint string,
// or the full string if shorter.
func truncFingerprint(fp string) string {
	if len(fp) > 12 {
		return fp[:12]
	}
	return fp
}
