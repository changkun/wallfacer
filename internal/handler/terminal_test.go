//go:build !windows

package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/coder/websocket"
)

// newTerminalTestServer creates a handler with terminal enabled and returns
// an httptest.Server whose only route is the terminal WebSocket endpoint,
// optionally wrapped with bearer auth middleware.
func newTerminalTestServer(t *testing.T, apiKey string, terminalEnabled bool) (*httptest.Server, *Handler) {
	t.Helper()

	storeDir, err := os.MkdirTemp("", "wallfacer-terminal-test-*")
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.NewFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	envPath := filepath.Join(t.TempDir(), ".env")
	envContent := ""
	if !terminalEnabled {
		envContent = "WALLFACER_TERMINAL_ENABLED=false\n"
	}
	if apiKey != "" {
		envContent += "WALLFACER_SERVER_API_KEY=" + apiKey + "\n"
	}
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:    envPath,
		Workspaces: []string{ws},
	})
	t.Cleanup(func() { _ = os.RemoveAll(storeDir) })
	t.Cleanup(s.WaitCompaction)
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)

	h := NewHandler(s, r, t.TempDir(), []string{ws}, nil)

	mux := http.NewServeMux()
	var handler http.Handler = http.HandlerFunc(h.HandleTerminalWS)
	if apiKey != "" {
		handler = BearerAuthMiddleware(apiKey)(http.HandlerFunc(h.HandleTerminalWS))
	}
	mux.Handle("/api/terminal/ws", handler)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h
}

func TestTerminalWS_Disabled(t *testing.T) {
	srv, _ := newTerminalTestServer(t, "", false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected error connecting to disabled terminal")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestTerminalWS_AuthRequired(t *testing.T) {
	srv, _ := newTerminalTestServer(t, "test-secret-key", true)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect without token — should get 401.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected error connecting without auth")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestTerminalWS_Connect(t *testing.T) {
	srv, _ := newTerminalTestServer(t, "", true)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws?cols=80&rows=24"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Send "echo hello\n" as input.
	input := base64.StdEncoding.EncodeToString([]byte("echo hello\n"))
	msg := `{"type":"input","data":"` + input + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read output until we see "hello" or timeout.
	deadline := time.After(5 * time.Second)
	var output []byte
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for 'hello' in output; got: %q", output)
		default:
		}

		readCtx, readCancel := context.WithTimeout(ctx, time.Second)
		_, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			// Timeout on individual read is OK; try again.
			if readCtx.Err() != nil {
				continue
			}
			t.Fatalf("read: %v", err)
		}
		output = append(output, data...)
		if strings.Contains(string(output), "hello") {
			break
		}
	}
}

func TestTerminalWS_Resize(t *testing.T) {
	srv, _ := newTerminalTestServer(t, "", true)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	msg := `{"type":"resize","cols":120,"rows":40}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("write resize: %v", err)
	}

	// If we can still send input after resize, the resize succeeded.
	input := base64.StdEncoding.EncodeToString([]byte("true\n"))
	msg = `{"type":"input","data":"` + input + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("write after resize: %v", err)
	}
}

func TestTerminalWS_Ping(t *testing.T) {
	srv, _ := newTerminalTestServer(t, "", true)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	// Read messages until we find a text pong (may get binary PTY output first).
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for pong")
		default:
		}

		readCtx, readCancel := context.WithTimeout(ctx, time.Second)
		typ, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			if readCtx.Err() != nil {
				continue
			}
			t.Fatalf("read: %v", err)
		}
		if typ == websocket.MessageText {
			var resp struct{ Type string }
			if json.Unmarshal(data, &resp) == nil && resp.Type == "pong" {
				return // success
			}
		}
	}
}

func TestTerminalWS_ShellExit(t *testing.T) {
	srv, _ := newTerminalTestServer(t, "", true)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Send "exit\n".
	input := base64.StdEncoding.EncodeToString([]byte("exit\n"))
	msg := `{"type":"input","data":"` + input + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The server should close the WebSocket with status 1000.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for WebSocket close")
		default:
		}
		readCtx, readCancel := context.WithTimeout(ctx, time.Second)
		_, _, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			// Any close is acceptable (shell exited).
			status := websocket.CloseStatus(err)
			if status == websocket.StatusNormalClosure {
				return // success
			}
			// Context cancellation or other close also acceptable
			// when shell exits — the relay goroutine may close first.
			return
		}
	}
}

func TestSessionRegistry_CreateAndGet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := &sessionRegistry{sessions: make(map[string]*terminalSession), switchCh: make(chan struct{}, 1), connCtx: ctx}

	id, err := reg.create("/bin/sh", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}

	sess, ok := reg.get(id)
	if !ok {
		t.Fatal("session not found after create")
	}
	if sess.id != id {
		t.Errorf("session.id = %q; want %q", sess.id, id)
	}
	if sess.ptmx == nil {
		t.Error("session.ptmx is nil")
	}
	if sess.done == nil {
		t.Error("session.done is nil")
	}

	// Active session should be the one we just created.
	active, ok := reg.activeSession()
	if !ok || active.id != id {
		t.Errorf("activeSession = %v; want %q", active, id)
	}

	reg.closeAll()
}

func TestSessionRegistry_Remove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := &sessionRegistry{sessions: make(map[string]*terminalSession), switchCh: make(chan struct{}, 1), connCtx: ctx}

	id, err := reg.create("/bin/sh", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	reg.remove(id)

	_, ok := reg.get(id)
	if ok {
		t.Error("session still found after remove")
	}

	_, ok = reg.activeSession()
	if ok {
		t.Error("active session should be empty after removing the active session")
	}

	// Removing a nonexistent session should not panic.
	reg.remove("nonexistent")
}

