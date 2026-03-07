package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/instructions"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
)

// ssrfHardenedTransport returns an http.Transport that re-checks the resolved
// IP address against private/loopback/link-local ranges immediately before
// opening the TCP connection, providing defense-in-depth against DNS-rebinding
// attacks even when validateBaseURL already approved the hostname.
func ssrfHardenedTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout: 30 * time.Second,
	}
	return &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("ssrf guard: %w", err)
			}
			addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("ssrf guard: resolve %q: %w", host, err)
			}
			if len(addrs) == 0 {
				return nil, fmt.Errorf("ssrf guard: no addresses resolved for %s", host)
			}
			for _, a := range addrs {
				if isPrivateIP(a.IP) {
					return nil, fmt.Errorf("ssrf guard: connection to %s (%s) is blocked", host, a.IP)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addrs[0].IP.String(), port))
		},
	}
}

// fetchModelsFromGateway queries the LLM gateway's /v1/models endpoint
// and returns the list of available model IDs.
func fetchModelsFromGateway(baseURL, authToken, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/models"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	} else if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: ssrfHardenedTransport(),
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// OpenAI-compatible /v1/models response format.
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var models []string
	for _, m := range result.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	sort.Strings(models)
	return models, nil
}

// ideationRunning returns true if any idea-agent task is currently in_progress.
func (h *Handler) ideationRunning(ctx context.Context) bool {
	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		return false
	}
	for _, t := range tasks {
		if t.Kind == store.TaskKindIdeaAgent && t.Status == store.TaskStatusInProgress {
			return true
		}
	}
	return false
}

// GetConfig returns the server configuration (workspaces, instructions path).
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	// Read the current default model from the env file.
	defaultModel := ""
	var models []string
	if h.envFile != "" {
		if cfg, err := envconfig.Parse(h.envFile); err == nil {
			defaultModel = cfg.DefaultModel
			// Fetch available models from the gateway if a base URL is configured.
			if cfg.BaseURL != "" {
				if fetched, err := fetchModelsFromGateway(cfg.BaseURL, cfg.AuthToken, cfg.APIKey); err != nil {
					logger.Handler.Warn("failed to fetch models from gateway", "url", cfg.BaseURL, "error", err)
				} else if len(fetched) > 0 {
					models = fetched
				}
			}
		}
	}

	resp := map[string]any{
		"workspaces":        h.runner.Workspaces(),
		"instructions_path": instructions.FilePath(h.configDir, h.workspaces),
		"autopilot":         h.AutopilotEnabled(),
		"ideation":          h.IdeationEnabled(),
		"ideation_running":  h.ideationRunning(r.Context()),
		"ideation_interval": int(h.IdeationInterval().Minutes()),
		"models":            models,
		"default_model":     defaultModel,
	}
	if nextRun := h.IdeationNextRun(); !nextRun.IsZero() {
		resp["ideation_next_run"] = nextRun
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateConfig handles PUT /api/config to update server-level settings.
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Autopilot        *bool `json:"autopilot"`
		Ideation         *bool `json:"ideation"`
		IdeationInterval *int  `json:"ideation_interval"` // minutes; 0 = run immediately on completion
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Autopilot != nil {
		h.SetAutopilot(*req.Autopilot)
	}
	// Re-trigger auto-promotion in case autopilot was just enabled.
	if h.AutopilotEnabled() {
		go h.tryAutoPromote(r.Context())
	}
	if req.IdeationInterval != nil {
		mins := *req.IdeationInterval
		if mins < 0 {
			mins = 0
		}
		h.SetIdeationInterval(time.Duration(mins) * time.Minute)
		// Reschedule with new interval if ideation is already active.
		if h.IdeationEnabled() {
			go h.maybeScheduleNextIdeation(r.Context())
		}
	}
	if req.Ideation != nil {
		h.SetIdeation(*req.Ideation)
		if *req.Ideation {
			// Enqueue or schedule a new idea-agent task card when enabled,
			// unless one is already backlogged or running.
			go h.maybeScheduleNextIdeation(r.Context())
		}
	}
	resp := map[string]any{
		"autopilot":         h.AutopilotEnabled(),
		"ideation":          h.IdeationEnabled(),
		"ideation_running":  h.ideationRunning(r.Context()),
		"ideation_interval": int(h.IdeationInterval().Minutes()),
	}
	if nextRun := h.IdeationNextRun(); !nextRun.IsZero() {
		resp["ideation_next_run"] = nextRun
	}
	writeJSON(w, http.StatusOK, resp)
}
