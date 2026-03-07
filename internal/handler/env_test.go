package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
)

// newTestHandlerWithEnv creates a Handler backed by a temp-dir store and a
// real env file so that UpdateEnvConfig can write to it.
func newTestHandlerWithEnv(t *testing.T) (*Handler, string) {
	t.Helper()
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), ".env")
	// Create an empty env file so envconfig.Update can write to it.
	if err := os.WriteFile(envPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{EnvFile: envPath})
	t.Cleanup(r.WaitBackground)
	h := NewHandler(s, r, t.TempDir(), nil)
	return h, envPath
}

// TestUpdateEnvConfig_TriggersAutoPromote verifies that updating
// max_parallel_tasks immediately triggers auto-promotion when autopilot is
// enabled and there are backlog tasks waiting.
func TestUpdateEnvConfig_TriggersAutoPromote(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	// Enable autopilot so tryAutoPromote will act.
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()

	// Create a backlog task.
	ctx := context.Background()
	_, err := h.store.CreateTask(ctx, "backlog task", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Update max_parallel_tasks to 1 via the HTTP handler.
	body := `{"max_parallel_tasks": 1}`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Poll briefly for the backlog task to leave backlog status.
	// The promotion happens in a background goroutine; the task moves to
	// in_progress (and may then move to failed if the runner has no command),
	// but either outcome proves tryAutoPromote was triggered.
	promoted := false
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		tasks, err := h.store.ListTasks(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) > 0 && tasks[0].Status != "backlog" {
			promoted = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !promoted {
		tasks, _ := h.store.ListTasks(ctx, false)
		status := store.TaskStatus("unknown")
		if len(tasks) > 0 {
			status = tasks[0].Status
		}
		t.Errorf("expected backlog task to be promoted (leave backlog status), got %s", status)
	}
}

// TestUpdateEnvConfig_NoAutoPromoteWhenAutopilotOff verifies that no
// auto-promotion happens when autopilot is disabled.
func TestUpdateEnvConfig_NoAutoPromoteWhenAutopilotOff(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	// autopilot is false by default.

	ctx := context.Background()
	_, err := h.store.CreateTask(ctx, "backlog task", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	body := `{"max_parallel_tasks": 1}`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Wait long enough that a goroutine would have run.
	time.Sleep(100 * time.Millisecond)

	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) > 0 && tasks[0].Status == "in_progress" {
		t.Errorf("expected task to remain in backlog when autopilot is off, got in_progress")
	}
}

// TestValidateBaseURL_SchemeAndIPChecks verifies the scheme, bare-IP, and
// single-label hostname checks that run without DNS resolution.
func TestValidateBaseURL_SchemeAndIPChecks(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"http scheme rejected", "http://api.example.com", true},
		{"file scheme rejected", "file:///etc/passwd", true},
		{"ftp scheme rejected", "ftp://api.example.com", true},
		{"bare IPv4 loopback rejected", "https://127.0.0.1", true},
		{"bare IPv4 private 10.x rejected", "https://10.0.0.1", true},
		{"bare IPv4 private 192.168.x rejected", "https://192.168.1.1", true},
		{"bare IPv4 private 172.16.x rejected", "https://172.16.0.1", true},
		{"bare IPv4 link-local rejected", "https://169.254.169.254", true},
		{"bare IPv6 loopback rejected", "https://[::1]", true},
		{"single-label hostname rejected", "https://localhost", true},
		{"single-label internal name rejected", "https://redis", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBaseURL(tc.url)
			if tc.wantErr && err == nil {
				t.Errorf("validateBaseURL(%q): expected error, got nil", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateBaseURL(%q): unexpected error: %v", tc.url, err)
			}
		})
	}
}

// TestUpdateEnvConfig_SSRFBaseURLReturns422 verifies that PUT /api/env with
// dangerous base_url values is rejected with HTTP 422.
// All listed URLs fail at the scheme-validation step (before any DNS lookup).
func TestUpdateEnvConfig_SSRFBaseURLReturns422(t *testing.T) {
	dangerousURLs := []string{
		"http://169.254.169.254/",
		"http://10.0.0.1/",
		"http://localhost/",
		"http://127.0.0.1/",
		"file:///etc/passwd",
	}
	for _, u := range dangerousURLs {
		t.Run(u, func(t *testing.T) {
			h, _ := newTestHandlerWithEnv(t)
			body, _ := json.Marshal(map[string]string{"base_url": u})
			req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(string(body)))
			w := httptest.NewRecorder()
			h.UpdateEnvConfig(w, req)
			if w.Code != http.StatusUnprocessableEntity {
				t.Errorf("base_url=%q: expected 422, got %d: %s", u, w.Code, w.Body.String())
			}
		})
	}
}

// TestUpdateEnvConfig_ValidHTTPSBaseURL_AcceptedAndStored verifies that a
// valid HTTPS URL with a public hostname is accepted (HTTP 204) and persisted.
// This test requires external DNS resolution and is skipped in offline environments.
func TestUpdateEnvConfig_ValidHTTPSBaseURL_AcceptedAndStored(t *testing.T) {
	const validURL = "https://api.anthropic.com"

	// Pre-check: confirm DNS resolution works in this environment.
	if err := validateBaseURL(validURL); err != nil {
		if strings.Contains(err.Error(), "cannot resolve") {
			t.Skipf("skipping: DNS resolution unavailable (%v)", err)
		}
		t.Fatalf("validateBaseURL(%q) unexpected error: %v", validURL, err)
	}

	h, _ := newTestHandlerWithEnv(t)
	body, _ := json.Marshal(map[string]string{"base_url": validURL})
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the value was stored by reading it back.
	req2 := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w2 := httptest.NewRecorder()
	h.GetEnvConfig(w2, req2)

	var resp envConfigResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode GET /api/env response: %v", err)
	}
	if resp.BaseURL != validURL {
		t.Errorf("stored base_url: want %q, got %q", validURL, resp.BaseURL)
	}
}
