package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/sandbox"
	"changkun.de/wallfacer/internal/store"
)

func TestGetEnvConfig_WebhookURLMasked(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	webhookURL := "https://example.com/webhook"
	webhookSecret := "topsecret"
	if err := envconfig.Update(envPath, envconfig.Updates{WebhookURL: &webhookURL, WebhookSecret: &webhookSecret}); err != nil {
		t.Fatalf("Update env: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w := httptest.NewRecorder()
	h.GetEnvConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp envConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WebhookURL != "configured" {
		t.Fatalf("webhook_url = %q, want %q", resp.WebhookURL, "configured")
	}
}

func TestTestWebhook_MissingConfiguration(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/env/test-webhook", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.TestWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "webhook URL is not configured") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestTestWebhook_Success(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	type received struct {
		header http.Header
		body   []byte
	}
	reqCh := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqCh <- received{header: r.Header.Clone(), body: body}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	webhookURL := srv.URL
	webhookSecret := "handler-secret"
	if err := envconfig.Update(envPath, envconfig.Updates{WebhookURL: &webhookURL, WebhookSecret: &webhookSecret}); err != nil {
		t.Fatalf("Update env: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/env/test-webhook", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.TestWebhook(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case got := <-reqCh:
		if got.header.Get("X-Wallfacer-Event") != runner.WebhookEventTaskStateChanged {
			t.Fatalf("event header = %q", got.header.Get("X-Wallfacer-Event"))
		}
		var payload runner.WebhookPayload
		if err := json.Unmarshal(got.body, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.EventType != runner.WebhookEventTaskStateChanged {
			t.Fatalf("event_type = %q", payload.EventType)
		}
		if payload.Status != store.TaskStatusDone {
			t.Fatalf("status = %q", payload.Status)
		}
		mac := hmac.New(sha256.New, []byte(webhookSecret))
		mac.Write(got.body)
		wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got.header.Get("X-Wallfacer-Signature") != wantSig {
			t.Fatalf("signature = %q, want %q", got.header.Get("X-Wallfacer-Signature"), wantSig)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook request")
	}

	tasks, err := h.store.ListTasks(context.Background(), true)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks to be created, got %d", len(tasks))
	}
}

func TestTestWebhook_DownstreamFailure(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	webhookURL := srv.URL
	if err := envconfig.Update(envPath, envconfig.Updates{WebhookURL: &webhookURL}); err != nil {
		t.Fatalf("Update env: %v", err)
	}
	h.webhookNotifier = func(cfg envconfig.Config) *runner.WebhookNotifier {
		wn := runner.NewWorkspaceWebhookNotifier(h.workspace, cfg)
		wn.SetRetryBackoffs([]time.Duration{0, 5 * time.Millisecond})
		return wn
	}

	req := httptest.NewRequest(http.MethodPost, "/api/env/test-webhook", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.TestWebhook(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "webhook delivery failed") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// newTestHandlerWithEnv creates a Handler backed by a temp-dir store and a
// real env file so that UpdateEnvConfig can write to it.
func newTestHandlerWithEnv(t *testing.T) (*Handler, string) {
	t.Helper()
	// Use os.MkdirTemp instead of t.TempDir for the store directory so that
	// late trace-file writes from background goroutines (which race with
	// shutdown) don't cause TempDir cleanup failures.
	storeDir, err := os.MkdirTemp("", "wallfacer-handler-test-*")
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.NewStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), ".env")
	// Create an empty env file so envconfig.Update can write to it.
	if err := os.WriteFile(envPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{EnvFile: envPath})
	// Cleanups run in LIFO order. Register WaitBackground first (runs second);
	// register Shutdown second (runs first) so background goroutines are
	// cancelled before WaitBackground drains remaining work.
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)
	t.Cleanup(func() { os.RemoveAll(storeDir) })
	h := NewHandler(s, r, t.TempDir(), nil, nil)
	return h, envPath
}

func newTestHandlerWithEnvAndCodexAuth(t *testing.T) (*Handler, string, string) {
	t.Helper()
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	codexAuthDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexAuthDir, "auth.json"), []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"header.payload.sig","refresh_token":"rt"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{EnvFile: envPath, CodexAuthPath: codexAuthDir})
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)
	h := NewHandler(s, r, t.TempDir(), nil, nil)
	return h, envPath, codexAuthDir
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

// TestUpdateEnvConfig_OversightIntervalRoundTrip verifies that oversight_interval
// is stored via PUT and returned by GET.
func TestUpdateEnvConfig_OversightIntervalRoundTrip(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	body := `{"oversight_interval": 15}`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w2 := httptest.NewRecorder()
	h.GetEnvConfig(w2, req2)

	var resp envConfigResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OversightInterval != 15 {
		t.Errorf("oversight_interval: want 15, got %d", resp.OversightInterval)
	}
}

func TestUpdateEnvConfig_SandboxFastRoundTrip(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(`{"sandbox_fast":false}`))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w2 := httptest.NewRecorder()
	h.GetEnvConfig(w2, req2)

	var resp envConfigResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SandboxFast {
		t.Fatal("sandbox_fast = true; want false")
	}
}

