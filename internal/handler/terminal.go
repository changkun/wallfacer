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
	id       string
	ptmx     *os.File
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	done     chan struct{} // closed when the shell process exits
	outputCh chan []byte   // PTY output pumped by a reader goroutine
}

// sessionRegistry manages multiple terminal sessions per WebSocket connection.
type sessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
	active   string          // ID of the currently active session
	switchCh chan struct{}   // signaled when active session changes
	connCtx  context.Context // connection-level context
}

// create spawns a new PTY-backed shell session and registers it as the active session.
func (r *sessionRegistry) create(shell, cwd string, cols, rows int) (string, error) {
	id := uuid.New().String()
	ctx, cancel := context.WithCancel(r.connCtx)

	cmd := exec.CommandContext(ctx, shell)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	ptmx, err := pty.StartWithSize(cmd, uint16(rows), uint16(cols))
	if err != nil {
		cancel()
		return "", err
	}

	outputCh := make(chan []byte, 64)
	sess := &terminalSession{
		id:       id,
		ptmx:     ptmx,
		cmd:      cmd,
		cancel:   cancel,
		done:     make(chan struct{}),
		outputCh: outputCh,
	}

	// Reader goroutine: pump PTY output into the channel.
	// Runs for the lifetime of the session. Closes outputCh when PTY closes.
	go func() {
		defer close(outputCh)
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				select {
				case outputCh <- data:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	r.mu.Lock()
	r.sessions[id] = sess
	r.active = id
	r.mu.Unlock()

	// Signal the relay to pick up the new active session.
	r.notifySwitch()

	return id, nil
}

// switchTo changes the active session and signals the relay dispatcher.
func (r *sessionRegistry) switchTo(id string) bool {
	r.mu.Lock()
	_, ok := r.sessions[id]
	if !ok {
		r.mu.Unlock()
		return false
	}
	r.active = id
	r.mu.Unlock()
	r.notifySwitch()
	return true
}

// notifySwitch sends a non-blocking signal on switchCh.
func (r *sessionRegistry) notifySwitch() {
	select {
	case r.switchCh <- struct{}{}:
	default:
	}
}

// monitorSession waits for a session's shell to exit and handles cleanup.
func (r *sessionRegistry) monitorSession(sess *terminalSession, connCancel context.CancelFunc, conn *websocket.Conn) {
	_ = sess.cmd.Wait()
	close(sess.done)
	r.handleSessionExit(sess.id, connCancel, conn)
}

// handleSessionExit removes an exited session and manages the active session fallback.
// It sends a session_exited message to the client. If the exited session was active
// and other sessions remain, it switches to one of them. If no sessions remain,
// it cancels the connection context to close the WebSocket.
func (r *sessionRegistry) handleSessionExit(id string, connCancel context.CancelFunc, conn *websocket.Conn) {
	r.mu.Lock()
	sess, ok := r.sessions[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.sessions, id)
	wasActive := r.active == id
	if wasActive {
		r.active = ""
	}

	// Find a fallback session if the active one exited.
	var fallbackID string
	if wasActive {
		for fid := range r.sessions {
			fallbackID = fid
			break
		}
	}
	remaining := len(r.sessions)
	r.mu.Unlock()

	sess.cleanup()

	// Notify the client.
	msg := `{"type":"session_exited","session":"` + id + `"}`
	_ = conn.Write(r.connCtx, websocket.MessageText, []byte(msg))

	if wasActive && remaining > 0 {
		r.switchTo(fallbackID)
	} else if remaining == 0 {
		connCancel()
		_ = conn.Close(websocket.StatusNormalClosure, "shell exited")
	}
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

	registry := &sessionRegistry{
		sessions: make(map[string]*terminalSession),
		switchCh: make(chan struct{}, 1),
		connCtx:  connCtx,
	}

	sessionID, err := registry.create(shell, cwd, cols, rows)
	if err != nil {
		logger.Handler.Error("terminal: pty start failed", "error", err)
		_ = conn.Close(websocket.StatusInternalError, "failed to start shell")
		return
	}

	// Start process monitor for the initial session.
	sess, _ := registry.get(sessionID)
	go registry.monitorSession(sess, connCancel, conn)

	// Send initial sessions list to the client.
	registry.sendSessionsList(conn)

	var wg sync.WaitGroup
	wg.Add(2)

	// PTY → WebSocket: relay output from the active session.
	// Each session gets a reader goroutine that pumps PTY data into a channel.
	// The relay goroutine selects on the active session's channel plus switchCh.
	go func() {
		defer wg.Done()
		var activeCh <-chan []byte
		var activeID string

		resolveActive := func() {
			sess, ok := registry.activeSession()
			if !ok {
				activeCh = nil
				activeID = ""
				return
			}
			if sess.id == activeID {
				return // already reading from this session
			}
			activeID = sess.id
			activeCh = sess.outputCh
		}

		resolveActive()
		for {
			if activeCh == nil {
				select {
				case <-registry.switchCh:
					resolveActive()
					continue
				case <-connCtx.Done():
					return
				}
			}

			select {
			case data, ok := <-activeCh:
				if !ok {
					// Session's PTY closed — re-resolve.
					activeCh = nil
					activeID = ""
					resolveActive()
					continue
				}
				if writeErr := conn.Write(connCtx, websocket.MessageBinary, data); writeErr != nil {
					return
				}
			case <-registry.switchCh:
				resolveActive()
			case <-connCtx.Done():
				return
			}
		}
	}()

	// WebSocket → PTY: relay client input to the active session.
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
				if active, ok := registry.activeSession(); ok {
					_, _ = active.ptmx.Write(decoded)
				}
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					if active, ok := registry.activeSession(); ok {
						_ = pty.Setsize(active.ptmx, uint16(msg.Rows), uint16(msg.Cols))
					}
				}
			case "ping":
				_ = conn.Write(connCtx, websocket.MessageText, []byte(`{"type":"pong"}`))
			case "create_session":
				newID, err := registry.create(shell, cwd, cols, rows)
				if err != nil {
					_ = conn.Write(connCtx, websocket.MessageText, []byte(`{"type":"error","data":"failed to create session"}`))
					continue
				}
				newSess, _ := registry.get(newID)
				go registry.monitorSession(newSess, connCancel, conn)
				_ = conn.Write(connCtx, websocket.MessageText, []byte(`{"type":"session_created","session":"`+newID+`"}`))
				registry.sendSessionsList(conn)
			case "switch_session":
				if !registry.switchTo(msg.Session) {
					_ = conn.Write(connCtx, websocket.MessageText, []byte(`{"type":"error","data":"session not found: `+msg.Session+`"}`))
					continue
				}
				_ = conn.Write(connCtx, websocket.MessageText, []byte(`{"type":"session_switched","session":"`+msg.Session+`"}`))
				registry.sendSessionsList(conn)
			case "close_session":
				if _, ok := registry.get(msg.Session); !ok {
					_ = conn.Write(connCtx, websocket.MessageText, []byte(`{"type":"error","data":"session not found: `+msg.Session+`"}`))
					continue
				}
				registry.remove(msg.Session)
				_ = conn.Write(connCtx, websocket.MessageText, []byte(`{"type":"session_closed","session":"`+msg.Session+`"}`))

				// If the closed session was active, pick a fallback.
				if _, ok := registry.activeSession(); !ok {
					// No active session — try to find one.
					registry.mu.Lock()
					var fallback string
					for fid := range registry.sessions {
						fallback = fid
						break
					}
					remaining := len(registry.sessions)
					registry.mu.Unlock()

					if remaining > 0 {
						registry.switchTo(fallback)
					} else {
						connCancel()
						_ = conn.Close(websocket.StatusNormalClosure, "all sessions closed")
						return
					}
				}
				registry.sendSessionsList(conn)
			}
		}
	}()

	wg.Wait()
	registry.closeAll()
}

// terminalMessage is the JSON envelope for client→server messages.
type terminalMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
	Session string `json:"session,omitempty"`
}

// sessionInfo describes a session in the sessions list message.
type sessionInfo struct {
	ID     string `json:"id"`
	Active bool   `json:"active"`
}

// sendSessionsList sends the current sessions list to the client.
func (r *sessionRegistry) sendSessionsList(conn *websocket.Conn) {
	r.mu.Lock()
	infos := make([]sessionInfo, 0, len(r.sessions))
	for id := range r.sessions {
		infos = append(infos, sessionInfo{ID: id, Active: id == r.active})
	}
	r.mu.Unlock()

	payload, err := json.Marshal(struct {
		Type     string        `json:"type"`
		Sessions []sessionInfo `json:"sessions"`
	}{Type: "sessions", Sessions: infos})
	if err != nil {
		return
	}
	_ = conn.Write(r.connCtx, websocket.MessageText, payload)
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
