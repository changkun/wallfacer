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

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/sandbox"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

const fallbackCodexSandboxImage = "wallfacer-codex:latest"
const defaultArchivedTasksPerPage = 20

// privateIPNets lists networks blocked for SSRF prevention: RFC 1918 private
// ranges, loopback (IPv4 and IPv6), and link-local ranges.
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
	OAuthToken           string                  `json:"oauth_token"` // masked
	APIKey               string                  `json:"api_key"`     // masked
	BaseURL              string                  `json:"base_url"`
	OpenAIAPIKey         string                  `json:"openai_api_key"` // masked
	OpenAIBaseURL        string                  `json:"openai_base_url"`
	DefaultModel         string                  `json:"default_model"`
	TitleModel           string                  `json:"title_model"`
	CodexDefaultModel    string                  `json:"codex_default_model"`
	CodexTitleModel      string                  `json:"codex_title_model"`
	DefaultSandbox       sandbox.Type            `json:"default_sandbox"`
	SandboxByActivity    map[store.SandboxActivity]sandbox.Type `json:"sandbox_by_activity,omitempty"`
	MaxParallelTasks     int                     `json:"max_parallel_tasks"`
	MaxTestParallelTasks int                     `json:"max_test_parallel_tasks"`
	OversightInterval    int                     `json:"oversight_interval"`
	ArchivedTasksPerPage int                     `json:"archived_tasks_per_page"`
	AutoPushEnabled      bool                    `json:"auto_push_enabled"`
	AutoPushThreshold    int                     `json:"auto_push_threshold"`
	SandboxFast          bool                    `json:"sandbox_fast"`
	ContainerNetwork     string                  `json:"container_network"`
	ContainerCPUs        string                  `json:"container_cpus"`
	ContainerMemory      string                  `json:"container_memory"`
	WebhookURL           string                  `json:"webhook_url"` // "configured" when set, "" otherwise
}

type sandboxTestResponse struct {
	TaskID         string       `json:"task_id"`
	Sandbox        sandbox.Type `json:"sandbox"`
	Status         string       `json:"status"`
	LastTestResult string       `json:"last_test_result,omitempty"`
	Result         string       `json:"result,omitempty"`
	StopReason     string       `json:"stop_reason,omitempty"`
}

type sandboxTestRequest struct {
	Sandbox           *sandbox.Type           `json:"sandbox"`
	Timeout           *int                    `json:"timeout"`
	Prompt            *string                 `json:"prompt"`
	OAuthToken        *string                 `json:"oauth_token"`
	APIKey            *string                 `json:"api_key"`
	BaseURL           *string                 `json:"base_url"`
	OpenAIAPIKey      *string                 `json:"openai_api_key"`
	OpenAIBaseURL     *string                 `json:"openai_base_url"`
	DefaultModel      *string                 `json:"default_model"`
	TitleModel        *string                 `json:"title_model"`
	CodexDefaultModel *string                 `json:"codex_default_model"`
	CodexTitleModel   *string                 `json:"codex_title_model"`
	DefaultSandbox    *sandbox.Type           `json:"default_sandbox"`
	SandboxByActivity map[store.SandboxActivity]sandbox.Type `json:"sandbox_by_activity"`
	SandboxFast       *bool                                  `json:"sandbox_fast"`
}