func TestUpdateEnvConfig_CodexModelRoundTrip(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	if err := validateBaseURL("https://api.openai.com/v1"); err != nil {
		if strings.Contains(err.Error(), "cannot resolve") {
			t.Skipf("skipping: DNS resolution unavailable (%v)", err)
		}
		t.Fatalf("validateBaseURL(%q) unexpected error: %v", "https://api.openai.com/v1", err)
	}

	body := map[string]string{
		"default_model":       "claude-opus-4-1",
		"title_model":         "claude-haiku-4-5",
		"openai_api_key":      "sk-openai-test",
		"openai_base_url":     "https://api.openai.com/v1",
		"codex_default_model": "codex-mini-latest",
		"codex_title_model":   "codex-title-test",
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(string(raw)))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w2 := httptest.NewRecorder()
	h.GetEnvConfig(w2, req2)

	var resp envConfigResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DefaultModel != "claude-opus-4-1" {
		t.Errorf("DefaultModel = %q; want claude-opus-4-1", resp.DefaultModel)
	}
	if resp.TitleModel != "claude-haiku-4-5" {
		t.Errorf("TitleModel = %q; want claude-haiku-4-5", resp.TitleModel)
	}
	if resp.CodexDefaultModel != "codex-mini-latest" {
		t.Errorf("CodexDefaultModel = %q; want codex-mini-latest", resp.CodexDefaultModel)
	}
	if resp.CodexTitleModel != "codex-title-test" {
		t.Errorf("CodexTitleModel = %q; want codex-title-test", resp.CodexTitleModel)
	}
	if resp.OpenAIAPIKey != "sk-o...test" {
		t.Errorf("OpenAIAPIKey = %q; want masked value", resp.OpenAIAPIKey)
	}
	if resp.OpenAIBaseURL != "https://api.openai.com/v1" {
		t.Errorf("OpenAIBaseURL = %q; want https://api.openai.com/v1", resp.OpenAIBaseURL)
	}
}

// TestUpdateEnvConfig_OversightIntervalClamped verifies that values outside
// [0, 120] are clamped before writing to the env file.
func TestUpdateEnvConfig_OversightIntervalClamped(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{"negative clamped to 0", -5, 0},
		{"above max clamped to 120", 200, 120},
		{"zero stays zero", 0, 0},
		{"valid stays", 60, 60},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newTestHandlerWithEnv(t)
			body, _ := json.Marshal(map[string]int{"oversight_interval": tc.input})
			req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(string(body)))
			w := httptest.NewRecorder()
			h.UpdateEnvConfig(w, req)
			if w.Code != http.StatusNoContent {
				t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
			}

			req2 := httptest.NewRequest(http.MethodGet, "/api/env", nil)
			w2 := httptest.NewRecorder()
			h.GetEnvConfig(w2, req2)
			var resp envConfigResponse
			if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.OversightInterval != tc.want {
				t.Errorf("oversight_interval: want %d, got %d", tc.want, resp.OversightInterval)
			}
		})
	}
}

func TestUpdateEnvConfig_ArchivedTasksPerPageRoundTrip(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	body := `{"archived_tasks_per_page": 33}`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/env", nil)
	w2 := httptest.NewRecorder()
	h.GetEnvConfig(w2, req2)

	var resp envConfigResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ArchivedTasksPerPage != 33 {
		t.Errorf("archived_tasks_per_page: want 33, got %d", resp.ArchivedTasksPerPage)
	}
}