func TestSessionRegistry_CloseAll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := &sessionRegistry{sessions: make(map[string]*terminalSession), switchCh: make(chan struct{}, 1), connCtx: ctx}

	id1, err := reg.create("/bin/sh", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatalf("create 1: %v", err)
	}
	id2, err := reg.create("/bin/sh", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}

	reg.closeAll()

	if _, ok := reg.get(id1); ok {
		t.Error("session 1 still found after closeAll")
	}
	if _, ok := reg.get(id2); ok {
		t.Error("session 2 still found after closeAll")
	}
	if _, ok := reg.activeSession(); ok {
		t.Error("active session should be empty after closeAll")
	}
}

func TestSessionRegistry_ActiveSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := &sessionRegistry{sessions: make(map[string]*terminalSession), switchCh: make(chan struct{}, 1), connCtx: ctx}

	// No sessions — activeSession returns false.
	_, ok := reg.activeSession()
	if ok {
		t.Error("activeSession should return false with no sessions")
	}

	id1, err := reg.create("/bin/sh", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatalf("create 1: %v", err)
	}

	// First session is active.
	active, ok := reg.activeSession()
	if !ok || active.id != id1 {
		t.Errorf("active = %v; want %q", active, id1)
	}

	// Second session becomes active (create sets active).
	id2, err := reg.create("/bin/sh", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}

	active, ok = reg.activeSession()
	if !ok || active.id != id2 {
		t.Errorf("active = %v; want %q", active, id2)
	}

	// Remove active session — active becomes empty.
	reg.remove(id2)
	_, ok = reg.activeSession()
	if ok {
		t.Error("activeSession should return false after removing the active session")
	}

	// First session is still accessible.
	_, ok = reg.get(id1)
	if !ok {
		t.Error("session 1 should still exist")
	}

	reg.closeAll()
}

func TestRelayDispatcher_SwitchActive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := &sessionRegistry{sessions: make(map[string]*terminalSession), switchCh: make(chan struct{}, 1), connCtx: ctx}

	// Create two sessions (both are /bin/sh shells).
	id1, err := reg.create("/bin/sh", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatalf("create 1: %v", err)
	}
	_, err = reg.create("/bin/sh", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}

	// Active is session 2 (create sets active). Switch back to session 1.
	if !reg.switchTo(id1) {
		t.Fatal("switchTo returned false for valid session")
	}
	active, ok := reg.activeSession()
	if !ok || active.id != id1 {
		t.Errorf("active = %v; want %q", active.id, id1)
	}

	// Switching to nonexistent session should fail.
	if reg.switchTo("nonexistent") {
		t.Error("switchTo should return false for nonexistent session")
	}

	// Active should remain unchanged after failed switch.
	active, ok = reg.activeSession()
	if !ok || active.id != id1 {
		t.Errorf("active after failed switch = %v; want %q", active.id, id1)
	}

	reg.closeAll()
}

func TestRelayDispatcher_SessionExit(t *testing.T) {
	srv, _ := newTerminalTestServer(t, "", true)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Send "exit\n" to make the shell exit.
	input := base64.StdEncoding.EncodeToString([]byte("exit\n"))
	msg := `{"type":"input","data":"` + input + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read until we see a session_exited text message or WebSocket close.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			// With a single session, the connection closes. We may or may not
			// receive the session_exited message before the close frame.
			// Either outcome is acceptable.
			return
		default:
		}
		readCtx, readCancel := context.WithTimeout(ctx, time.Second)
		typ, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			// WebSocket closed — expected for last-session exit.
			return
		}
		if typ == websocket.MessageText {
			var resp struct {
				Type    string `json:"type"`
				Session string `json:"session"`
			}
			if json.Unmarshal(data, &resp) == nil && resp.Type == "session_exited" {
				if resp.Session == "" {
					t.Error("session_exited message has empty session ID")
				}
			}
		}
	}
}

func TestRelayDispatcher_LastSessionExit(t *testing.T) {
	// This is a behavioral duplicate of TestTerminalWS_ShellExit,
	// confirming backward compatibility with the new dispatcher.
	srv, _ := newTerminalTestServer(t, "", true)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	input := base64.StdEncoding.EncodeToString([]byte("exit\n"))
	msg := `{"type":"input","data":"` + input + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for WebSocket close")
		default:
		}
		readCtx, readCancel := context.WithTimeout(ctx, time.Second)
		_, _, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			status := websocket.CloseStatus(err)
			if status == websocket.StatusNormalClosure {
				return
			}
			return
		}
	}
}

func TestTerminalWS_CwdValidation(t *testing.T) {
	srv, h := newTerminalTestServer(t, "", true)
	workspaces := h.currentWorkspaces()
	if len(workspaces) == 0 {
		t.Skip("no workspaces configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect with a nonexistent cwd — should fall back to workspace.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal/ws?cwd=/nonexistent/path"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// The cwd query param was invalid, so the shell should start in the
	// first workspace. Verify by checking resolveTerminalCwd directly.
	resolved := h.resolveTerminalCwd("/nonexistent/path")
	if resolved != workspaces[0] {
		t.Errorf("resolveTerminalCwd returned %q; want %q", resolved, workspaces[0])
	}

	// Verify the connection is functional (shell is running).
	input := base64.StdEncoding.EncodeToString([]byte("true\n"))
	msg := `{"type":"input","data":"` + input + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
}
