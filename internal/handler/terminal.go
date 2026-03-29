//go:build !windows

package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/pty"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// terminalSession holds the state for a single PTY-backed shell session.
type terminalSession struct {
	id     string
	ptmx   *os.File
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{} // closed when the shell process exits
}

// sessionRegistry manages multiple terminal sessions per WebSocket connection.
type sessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
	active   string // ID of the currently active session
}

// create spawns a new PTY-backed shell session and registers it as the active session.
func (r *sessionRegistry) create(connCtx context.Context, shell, cwd string, cols, rows int) (string, error) {
	id := uuid.New().String()
	ctx, cancel := context.WithCancel(connCtx)

	cmd := exec.CommandContext(ctx, shell)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	ptmx, err := pty.StartWithSize(cmd, uint16(rows), uint16(cols))
	if err != nil {
		cancel()
		return "", err
	}

	sess := &terminalSession{
		id:     id,
		ptmx:   ptmx,
		cmd:    cmd,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	r.mu.Lock()
	r.sessions[id] = sess
	r.active = id
	r.mu.Unlock()

	return id, nil
}

// get retrieves a session by ID.
func (r *sessionRegistry) get(id string) (*terminalSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.sessions[id]
	return sess, ok
}

// remove cleans up a session and removes it from the registry.
func (r *sessionRegistry) remove(id string) {
	r.mu.Lock()
	sess, ok := r.sessions[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.sessions, id)
	if r.active == id {
		r.active = ""
	}
	r.mu.Unlock()

	sess.cleanup()
}

// closeAll cleans up all sessions.
func (r *sessionRegistry) closeAll() {
	r.mu.Lock()
	all := make([]*terminalSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		all = append(all, s)
	}
	r.sessions = make(map[string]*terminalSession)
	r.active = ""
	r.mu.Unlock()

	for _, s := range all {
		s.cleanup()
	}
}

// activeSession returns the currently active session.
func (r *sessionRegistry) activeSession() (*terminalSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.sessions[r.active]
	return sess, ok
}

// HandleTerminalWS upgrades to a WebSocket connection and relays I/O
// between the client and a host shell via a PTY.
func (h *Handler) HandleTerminalWS(w http.ResponseWriter, r *http.Request) {
	// Gate on WALLFACER_TERMINAL_ENABLED (defaults to true for local use).
	if h.envFile != "" {
		cfg, err := envconfig.Parse(h.envFile)
		if err == nil && !cfg.TerminalEnabled {
			httpjson.Write(w, http.StatusForbidden, map[string]string{"error": "terminal disabled"})
			return
		}
	}

	cols := parseIntParam(r, "cols", 80)
	rows := parseIntParam(r, "rows", 24)
	cwd := r.URL.Query().Get("cwd")
	cwd = h.resolveTerminalCwd(cwd)

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Origin check not needed; same-host dev tool.
	})
	if err != nil {
		logger.Handler.Error("terminal: websocket accept failed", "error", err)
		return
	}

	connCtx, connCancel := context.WithCancel(r.Context())
	defer connCancel()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
		if _, err := os.Stat(shell); err != nil {
			shell = "/bin/sh"
		}
	}

	registry := &sessionRegistry{sessions: make(map[string]*terminalSession)}

	sessionID, err := registry.create(connCtx, shell, cwd, cols, rows)
	if err != nil {
		logger.Handler.Error("terminal: pty start failed", "error", err)
		_ = conn.Close(websocket.StatusInternalError, "failed to start shell")
		return
	}

	sess, _ := registry.get(sessionID)

	var wg sync.WaitGroup
	wg.Add(2)

	// Monitor shell exit.
	go func() {
		_ = sess.cmd.Wait()
		close(sess.done)
		connCancel()
		_ = conn.Close(websocket.StatusNormalClosure, "shell exited")
	}()

	// PTY → WebSocket: relay shell output to the client.
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.ptmx.Read(buf)
			if n > 0 {
				if writeErr := conn.Write(connCtx, websocket.MessageBinary, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → PTY: relay client input to the shell.
	go func() {
		defer wg.Done()
		for {
			typ, data, err := conn.Read(connCtx)
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var msg terminalMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "input":
				decoded, err := base64.StdEncoding.DecodeString(msg.Data)
				if err != nil {
					continue
				}
				_, _ = sess.ptmx.Write(decoded)
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					_ = pty.Setsize(sess.ptmx, uint16(msg.Rows), uint16(msg.Cols))
				}
			case "ping":
				_ = conn.Write(connCtx, websocket.MessageText, []byte(`{"type":"pong"}`))
			}
		}
	}()

	wg.Wait()
	registry.closeAll()
}

// terminalMessage is the JSON envelope for client→server messages.
type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// cleanup sends SIGHUP to the shell process group (graceful hangup signal),
// waits up to 2 seconds for the process to exit, then sends SIGKILL if
// still alive. The negative PID targets the entire process group so child
// processes spawned by the shell are also terminated.
func (s *terminalSession) cleanup() {
	s.cancel()
	if s.cmd.Process != nil {
		// SIGHUP to process group.
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGHUP)
		done := make(chan struct{})
		go func() {
			_, _ = s.cmd.Process.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	_ = s.ptmx.Close()
}

// resolveTerminalCwd validates the requested cwd against active workspaces.
// Falls back to the first workspace, or os.TempDir() if none configured.
func (h *Handler) resolveTerminalCwd(cwd string) string {
	workspaces := h.currentWorkspaces()

	if cwd != "" {
		abs, err := filepath.Abs(cwd)
		if err == nil {
			// Accept if it's within any active workspace.
			for _, ws := range workspaces {
				if abs == ws || hasPrefix(abs, ws+string(filepath.Separator)) {
					if info, err := os.Stat(abs); err == nil && info.IsDir() {
						return abs
					}
				}
			}
		}
	}

	if len(workspaces) > 0 {
		return workspaces[0]
	}
	return os.TempDir()
}

// hasPrefix is a simple string prefix check. It avoids importing strings
// for a single call site in resolveTerminalCwd.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// parseIntParam reads an integer query parameter with a default fallback.
func parseIntParam(r *http.Request, name string, fallback int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
