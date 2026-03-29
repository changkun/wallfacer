package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
)

// ssrfHardenedTransport returns an http.Transport that re-checks the resolved
// IP address against private/loopback/link-local ranges immediately before
// opening the TCP connection. This is defense-in-depth against DNS-rebinding
// attacks: even if validateBaseURL approved the hostname at configuration time,
// a subsequent DNS change could point it to a private IP. By re-resolving and
// checking at connect time, the attack window is closed.
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

// availableSandboxes returns all sandbox types the UI should display,
// combining the built-in Claude and Codex sandboxes with any user-configured
// default or per-activity sandbox overrides.
func availableSandboxes(cfg envconfig.Config) []sandbox.Type {
	sandboxSet := map[sandbox.Type]bool{}
	var sandboxes []sandbox.Type
	add := func(name sandbox.Type) {
		name = sandbox.Normalize(strings.TrimSpace(string(name)))
		if name == "" || sandboxSet[name] {
			return
		}
		sandboxSet[name] = true
		sandboxes = append(sandboxes, name)
	}
	// Always expose both built-in sandboxes in the UI so users can select
	// either provider even before model/env values are configured.
	add(sandbox.Claude)
	add(sandbox.Codex)

	if cfg.DefaultSandbox != "" {
		add(cfg.DefaultSandbox)
	}
	for _, v := range cfg.SandboxByActivity() {
		add(v)
	}
	return sandboxes
}

// defaultSandbox determines which sandbox type should be pre-selected for new
// tasks. Priority: explicit DefaultSandbox > Claude (if DefaultModel set) >
// Codex (if CodexDefaultModel set) > Claude as fallback.
func defaultSandbox(cfg envconfig.Config) sandbox.Type {
	if cfg.DefaultSandbox != "" {
		return cfg.DefaultSandbox
	}
	if cfg.DefaultModel != "" {
		return sandbox.Claude
	}
	if cfg.CodexDefaultModel != "" {
		return sandbox.Codex
	}
	return sandbox.Claude
}

// buildConfigResponse assembles the full configuration payload returned by
// GET /api/config and reused by UpdateWorkspaces after a workspace switch.
// When cfg is nil (env file not readable), sandbox-related fields use safe defaults.
// activeGroupInfo describes a workspace group with open stores,
// including per-status task counts for the frontend to display.
type activeGroupInfo struct {
	Key        string `json:"key"`
	InProgress int    `json:"in_progress"`
	Waiting    int    `json:"waiting"`
}

// activeGroupInfos returns per-group task counts for all active workspace
// groups (the viewed group plus any groups with running tasks).
func (h *Handler) activeGroupInfos(ctx context.Context) []activeGroupInfo {
	var infos []activeGroupInfo
	if h.workspace == nil {
		return infos
	}
	for _, snap := range h.workspace.AllActiveSnapshots() {
		info := activeGroupInfo{Key: snap.Key}
		if snap.Store != nil {
			if tasks, err := snap.Store.ListTasks(ctx, false); err == nil {
				for _, t := range tasks {
					switch t.Status {
					case store.TaskStatusInProgress, store.TaskStatusCommitting:
						info.InProgress++
					case store.TaskStatusWaiting:
						info.Waiting++
					}
				}
			}
		}
		infos = append(infos, info)
	}
	return infos
}

