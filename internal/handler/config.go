package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/instructions"
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
func availableSandboxes(cfg envconfig.Config) []string {
	sandboxSet := map[string]bool{}
	var sandboxes []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || sandboxSet[name] {
			return
		}
		sandboxSet[name] = true
		sandboxes = append(sandboxes, name)
	}
	// Always expose both built-in sandboxes in the UI so users can select
	// either provider even before model/env values are configured.
	add("claude")
	add("codex")

	if cfg.DefaultSandbox != "" {
		add(cfg.DefaultSandbox)
	}
	for _, v := range cfg.SandboxByActivity() {
		add(v)
	}
	return sandboxes
}

func defaultSandbox(cfg envconfig.Config) string {
	if cfg.DefaultSandbox != "" {
		return cfg.DefaultSandbox
	}
	if cfg.DefaultModel != "" {
		return "claude"
	}
	if cfg.CodexDefaultModel != "" {
		return "codex"
	}
	return "claude"
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
	defaultModel := ""
	defaultSandboxName := ""
	var sandboxes []string
	if h.envFile != "" {
		if cfg, err := envconfig.Parse(h.envFile); err == nil {
			defaultModel = cfg.DefaultModel
			sandboxes = availableSandboxes(cfg)
			defaultSandboxName = defaultSandbox(cfg)
			resp := map[string]any{
				"workspaces":        h.runner.Workspaces(),
				"instructions_path": instructions.FilePath(h.configDir, h.workspaces),
				"sandboxes":         sandboxes,
				"default_sandbox":   defaultSandboxName,
				"activity_sandboxes": cfg.SandboxByActivity(),
				"autopilot":         h.AutopilotEnabled(),
				"autotest":          h.AutotestEnabled(),
				"autosubmit":        h.AutosubmitEnabled(),
				"ideation":          h.IdeationEnabled(),
				"ideation_running":  h.ideationRunning(r.Context()),
				"ideation_interval": int(h.IdeationInterval().Minutes()),
				"default_model":     defaultModel,
			}
			if nextRun := h.IdeationNextRun(); !nextRun.IsZero() {
				resp["ideation_next_run"] = nextRun
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}
	if len(sandboxes) == 0 {
		sandboxes = []string{"claude", "codex"}
	}
	if defaultSandboxName == "" {
		defaultSandboxName = sandboxes[0]
	}

	resp := map[string]any{
		"workspaces":        h.runner.Workspaces(),
		"instructions_path": instructions.FilePath(h.configDir, h.workspaces),
		"sandboxes":         sandboxes,
		"default_sandbox":   defaultSandboxName,
		"activity_sandboxes": map[string]string{},
		"autopilot":         h.AutopilotEnabled(),
		"autotest":          h.AutotestEnabled(),
		"autosubmit":        h.AutosubmitEnabled(),
		"ideation":          h.IdeationEnabled(),
		"ideation_running":  h.ideationRunning(r.Context()),
		"ideation_interval": int(h.IdeationInterval().Minutes()),
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
		Autotest         *bool `json:"autotest"`
		Autosubmit       *bool `json:"autosubmit"`
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
	if req.Autotest != nil {
		h.SetAutotest(*req.Autotest)
	}
	// Re-trigger auto-test scan in case autotest was just enabled.
	if h.AutotestEnabled() {
		go h.tryAutoTest(r.Context())
	}
	if req.Autosubmit != nil {
		h.SetAutosubmit(*req.Autosubmit)
	}
	// Re-trigger auto-submit scan in case autosubmit was just enabled.
	if h.AutosubmitEnabled() {
		go h.tryAutoSubmit(r.Context())
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
		"autotest":          h.AutotestEnabled(),
		"autosubmit":        h.AutosubmitEnabled(),
		"ideation":          h.IdeationEnabled(),
		"ideation_running":  h.ideationRunning(r.Context()),
		"ideation_interval": int(h.IdeationInterval().Minutes()),
	}
	if nextRun := h.IdeationNextRun(); !nextRun.IsZero() {
		resp["ideation_next_run"] = nextRun
	}
	writeJSON(w, http.StatusOK, resp)
}
