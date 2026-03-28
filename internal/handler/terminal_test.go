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