func TestUpdateEnvConfig_ArchivedTasksPerPageClamped(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{"minimum clamp", 0, 1},
		{"maximum clamp", 999, 200},
		{"valid", 25, 25},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newTestHandlerWithEnv(t)
			body, _ := json.Marshal(map[string]int{"archived_tasks_per_page": tc.input})
			req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(string(body)))
			w := httptest.NewRecorder()
			h.UpdateEnvConfig(w, req)
			if w.Code != http.StatusNoContent {
				t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
			}

			req2 := httptest.NewRequest(http.MethodGet, "/api/env", nil)
			w2 := httptest.NewRecorder()
			h.GetEnvConfig(w2, req2)
			var resp envConfigResponse
			if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.ArchivedTasksPerPage != tc.want {
				t.Errorf("archived_tasks_per_page: want %d, got %d", tc.want, resp.ArchivedTasksPerPage)
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

func TestTestSandbox_InvalidSandboxRejected(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	body := map[string]string{"sandbox": "llama"}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/env/test", strings.NewReader(string(raw)))
	w := httptest.NewRecorder()
	h.TestSandbox(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestSandbox_InvalidBaseURLRejected(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	body := map[string]string{
		"sandbox":  "claude",
		"base_url": "http://localhost",
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/env/test", strings.NewReader(string(raw)))
	w := httptest.NewRecorder()
	h.TestSandbox(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSandboxImageForTest_CodexResolution(t *testing.T) {
	tests := []struct {
		name    string
		sandbox string
		inImage string
		want    string
	}{
		{
			name:    "codex uses wallfacer-codex default image",
			sandbox: "codex",
			inImage: "wallfacer:latest",
			want:    "wallfacer-codex:latest",
		},
		{
			name:    "codex preserves hosted wallfacer image family",
			sandbox: "codex",
			inImage: "ghcr.io/changkun/wallfacer:latest",
			want:    "ghcr.io/changkun/wallfacer-codex:latest",
		},
		{
			name:    "codex keeps preconfigured codex image",
			sandbox: "codex",
			inImage: "wallfacer-codex:latest",
			want:    "wallfacer-codex:latest",
		},
		{
			name:    "claude keeps default image",
			sandbox: "claude",
			inImage: "wallfacer:latest",
			want:    "wallfacer:latest",
		},
		{
			name:    "codex default fallback",
			sandbox: "codex",
			inImage: "",
			want:    fallbackCodexSandboxImage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sandboxImageForTest(sandbox.Type(tt.sandbox), tt.inImage)
			if got != tt.want {
				t.Fatalf("sandboxImageForTest(%q, %q) = %q; want %q", tt.sandbox, tt.inImage, got, tt.want)
			}
		})
	}
}

func TestTestSandbox_PersistsTaskAfterRun(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	body := map[string]interface{}{
		"sandbox": "claude",
		"timeout": 1,
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/env/test", strings.NewReader(string(raw)))
	w := httptest.NewRecorder()
	h.TestSandbox(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp sandboxTestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TaskID == "" {
		t.Fatalf("expected task_id in response")
	}

	tasks, err := h.store.ListTasks(context.Background(), false)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one task remaining after test run, got %d", len(tasks))
	}
	if tasks[0].ID.String() != resp.TaskID {
		t.Fatalf("remaining task id mismatch: got %q, want %q", tasks[0].ID, resp.TaskID)
	}
	if tasks[0].Status == store.TaskStatusBacklog || tasks[0].Archived {
		t.Fatalf("expected completed test task, got status=%q archived=%v", tasks[0].Status, tasks[0].Archived)
	}
}

// --- strict JSON decoding ---

// TestUpdateEnvConfig_RejectsUnknownFields verifies that unknown JSON keys return 400.
func TestUpdateEnvConfig_RejectsUnknownFields(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	body := `{"api_key": "test-key", "unknown_field": true}`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown fields, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateEnvConfig_RejectsTrailingContent verifies that trailing data after
// the JSON object returns 400.
func TestUpdateEnvConfig_RejectsTrailingContent(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	body := `{"api_key": "test-key"} garbage`
	req := httptest.NewRequest(http.MethodPut, "/api/env", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for trailing content, got %d: %s", w.Code, w.Body.String())
	}
}
