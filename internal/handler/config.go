package handler

import (
	"context"
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

func (h *Handler) buildConfigResponse(ctx context.Context, cfg *envconfig.Config) map[string]any {
	promptsDir := h.runner.Prompts().PromptsDir()
	resp := map[string]any{
		"workspaces":          h.runner.Workspaces(),
		"instructions_path":   instructions.FilePath(h.configDir, h.workspaces),
		"prompts_dir":         promptsDir,
		"sandbox_activities":  store.SandboxActivities,
		"sandboxes":           []string{"claude", "codex"},
		"default_sandbox":     "claude",
		"sandbox_usable":      map[string]bool{"claude": true, "codex": true},
		"sandbox_reasons":     map[string]string{},
		"activity_sandboxes":  map[string]string{},
		"autopilot":           h.AutopilotEnabled(),
		"autotest":            h.AutotestEnabled(),
		"autosubmit":          h.AutosubmitEnabled(),
		"ideation":            h.IdeationEnabled(),
		"ideation_running":    h.ideationRunning(ctx),
		"ideation_interval":   int(h.IdeationInterval().Minutes()),
		"ideation_categories": h.runner.IdeationCategories(),
		"default_model":       "",
		"payload_limits":      h.store.GetPayloadLimits(),
	}
	if nextRun := h.IdeationNextRun(); !nextRun.IsZero() {
		resp["ideation_next_run"] = nextRun
	}
	if cfg == nil {
		return resp
	}

	sandboxes := availableSandboxes(*cfg)
	sandboxUsable := map[string]bool{
		"claude": true,
		"codex":  true,
	}
	sandboxReasons := map[string]string{}
	for _, sbox := range sandboxes {
		ok, reason := h.sandboxUsable(sbox)
		sandboxUsable[sbox] = ok
		if reason != "" {
			sandboxReasons[sbox] = reason
		}
	}

	resp["sandboxes"] = sandboxes
	resp["default_sandbox"] = defaultSandbox(*cfg)
	resp["sandbox_usable"] = sandboxUsable
	resp["sandbox_reasons"] = sandboxReasons
	resp["activity_sandboxes"] = cfg.SandboxByActivity()
	resp["default_model"] = cfg.DefaultModel
	return resp
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
	var cfg *envconfig.Config
	if h.envFile != "" {
		if parsed, err := envconfig.Parse(h.envFile); err == nil {
			cfg = &parsed
		}
	}
	writeJSON(w, http.StatusOK, h.buildConfigResponse(r.Context(), cfg))
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
	if !decodeJSONBody(w, r, &req) {
		return
	}
	applyBoolToggle := func(reqVal *bool, set func(bool), enabled func() bool, onEnable func(context.Context)) {
		if reqVal == nil {
			return
		}
		set(*reqVal)
		if enabled() {
			go onEnable(r.Context())
		}
	}
	applyBoolToggle(req.Autopilot, h.SetAutopilot, h.AutopilotEnabled, h.tryAutoPromote)
	applyBoolToggle(req.Autotest, h.SetAutotest, h.AutotestEnabled, h.tryAutoTest)
	applyBoolToggle(req.Autosubmit, h.SetAutosubmit, h.AutosubmitEnabled, h.tryAutoSubmit)
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
		"autopilot":           h.AutopilotEnabled(),
		"autotest":            h.AutotestEnabled(),
		"autosubmit":          h.AutosubmitEnabled(),
		"ideation":            h.IdeationEnabled(),
		"ideation_running":    h.ideationRunning(r.Context()),
		"ideation_interval":   int(h.IdeationInterval().Minutes()),
		"ideation_categories": h.runner.IdeationCategories(),
	}
	if nextRun := h.IdeationNextRun(); !nextRun.IsZero() {
		resp["ideation_next_run"] = nextRun
	}
	writeJSON(w, http.StatusOK, resp)
}