// GetEnvConfig returns the current env configuration with tokens masked.
func (h *Handler) GetEnvConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil {
		http.Error(w, "failed to read env file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	maxParallel := cfg.MaxParallelTasks
	if maxParallel <= 0 {
		maxParallel = defaultMaxConcurrentTasks
	}
	maxTestParallel := cfg.MaxTestParallelTasks
	if maxTestParallel <= 0 {
		maxTestParallel = defaultMaxTestConcurrentTasks
	}
	autoPushThreshold := cfg.AutoPushThreshold
	if autoPushThreshold <= 0 {
		autoPushThreshold = 1
	}
	archivedTasksPerPage := cfg.ArchivedTasksPerPage
	if archivedTasksPerPage <= 0 {
		archivedTasksPerPage = defaultArchivedTasksPerPage
	}
	webhookURL := ""
	if cfg.WebhookURL != "" {
		webhookURL = "configured"
	}
	writeJSON(w, http.StatusOK, envConfigResponse{
		OAuthToken:           envconfig.MaskToken(cfg.OAuthToken),
		APIKey:               envconfig.MaskToken(cfg.APIKey),
		BaseURL:              cfg.BaseURL,
		OpenAIAPIKey:         envconfig.MaskToken(cfg.OpenAIAPIKey),
		OpenAIBaseURL:        cfg.OpenAIBaseURL,
		DefaultModel:         cfg.DefaultModel,
		TitleModel:           cfg.TitleModel,
		CodexDefaultModel:    cfg.CodexDefaultModel,
		CodexTitleModel:      cfg.CodexTitleModel,
		DefaultSandbox:       cfg.DefaultSandbox,
		SandboxByActivity:    cfg.SandboxByActivity(),
		MaxParallelTasks:     maxParallel,
		MaxTestParallelTasks: maxTestParallel,
		OversightInterval:    cfg.OversightInterval,
		ArchivedTasksPerPage: archivedTasksPerPage,
		AutoPushEnabled:      cfg.AutoPushEnabled,
		AutoPushThreshold:    autoPushThreshold,
		SandboxFast:          cfg.SandboxFast,
		ContainerNetwork:     cfg.ContainerNetwork,
		ContainerCPUs:        cfg.ContainerCPUs,
		ContainerMemory:      cfg.ContainerMemory,
		WebhookURL:           webhookURL,
	})
}

// TestWebhook sends a synthetic task.state_changed payload using the canonical
// webhook notifier path without mutating store state or creating a task.
func (h *Handler) TestWebhook(w http.ResponseWriter, r *http.Request) {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil {
		http.Error(w, "failed to read env file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(cfg.WebhookURL) == "" {
		http.Error(w, "webhook URL is not configured", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	payload := runner.NewTaskStateChangedPayload(
		uuid.NewString(),
		store.TaskStatusDone,
		"Webhook delivery smoke test",
		"Manual webhook test triggered from Wallfacer settings.",
		"Synthetic task completion used to verify webhook delivery.",
		now,
	)

	notifier := h.webhookNotifier(cfg)
	if err := notifier.Send(r.Context(), payload); err != nil {
		http.Error(w, "webhook delivery failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TestSandbox spins up a sandbox with the provided (or saved) credentials and
// model settings and runs a smoke-check prompt.
//
// This is used by the settings modal "Test" button for each sandbox block.
func (h *Handler) TestSandbox(w http.ResponseWriter, r *http.Request) {
	var req sandboxTestRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	sb := sandbox.Claude
	if req.Sandbox != nil {
		if !req.Sandbox.IsValid() {
			http.Error(w, "invalid sandbox: use claude or codex", http.StatusBadRequest)
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

	tempEnvFile, err := h.buildTestEnvFile(&req)
	if err != nil {
		http.Error(w, "failed to prepare test env: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tempEnvFile)

	timeout := 3
	if req.Timeout != nil {
		timeout = *req.Timeout
	}

	prompt := "You are a smoke-check for sandbox connectivity and auth. Reply with PASS."
	if req.Prompt != nil && strings.TrimSpace(*req.Prompt) != "" {
		prompt = strings.TrimSpace(*req.Prompt)
	}

	task, err := h.store.CreateTaskWithOptions(r.Context(), store.TaskCreateOptions{
		Prompt:  prompt,
		Timeout: timeout,
		Sandbox: sb,
		Tags:    []string{"sandbox-test"},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.store.UpdateTaskStatus(r.Context(), task.ID, store.TaskStatusInProgress); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.store.UpdateTaskTestRun(r.Context(), task.ID, true, ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	probeRunner := runner.NewRunner(h.store, runner.RunnerConfig{
		Command:          h.runner.Command(),
		SandboxImage:     sandboxImageForTest(sb, h.runner.SandboxImage()),
		EnvFile:          tempEnvFile,
		Workspaces:       strings.Join(h.currentWorkspaces(), " "),
		WorktreesDir:     h.runner.WorktreesDir(),
		InstructionsPath: h.currentInstructionsPath(),
		CodexAuthPath:    h.runner.CodexAuthPath(),
	})
	probeRunner.Run(task.ID, prompt, "", false)

	updated, err := h.store.GetTask(r.Context(), task.ID)
	if err != nil {
		http.Error(w, "failed to read sandbox test result: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Keep test tasks visible for auditability and quick re-test.
	// In the happy path a sandbox connectivity test that returns a PASS
	// is represented as a terminal done task; failures keep their natural
	// terminal state from the runner.
	if updated.Status == store.TaskStatusWaiting && updated.LastTestResult == "pass" {
		if err := h.store.ForceUpdateTaskStatus(r.Context(), task.ID, store.TaskStatusDone); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		updated.Status = store.TaskStatusDone
		updated, err = h.store.GetTask(r.Context(), task.ID)
		if err != nil {
			http.Error(w, "failed to read sandbox test result: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Auto-archive smoke-test tasks so they don't clutter the board.
	if archiveErr := h.store.SetTaskArchived(r.Context(), task.ID, true); archiveErr != nil {
		logger.Handler.Warn("sandbox test: failed to archive smoke task", "task", task.ID, "error", archiveErr)
		// non-fatal: the test result is still returned correctly
	}

	resp := sandboxTestResponse{
		TaskID:         updated.ID.String(),
		Sandbox:        sb,
		Status:         string(updated.Status),
		LastTestResult: updated.LastTestResult,
	}
	if updated.Result != nil {
		resp.Result = *updated.Result
	}
	if updated.StopReason != nil {
		resp.StopReason = *updated.StopReason
	}

	passed := strings.EqualFold(updated.LastTestResult, "pass") &&
		(updated.Status == store.TaskStatusDone || updated.Status == store.TaskStatusWaiting)
	h.setSandboxTestPassed(sb, passed)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) buildTestEnvFile(req *sandboxTestRequest) (string, error) {
	tempFile, err := os.CreateTemp("", "wallfacer-env-test-")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	if h.envFile != "" {
		raw, err := os.ReadFile(h.envFile)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil {
			if _, err := tempFile.Write(raw); err != nil {
				return "", err
			}
		}
	}

	if err := envconfig.Update(tempFile.Name(), envconfig.Updates{
		OAuthToken:        req.OAuthToken,
		APIKey:            req.APIKey,
		BaseURL:           req.BaseURL,
		OpenAIAPIKey:      req.OpenAIAPIKey,
		OpenAIBaseURL:     req.OpenAIBaseURL,
		DefaultModel:      req.DefaultModel,
		TitleModel:        req.TitleModel,
		CodexDefaultModel: req.CodexDefaultModel,
		CodexTitleModel:   req.CodexTitleModel,
		SandboxFast:       reqBoolString(req.SandboxFast),
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

	return tempFile.Name(), nil
}

func sandboxImageForTest(sb sandbox.Type, baseImage string) string {
	if sb == sandbox.Codex {
		return testCodexImage(baseImage)
	}
	return strings.TrimSpace(baseImage)
}

func testCodexImage(baseImage string) string {
	baseImage = strings.TrimSpace(baseImage)
	if baseImage == "" {
		return fallbackCodexSandboxImage
	}

	low := strings.ToLower(baseImage)
	if strings.Contains(low, "wallfacer-codex") {
		return baseImage
	}

	registry := baseImage
	digest := ""
	if at := strings.Index(registry, "@"); at != -1 {
		digest = registry[at:]
		registry = registry[:at]
	}

	// Assume tag format <repo>:<tag> and preserve host:port if present.
	tag := ""
	if at := strings.LastIndex(registry, ":"); at != -1 {
		tag = registry[at:]
		registry = registry[:at]
	}

	prefix := ""
	repoName := registry
	if idx := strings.LastIndex(repoName, "/"); idx != -1 {
		prefix = repoName[:idx+1]
		repoName = repoName[idx+1:]
	}

	if repoName != "wallfacer" {
		return baseImage
	}

	return prefix + "wallfacer-codex" + tag + digest
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
	var req struct {
		OAuthToken           *string                 `json:"oauth_token"`
		APIKey               *string                 `json:"api_key"`
		BaseURL              *string                 `json:"base_url"`
		OpenAIAPIKey         *string                 `json:"openai_api_key"`
		OpenAIBaseURL        *string                 `json:"openai_base_url"`
		DefaultModel         *string                 `json:"default_model"`
		TitleModel           *string                 `json:"title_model"`
		CodexDefaultModel    *string                 `json:"codex_default_model"`
		CodexTitleModel      *string                 `json:"codex_title_model"`
		DefaultSandbox       *sandbox.Type           `json:"default_sandbox"`
		SandboxByActivity    map[store.SandboxActivity]sandbox.Type `json:"sandbox_by_activity"`
		MaxParallelTasks     *int                                   `json:"max_parallel_tasks"`
		MaxTestParallelTasks *int                    `json:"max_test_parallel_tasks"`
		OversightInterval    *int                    `json:"oversight_interval"`
		ArchivedTasksPerPage *int                    `json:"archived_tasks_per_page"`
		AutoPushEnabled      *bool                   `json:"auto_push_enabled"`
		AutoPushThreshold    *int                    `json:"auto_push_threshold"`
		SandboxFast          *bool                   `json:"sandbox_fast"`
		ContainerNetwork     *string                 `json:"container_network"`
		ContainerCPUs        *string                 `json:"container_cpus"`
		ContainerMemory      *string                 `json:"container_memory"`
		WebhookURL           *string                 `json:"webhook_url"`
		WebhookSecret        *string                 `json:"webhook_secret"`
	}
	if !decodeJSONBody(w, r, &req) {
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
	if req.WebhookSecret != nil && *req.WebhookSecret == "" {
		req.WebhookSecret = nil
	}

	// Convert max_parallel_tasks int to string for the env file.
	var maxParallel *string
	if req.MaxParallelTasks != nil {
		v := *req.MaxParallelTasks
		if v < 1 {
			v = 1
		}
		s := fmt.Sprintf("%d", v)
		maxParallel = &s
	}

	// Convert max_test_parallel_tasks int to string for the env file.
	var maxTestParallel *string
	if req.MaxTestParallelTasks != nil {
		v := *req.MaxTestParallelTasks
		if v < 1 {
			v = 1
		}
		s := fmt.Sprintf("%d", v)
		maxTestParallel = &s
	}

	// Convert oversight_interval int to string for the env file.
	// Clamp to [0, 120]: 0 = disabled; 120 minutes = max.
	var oversightInterval *string
	if req.OversightInterval != nil {
		v := *req.OversightInterval
		if v < 0 {
			v = 0
		}
		if v > 120 {
			v = 120
		}
		s := fmt.Sprintf("%d", v)
		oversightInterval = &s
	}
	var archivedTasksPerPage *string
	if req.ArchivedTasksPerPage != nil {
		v := *req.ArchivedTasksPerPage
		if v < 1 {
			v = 1
		}
		if v > 200 {
			v = 200
		}
		s := fmt.Sprintf("%d", v)
		archivedTasksPerPage = &s
	}

	// Convert auto_push_enabled bool to string for the env file.
	var autoPush *string
	if req.AutoPushEnabled != nil {
		v := "false"
		if *req.AutoPushEnabled {
			v = "true"
		}
		autoPush = &v
	}

	// Convert auto_push_threshold int to string for the env file.
	// Clamp to [1, ∞): minimum threshold is 1 commit ahead.
	var autoPushThreshold *string
	if req.AutoPushThreshold != nil {
		v := *req.AutoPushThreshold
		if v < 1 {
			v = 1
		}
		s := fmt.Sprintf("%d", v)
		autoPushThreshold = &s
	}

	var sandboxFast *string
	if req.SandboxFast != nil {
		v := "false"
		if *req.SandboxFast {
			v = "true"
		}
		sandboxFast = &v
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

	if err := envconfig.Update(h.envFile, envconfig.Updates{
		OAuthToken:           req.OAuthToken,
		APIKey:               req.APIKey,
		BaseURL:              req.BaseURL,
		OpenAIAPIKey:         req.OpenAIAPIKey,
		OpenAIBaseURL:        req.OpenAIBaseURL,
		DefaultModel:         req.DefaultModel,
		TitleModel:           req.TitleModel,
		CodexDefaultModel:    req.CodexDefaultModel,
		CodexTitleModel:      req.CodexTitleModel,
		MaxParallel:          maxParallel,
		MaxTestParallel:      maxTestParallel,
		OversightInterval:    oversightInterval,
		ArchivedTasksPerPage: archivedTasksPerPage,
		AutoPush:             autoPush,
		AutoPushThreshold:    autoPushThreshold,
		SandboxFast:          sandboxFast,
		ContainerNetwork:     req.ContainerNetwork,
		ContainerCPUs:        req.ContainerCPUs,
		ContainerMemory:      req.ContainerMemory,
		WebhookURL:           req.WebhookURL,
		WebhookSecret:        req.WebhookSecret,
	}); err != nil {
		http.Error(w, "failed to update env file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Any env update may affect sandbox connectivity/model settings; require
	// a fresh sandbox test before allowing API-key codex tasks again.
	// If valid host codex auth is present, keep codex usable.
	h.setSandboxTestPassed(sandbox.Codex, false)
	h.refreshCodexBootstrapAuthState()
	if err := envconfig.UpdateSandboxSettings(
		h.envFile,
		req.DefaultSandbox,
		req.SandboxByActivity,
	); err != nil {
		http.Error(w, "failed to update env file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// When the parallel task limit changes, re-evaluate immediately so new
	// capacity is filled without waiting for the next store event.
	if req.MaxParallelTasks != nil {
		go h.tryAutoPromote(h.runner.ShutdownCtx())
	}
	if req.MaxTestParallelTasks != nil {
		go h.tryAutoTest(h.runner.ShutdownCtx())
	}

	w.WriteHeader(http.StatusNoContent)
}

func reqBoolString(v *bool) *string {
	if v == nil {
		return nil
	}
	s := "false"
	if *v {
		s = "true"
	}
	return &s
}
