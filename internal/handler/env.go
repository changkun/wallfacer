package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
)

// privateIPNets lists networks blocked for SSRF prevention: RFC 1918 private
// ranges, loopback (IPv4 and IPv6), and link-local ranges. Populated once at
// init to avoid repeated CIDR parsing on every validateBaseURL call.
var privateIPNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"169.254.0.0/16",
		"fe80::/10",
	} {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("invalid CIDR in privateIPNets: " + cidr)
		}
		privateIPNets = append(privateIPNets, network)
	}
}

// isPrivateIP reports whether ip falls in a private, loopback, or link-local
// address range.
func isPrivateIP(ip net.IP) bool {
	for _, network := range privateIPNets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// validateBaseURL validates that u is safe to use as a remote API base URL.
// It rejects: non-https schemes, bare IP addresses, single-label hostnames
// (e.g. "localhost"), and hostnames that resolve to private/loopback/link-local
// IP addresses.
func validateBaseURL(u string) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("URL scheme must be https, got %q", parsed.Scheme)
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a non-empty hostname")
	}
	// Reject bare IP addresses (IPv4 and IPv6).
	if ip := net.ParseIP(hostname); ip != nil {
		return fmt.Errorf("bare IP addresses are not allowed as the base URL hostname")
	}
	// Require at least one dot to rule out single-label names like "localhost"
	// or internal container names that may resolve to private addresses.
	if !strings.Contains(hostname, ".") {
		return fmt.Errorf("hostname %q must be a fully qualified domain name (must contain at least one dot)", hostname)
	}
	// Resolve to IPs and verify none fall in a blocked range.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname %q: %w", hostname, err)
	}
	for _, addr := range addrs {
		if isPrivateIP(addr.IP) {
			return fmt.Errorf("hostname %q resolves to a restricted IP address (%s)", hostname, addr.IP)
		}
	}
	return nil
}

// envConfigResponse is the JSON representation of the env config sent to the UI.
// Sensitive tokens are masked so they are never exposed in full over HTTP.
type envConfigResponse struct {
	SecretStore          string                               `json:"secret_store"`
	OAuthToken           string                               `json:"oauth_token"` // masked
	APIKey               string                               `json:"api_key"`     // masked
	BaseURL              string                               `json:"base_url"`
	OpenAIAPIKey         string                               `json:"openai_api_key"` // masked
	OpenAIBaseURL        string                               `json:"openai_base_url"`
	CursorAPIKey         string                               `json:"cursor_api_key"` // masked
	DefaultModel         string                               `json:"default_model"`
	CodexDefaultModel    string                               `json:"codex_default_model"`
	DefaultSandbox       harness.ID                           `json:"default_sandbox"`
	SandboxByActivity    map[store.SandboxActivity]harness.ID `json:"sandbox_by_activity,omitempty"`
	MaxParallelTasks     int                                  `json:"max_parallel_tasks"`
	MaxAgents            int                                  `json:"max_agents"`
	AgentNice            int                                  `json:"agent_nice"`
	OversightInterval    int                                  `json:"oversight_interval"`
	ArchivedTasksPerPage int                                  `json:"archived_tasks_per_page"`
}

// sandboxTestResponse is the JSON body returned after running a sandbox
// smoke-check via POST /api/env/test. Passed is true when the probe run
// ended with the harness's answer rather than an error.
type sandboxTestResponse struct {
	TaskID     string     `json:"task_id"`
	Sandbox    harness.ID `json:"sandbox"`
	Status     string     `json:"status"`
	Passed     bool       `json:"passed"`
	Result     string     `json:"result,omitempty"`
	StopReason string     `json:"stop_reason,omitempty"`
}

// sandboxTestRequest is the JSON body accepted by POST /api/env/test.
// All credential fields use pointer semantics: nil means "use saved value".
type sandboxTestRequest struct {
	Sandbox           *harness.ID                          `json:"sandbox"`
	Timeout           *int                                 `json:"timeout"`
	Prompt            *string                              `json:"prompt"`
	OAuthToken        *string                              `json:"oauth_token"`
	APIKey            *string                              `json:"api_key"`
	BaseURL           *string                              `json:"base_url"`
	OpenAIAPIKey      *string                              `json:"openai_api_key"`
	OpenAIBaseURL     *string                              `json:"openai_base_url"`
	CursorAPIKey      *string                              `json:"cursor_api_key"`
	DefaultModel      *string                              `json:"default_model"`
	CodexDefaultModel *string                              `json:"codex_default_model"`
	DefaultSandbox    *harness.ID                          `json:"default_sandbox"`
	SandboxByActivity map[store.SandboxActivity]harness.ID `json:"sandbox_by_activity"`
}