func (h *Handler) buildConfigResponse(ctx context.Context, cfg *envconfig.Config) map[string]any {
	promptsDir := h.runner.Prompts().PromptsDir()
	workspaces := h.currentWorkspaces()
	instructionsPath := h.currentInstructionsPath()
	workspaceBrowserPath := ""
	if len(workspaces) > 0 {
		workspaceBrowserPath = workspaces[0]
	} else if cwd, err := os.Getwd(); err == nil {
		workspaceBrowserPath = cwd
	}
	payloadLimits := store.PayloadLimits{}
	if s, ok := h.currentStore(); ok && s != nil {
		payloadLimits = s.GetPayloadLimits()
	}
	groups, _ := workspace.LoadGroups(h.configDir)
	if len(workspaces) > 0 {
		key := workspace.GroupKey(workspaces)
		found := false
		for i, g := range groups {
			if workspace.GroupKey(g.Workspaces) == key {
				// Promote existing group to front, preserving its Name.
				promoted := g
				promoted.Workspaces = workspaces
				groups = append([]workspace.Group{promoted}, append(groups[:i], groups[i+1:]...)...)
				found = true
				break
			}
		}
		if !found {
			groups = append([]workspace.Group{{Workspaces: workspaces}}, groups...)
		}
		groups = workspace.NormalizeGroups(groups)
	}
	// Enrich groups with their deterministic keys so the frontend can match
	// them against activeGroupInfos entries.
	type keyedGroup struct {
		Name       string   `json:"name,omitempty"`
		Workspaces []string `json:"workspaces"`
		Key        string   `json:"key"`
	}
	keyedGroups := make([]keyedGroup, len(groups))
	for i, g := range groups {
		keyedGroups[i] = keyedGroup{
			Name:       g.Name,
			Workspaces: g.Workspaces,
			Key:        prompts.InstructionsKey(g.Workspaces),
		}
	}

	watcherNames := []string{"auto-promote", "auto-retry", "auto-test", "auto-submit", "auto-sync", "auto-refine"}
	watcherHealth := make([]watcherHealthEntry, 0, len(watcherNames))
	for _, name := range watcherNames {
		if wb, ok := h.breakers[name]; ok {
			watcherHealth = append(watcherHealth, wb.healthEntry(name))
		}
	}

	resp := map[string]any{
		"workspaces":               workspaces,
		"workspace_browser_path":   workspaceBrowserPath,
		"workspace_groups":         keyedGroups,
		"instructions_path":        instructionsPath,
		"prompts_dir":              promptsDir,
		"sandbox_activities":       store.SandboxActivities,
		"sandboxes":                []sandbox.Type{sandbox.Claude, sandbox.Codex},
		"default_sandbox":          sandbox.Claude,
		"sandbox_usable":           map[sandbox.Type]bool{sandbox.Claude: true, sandbox.Codex: true},
		"sandbox_reasons":          map[string]string{},
		"activity_sandboxes":       map[string]string{},
		"autopilot":                h.AutopilotEnabled(),
		"autorefine":               h.AutorefineEnabled(),
		"autotest":                 h.AutotestEnabled(),
		"autosubmit":               h.AutosubmitEnabled(),
		"autosync":                 h.AutosyncEnabled(),
		"autopush":                 h.AutopushEnabled(),
		"ideation":                 h.IdeationEnabled(),
		"ideation_running":         h.ideationRunning(ctx),
		"ideation_interval":        int(h.IdeationInterval().Minutes()),
		"ideation_exploit_ratio":   h.IdeationExploitRatio(),
		"ideation_categories":      h.runner.IdeationCategories(),
		"ideation_ignore_patterns": h.runner.IdeationIgnorePatterns(),
		"default_model":            "",
		"payload_limits":           payloadLimits,
		"watcher_health":           watcherHealth,
		"active_groups":            h.activeGroupInfos(ctx),
		"terminal_enabled":         true,
	}
	if nextRun := h.IdeationNextRun(); !nextRun.IsZero() {
		resp["ideation_next_run"] = nextRun
	}
	if cfg == nil {
		return resp
	}

	sandboxes := availableSandboxes(*cfg)
	sandboxUsable := map[sandbox.Type]bool{
		sandbox.Claude: true,
		sandbox.Codex:  true,
	}
	sandboxReasons := map[string]string{}
	for _, sbox := range sandboxes {
		ok, reason := h.sandboxUsable(sbox)
		sandboxUsable[sbox] = ok
		if reason != "" {
			sandboxReasons[string(sbox)] = reason
		}
	}

	resp["sandboxes"] = sandboxes
	resp["default_sandbox"] = defaultSandbox(*cfg)
	resp["sandbox_usable"] = sandboxUsable
	resp["sandbox_reasons"] = sandboxReasons
	resp["activity_sandboxes"] = cfg.SandboxByActivity()
	resp["default_model"] = cfg.DefaultModel
	resp["terminal_enabled"] = cfg.TerminalEnabled
	return resp
}

// ideationRunning returns true if any idea-agent task is currently in_progress.
func (h *Handler) ideationRunning(ctx context.Context) bool {
	s, ok := h.currentStore()
	if !ok || s == nil {
		return false
	}
	tasks, err := s.ListTasks(ctx, false)
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
	httpjson.Write(w, http.StatusOK, h.buildConfigResponse(r.Context(), cfg))
}

// UpdateConfig handles PUT /api/config to update server-level settings.
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		Autopilot            *bool             `json:"autopilot"`
		Autorefine           *bool             `json:"autorefine"`
		Autotest             *bool             `json:"autotest"`
		Autosubmit           *bool             `json:"autosubmit"`
		Autosync             *bool             `json:"autosync"`
		Autopush             *bool             `json:"autopush"`
		Ideation             *bool             `json:"ideation"`
		IdeationInterval     *int              `json:"ideation_interval"`      // minutes; 0 = run immediately on completion
		IdeationExploitRatio *float64          `json:"ideation_exploit_ratio"` // 0.0–1.0; fraction of exploitation ideas
		WorkspaceGroups      []workspace.Group `json:"workspace_groups"`
	}](w, r)
	if !ok {
		return
	}
	if req.WorkspaceGroups != nil {
		if err := workspace.SaveGroups(h.configDir, req.WorkspaceGroups); err != nil {
			http.Error(w, "save workspace groups: "+err.Error(), http.StatusInternalServerError)
			return
		}
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
	applyBoolToggle(req.Autorefine, h.SetAutorefine, h.AutorefineEnabled, h.tryAutoRefine)
	applyBoolToggle(req.Autotest, h.SetAutotest, h.AutotestEnabled, h.tryAutoTest)
	applyBoolToggle(req.Autosubmit, h.SetAutosubmit, h.AutosubmitEnabled, h.tryAutoSubmit)
	applyBoolToggle(req.Autosync, h.SetAutosync, h.AutosyncEnabled, h.checkAndSyncWaitingTasks)
	// Auto-push: update both the in-memory toggle and the .env file so the
	// runner (which reads .env on every commit) picks it up immediately.
	if req.Autopush != nil {
		h.SetAutopush(*req.Autopush)
		if h.envFile != "" {
			v := "false"
			if *req.Autopush {
				v = "true"
			}
			_ = envconfig.Update(h.envFile, envconfig.Updates{AutoPush: &v})
		}
	}
	if req.IdeationExploitRatio != nil {
		h.SetIdeationExploitRatio(*req.IdeationExploitRatio)
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
		"autopilot":              h.AutopilotEnabled(),
		"autorefine":             h.AutorefineEnabled(),
		"autotest":               h.AutotestEnabled(),
		"autosubmit":             h.AutosubmitEnabled(),
		"autosync":               h.AutosyncEnabled(),
		"autopush":               h.AutopushEnabled(),
		"ideation":               h.IdeationEnabled(),
		"ideation_running":       h.ideationRunning(r.Context()),
		"ideation_interval":      int(h.IdeationInterval().Minutes()),
		"ideation_exploit_ratio": h.IdeationExploitRatio(),
		"ideation_categories":    h.runner.IdeationCategories(),
	}
	if nextRun := h.IdeationNextRun(); !nextRun.IsZero() {
		resp["ideation_next_run"] = nextRun
	}
	httpjson.Write(w, http.StatusOK, resp)
}