// GetEnvConfig returns the current env configuration with tokens masked.
func (h *Handler) GetEnvConfig(w http.ResponseWriter, _ *http.Request) {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil {
		http.Error(w, "failed to read env file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	maxParallel := cfg.MaxParallelTasks
	if maxParallel <= 0 {
		maxParallel = constants.DefaultMaxConcurrentTasks
	}
	archivedTasksPerPage := cfg.ArchivedTasksPerPage
	if archivedTasksPerPage <= 0 {
		archivedTasksPerPage = constants.DefaultArchivedTasksPerPage
	}
	// agentNice 0 reports the backend default; negative means throttling is
	// disabled. maxAgents 0 means unlimited.
	agentNice := cfg.AgentNice
	if agentNice == 0 {
		agentNice = executor.DefaultAgentNice
	}
	httpjson.Write(w, http.StatusOK, envConfigResponse{
		SecretStore: cfg.SecretStore, OAuthToken: envconfig.MaskToken(cfg.OAuthToken),
		APIKey:               envconfig.MaskToken(cfg.APIKey),
		BaseURL:              cfg.BaseURL,
		OpenAIAPIKey:         envconfig.MaskToken(cfg.OpenAIAPIKey),
		OpenAIBaseURL:        cfg.OpenAIBaseURL,
		CursorAPIKey:         envconfig.MaskToken(cfg.CursorAPIKey),
		DefaultModel:         cfg.DefaultModel,
		CodexDefaultModel:    cfg.CodexDefaultModel,
		DefaultSandbox:       cfg.DefaultSandbox,
		SandboxByActivity:    cfg.SandboxByActivity(),
		MaxParallelTasks:     maxParallel,
		MaxAgents:            cfg.MaxAgents,
		AgentNice:            agentNice,
		OversightInterval:    cfg.OversightInterval,
		ArchivedTasksPerPage: archivedTasksPerPage,
	})
}

// TestSandbox spins up a sandbox with the provided (or saved) credentials and
// model settings and runs a smoke-check prompt.
//
// This is used by the settings modal "Test" button for each sandbox block.
func (h *Handler) TestSandbox(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[sandboxTestRequest](w, r)
	if !ok {
		return
	}

	sb := harness.Claude
	if req.Sandbox != nil {
		if !req.Sandbox.IsValid() {
			http.Error(w, "invalid sandbox: unknown harness", http.StatusBadRequest)
			return
		}
		sb = *req.Sandbox
	}

	// Preserve existing token handling behavior (empty string means no change).
	if req.OAuthToken != nil && *req.OAuthToken == "" {
		req.OAuthToken = nil
	}
	if req.APIKey != nil && *req.APIKey == "" {
		req.APIKey = nil
	}
	if req.OpenAIAPIKey != nil && *req.OpenAIAPIKey == "" {
		req.OpenAIAPIKey = nil
	}
	if req.CursorAPIKey != nil && *req.CursorAPIKey == "" {
		req.CursorAPIKey = nil
	}

	// Validate base URLs (same checks as regular env updates).
	if req.BaseURL != nil && *req.BaseURL != "" {
		if err := validateBaseURL(*req.BaseURL); err != nil {
			http.Error(w, "invalid base_url: "+err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}
	if req.OpenAIBaseURL != nil && *req.OpenAIBaseURL != "" {
		if err := validateBaseURL(*req.OpenAIBaseURL); err != nil {
			http.Error(w, "invalid openai_base_url: "+err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}

	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	tempEnvFile, err := h.buildTestEnvFile(req)
	if err != nil {
		http.Error(w, "failed to prepare test env: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer envconfig.RemoveTestFile(tempEnvFile)

	timeout := 3
	if req.Timeout != nil {
		timeout = *req.Timeout
	}

	prompt := "You are a smoke-check for sandbox connectivity and auth. Reply with PASS."
	if req.Prompt != nil && strings.TrimSpace(*req.Prompt) != "" {
		prompt = strings.TrimSpace(*req.Prompt)
	}

	task, err := s.CreateTaskWithOptions(r.Context(), store.TaskCreateOptions{
		Prompt:  prompt,
		Timeout: timeout,
		Sandbox: sb,
		Tags:    []string{"sandbox-test"},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.UpdateTaskStatus(r.Context(), task.ID, store.TaskStatusInProgress); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	probeRunner := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:       tempEnvFile,
		Workspaces:    h.currentWorkspaces(),
		WorktreesDir:  h.runner.WorktreesDir(),
		TmpDir:        h.runner.TmpDir(),
		CodexAuthPath: h.runner.CodexAuthPath(),
	})
	probeRunner.Run(task.ID, prompt, "", false)
	// Run starts the oversight summary in the background when the probe ends
	// its turn; wait for it so it runs on the probed credentials before the
	// deferred cleanup removes the temporary env file.
	probeRunner.WaitBackground()

	updated, err := s.GetTask(r.Context(), task.ID)
	if err != nil {
		http.Error(w, "failed to read sandbox test result: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// The probe passes when the run ends waiting for review with no failure
	// category: the harness started, authenticated and answered. A probe
	// that passes is recorded as a done task; failures keep their natural
	// terminal state from the runner.
	passed := updated.Status == store.TaskStatusWaiting && updated.FailureCategory == ""
	if passed {
		if err := s.ForceUpdateTaskStatus(r.Context(), task.ID, store.TaskStatusDone); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		updated.Status = store.TaskStatusDone
		updated, err = s.GetTask(r.Context(), task.ID)
		if err != nil {
			http.Error(w, "failed to read sandbox test result: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Auto-archive smoke-test tasks so they don't clutter the board.
	if archiveErr := s.SetTaskArchived(r.Context(), task.ID, true); archiveErr != nil {
		logger.Handler.Warn("sandbox test: failed to archive smoke task", "task", task.ID, "error", archiveErr)
		// non-fatal: the test result is still returned correctly
	}

	resp := sandboxTestResponse{
		TaskID:  updated.ID.String(),
		Sandbox: sb,
		Status:  string(updated.Status),
		Passed:  passed,
	}
	if updated.Result != nil {
		resp.Result = *updated.Result
	}
	if updated.StopReason != nil {
		resp.StopReason = *updated.StopReason
	}

	h.setSandboxTestPassed(sb, passed)

	httpjson.Write(w, http.StatusOK, resp)
}

// buildTestEnvFile creates a temporary .env file by copying the current env
// file and applying any overrides from the sandbox test request. The caller
// is responsible for removing the temp file after use.
func (h *Handler) buildTestEnvFile(req *sandboxTestRequest) (string, error) {
	tempFile, err := os.CreateTemp("", "wallfacer-env-test-")
	if err != nil {
		return "", err
	}
	_ = tempFile.Close()
	success := false
	defer func() {
		if !success {
			envconfig.RemoveTestFile(tempFile.Name())
		}
	}()
	if h.envFile != "" {
		if err := envconfig.CloneForTest(h.envFile, tempFile.Name()); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}

	if err := envconfig.Update(tempFile.Name(), envconfig.Updates{
		OAuthToken:        req.OAuthToken,
		APIKey:            req.APIKey,
		BaseURL:           req.BaseURL,
		OpenAIAPIKey:      req.OpenAIAPIKey,
		OpenAIBaseURL:     req.OpenAIBaseURL,
		CursorAPIKey:      req.CursorAPIKey,
		DefaultModel:      req.DefaultModel,
		CodexDefaultModel: req.CodexDefaultModel,
	}); err != nil {
		return "", err
	}
	if err := envconfig.UpdateSandboxSettings(
		tempFile.Name(),
		req.DefaultSandbox,
		req.SandboxByActivity,
	); err != nil {
		return "", err
	}

	success = true
	return tempFile.Name(), nil
}

// UpdateEnvConfig writes changes to the env file.
//
// Pointer semantics per field:
//   - field absent from JSON body (null) → leave unchanged
//   - field present with a value          → update
//   - field present with ""               → clear (for non-secret fields)
//
// For token fields (oauth_token, api_key, openai_api_key), an empty value is treated
// as "no change" to prevent accidental token deletion.
func (h *Handler) UpdateEnvConfig(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		SecretStore          *string                              `json:"secret_store"`
		OAuthToken           *string                              `json:"oauth_token"`
		APIKey               *string                              `json:"api_key"`
		BaseURL              *string                              `json:"base_url"`
		OpenAIAPIKey         *string                              `json:"openai_api_key"`
		OpenAIBaseURL        *string                              `json:"openai_base_url"`
		CursorAPIKey         *string                              `json:"cursor_api_key"`
		DefaultModel         *string                              `json:"default_model"`
		CodexDefaultModel    *string                              `json:"codex_default_model"`
		DefaultSandbox       *harness.ID                          `json:"default_sandbox"`
		SandboxByActivity    map[store.SandboxActivity]harness.ID `json:"sandbox_by_activity"`
		MaxParallelTasks     *int                                 `json:"max_parallel_tasks"`
		MaxAgents            *int                                 `json:"max_agents"`
		AgentNice            *int                                 `json:"agent_nice"`
		OversightInterval    *int                                 `json:"oversight_interval"`
		ArchivedTasksPerPage *int                                 `json:"archived_tasks_per_page"`
	}](w, r)
	if !ok {
		return
	}

	// Guard: treat empty-string tokens and secrets as "no change" to avoid accidental clears.
	if req.OAuthToken != nil && *req.OAuthToken == "" {
		req.OAuthToken = nil
	}
	if req.APIKey != nil && *req.APIKey == "" {
		req.APIKey = nil
	}
	if req.OpenAIAPIKey != nil && *req.OpenAIAPIKey == "" {
		req.OpenAIAPIKey = nil
	}
	if req.CursorAPIKey != nil && *req.CursorAPIKey == "" {
		req.CursorAPIKey = nil
	}
	// Convert max_parallel_tasks int to string for the env file.
	var maxParallel *string
	if req.MaxParallelTasks != nil {
		v := max(*req.MaxParallelTasks, 1)
		s := fmt.Sprintf("%d", v)
		maxParallel = &s
	}

	// Convert max_agents int to string. Clamp to [0, ∞): 0 = unlimited.
	var maxAgents *string
	if req.MaxAgents != nil {
		v := max(*req.MaxAgents, 0)
		s := fmt.Sprintf("%d", v)
		maxAgents = &s
	}

	// Convert agent_nice int to string. Clamp to [-1, 19]: -1 disables
	// throttling, 0 keeps the backend default, 1..19 sets the niceness.
	var agentNice *string
	if req.AgentNice != nil {
		v := min(max(*req.AgentNice, -1), 19)
		s := fmt.Sprintf("%d", v)
		agentNice = &s
	}

	// Convert oversight_interval int to string for the env file.
	// Clamp to [0, 120]: 0 = disabled; 120 minutes = max.
	var oversightInterval *string
	if req.OversightInterval != nil {
		v := min(max(*req.OversightInterval, 0), 120)
		s := fmt.Sprintf("%d", v)
		oversightInterval = &s
	}
	var archivedTasksPerPage *string
	if req.ArchivedTasksPerPage != nil {
		v := min(max(*req.ArchivedTasksPerPage, 1), 200)
		s := fmt.Sprintf("%d", v)
		archivedTasksPerPage = &s
	}

	// Validate the base URL if provided to prevent SSRF.
	if req.BaseURL != nil && *req.BaseURL != "" {
		if err := validateBaseURL(*req.BaseURL); err != nil {
			http.Error(w, "invalid base_url: "+err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}
	if req.OpenAIBaseURL != nil && *req.OpenAIBaseURL != "" {
		if err := validateBaseURL(*req.OpenAIBaseURL); err != nil {
			http.Error(w, "invalid openai_base_url: "+err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}

	if req.SecretStore != nil && *req.SecretStore != "file" && *req.SecretStore != "keyring" {
		http.Error(w, "secret_store must be file or keyring", http.StatusUnprocessableEntity)
		return
	}

	if err := envconfig.Update(h.envFile, envconfig.Updates{
		SecretStore: req.SecretStore, OAuthToken: req.OAuthToken,
		APIKey:               req.APIKey,
		BaseURL:              req.BaseURL,
		OpenAIAPIKey:         req.OpenAIAPIKey,
		OpenAIBaseURL:        req.OpenAIBaseURL,
		CursorAPIKey:         req.CursorAPIKey,
		DefaultModel:         req.DefaultModel,
		CodexDefaultModel:    req.CodexDefaultModel,
		MaxParallel:          maxParallel,
		MaxAgents:            maxAgents,
		AgentNice:            agentNice,
		OversightInterval:    oversightInterval,
		ArchivedTasksPerPage: archivedTasksPerPage,
	}); err != nil {
		http.Error(w, "failed to update env file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Any env update may affect sandbox connectivity/model settings; require
	// a fresh sandbox test before allowing API-key codex tasks again.
	// If valid host codex auth is present, keep codex usable.
	h.setSandboxTestPassed(harness.Codex, false)
	h.refreshCodexBootstrapAuthState()
	if err := envconfig.UpdateSandboxSettings(
		h.envFile,
		req.DefaultSandbox,
		req.SandboxByActivity,
	); err != nil {
		http.Error(w, "failed to update env file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Invalidate the cached parallel limit so the next call to
	// maxConcurrentTasks re-reads it from the env file.
	h.cachedMaxParallel.Invalidate()

	w.WriteHeader(http.StatusNoContent)
}
