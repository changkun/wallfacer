package handler

import (
	"context"
	"net/http"
	"os"
	"strings"

	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/prompts"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/workspace"
)

// availableSandboxes returns all sandbox types the UI should display,
// combining the built-in (registered) harnesses with any user-configured
// default or per-activity sandbox overrides.
func availableSandboxes(cfg envconfig.Config) []harness.ID {
	sandboxSet := map[harness.ID]bool{}
	var sandboxes []harness.ID
	add := func(name harness.ID) {
		name = harness.NormalizeID(strings.TrimSpace(string(name)))
		if name == "" || sandboxSet[name] {
			return
		}
		sandboxSet[name] = true
		sandboxes = append(sandboxes, name)
	}
	// Always expose every registered harness in the UI so users can select
	// any provider even before model/env values are configured. Driving this
	// from the registry means a newly registered harness reaches the UI
	// without editing this list.
	for _, id := range harness.All() {
		add(id)
	}

	if cfg.DefaultSandbox != "" {
		add(cfg.DefaultSandbox)
	}
	for _, v := range cfg.SandboxByActivity() {
		add(v)
	}
	return sandboxes
}

// allSandboxesUsable returns a usability map seeded true for every
// registered harness. Per-harness credential checks downgrade entries
// afterwards. Built from the registry so a new harness is included
// automatically.
func allSandboxesUsable() map[harness.ID]bool {
	usable := map[harness.ID]bool{}
	for _, id := range harness.All() {
		usable[id] = true
	}
	return usable
}

// defaultSandbox determines which sandbox type should be pre-selected for new
// tasks. Priority: explicit DefaultSandbox > Claude (if DefaultModel set) >
// Codex (if CodexDefaultModel set) > Claude as fallback.
func defaultSandbox(cfg envconfig.Config) harness.ID {
	if cfg.DefaultSandbox != "" {
		return cfg.DefaultSandbox
	}
	if cfg.DefaultModel != "" {
		return harness.Claude
	}
	if cfg.CodexDefaultModel != "" {
		return harness.Codex
	}
	return harness.Claude
}

// workspaceVisible reports whether the active workspace set (by its group key)
// is present in the principal-visible group list. Used to drop a cross-org
// active workspace from the config so an org switch doesn't carry the previous
// org's board over.
func workspaceVisible(groups []workspace.Group, workspaces []string) bool {
	key := workspace.GroupKey(workspaces)
	for _, g := range groups {
		if workspace.GroupKey(g.Workspaces) == key {
			return true
		}
	}
	return false
}

// workspaceVisibleTo reports whether the active workspace set is visible to the
// request's principal. Local mode (no principal) always passes. Mirrors the
// org/personal isolation buildConfigResponse applies, so every browser-facing
// surface agrees on whether a workspace is active for this caller.
func (h *Handler) workspaceVisibleTo(ctx context.Context, workspaces []string) bool {
	if len(workspaces) == 0 {
		return false
	}
	// Org/personal isolation only applies to multi-tenant cloud deployments.
	// A local single-user run must never hide the user's own workspace just
	// because their session carries a different org label than the one that
	// first stamped the group.
	if !h.cloudMode {
		return true
	}
	c, ok := auth.PrincipalFromContext(ctx)
	if !ok || c == nil {
		return true
	}
	groups, _ := workspace.LoadGroups(h.configDir)
	groups = workspace.GroupsForPrincipal(groups, &workspace.Principal{Sub: c.Sub, OrgID: c.OrgID})
	return workspaceVisible(groups, workspaces)
}

// visibleWorkspaces returns the active workspaces, or nil when the active
// workspace group is hidden from the request's principal. Browser-facing
// read handlers (files, specs, plan, terminal) use this instead of
// currentWorkspaces() so an org-scoped workspace does not leak into a
// session that config.go reports as having "no workspace".
func (h *Handler) visibleWorkspaces(ctx context.Context) []string {
	workspaces := h.currentWorkspaces()
	if h.workspaceVisibleTo(ctx, workspaces) {
		return workspaces
	}
	return nil
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

	groups, _ := workspace.LoadGroups(h.configDir)
	// Org / personal filtering: cloud-mode callers see only groups their
	// principal is allowed to see. Local single-user runs (cloudMode=false)
	// show every group regardless of session org, so switching org labels
	// never hides the user's own workspaces. We resolve the principal directly
	// from ctx since buildConfigResponse doesn't take *Request.
	if c, ok := auth.PrincipalFromContext(ctx); h.cloudMode && ok && c != nil {
		groups = workspace.GroupsForPrincipal(groups, &workspace.Principal{Sub: c.Sub, OrgID: c.OrgID})
		// The active workspace is global server state. After an org switch it
		// may still point at the previous org's group; if this principal can't
		// see that group, don't present it as active. They get their org's
		// groups (or the picker), a clean per-org view rather than the prior
		// org's board carried across the switch.
		if len(workspaces) > 0 && !workspaceVisible(groups, workspaces) {
			workspaces = nil
		}
	}

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
	// them against activeGroupInfos entries. Per-group concurrency overrides
	// ride along so the In-Progress column editor can show the current value.
	type keyedGroup struct {
		Name            string   `json:"name,omitempty"`
		Workspaces      []string `json:"workspaces"`
		Key             string   `json:"key"`
		MaxParallel     *int     `json:"max_parallel,omitempty"`
		MaxTestParallel *int     `json:"max_test_parallel,omitempty"`
	}
	keyedGroups := make([]keyedGroup, len(groups))
	for i, g := range groups {
		keyedGroups[i] = keyedGroup{
			Name:            g.Name,
			Workspaces:      g.Workspaces,
			Key:             prompts.InstructionsKey(g.Workspaces),
			MaxParallel:     g.MaxParallel,
			MaxTestParallel: g.MaxTestParallel,
		}
	}

	watcherNames := []string{"auto-promote", "auto-retry", "auto-test", "auto-submit", "auto-sync"}
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
		"prompts_dir":              promptsDir,
		"sandbox_activities":       store.SandboxActivities,
		"sandboxes":                harness.All(),
		"default_sandbox":          harness.Claude,
		"sandbox_usable":           allSandboxesUsable(),
		"sandbox_reasons":          map[string]string{},
		"activity_sandboxes":       map[string]string{},
		"autopilot":                h.AutopilotEnabled(),
		"autotest":                 h.AutotestEnabled(),
		"autosubmit":               h.AutosubmitEnabled(),
		"autosync":                 h.AutosyncEnabled(),
		"autopush":                 h.AutopushEnabled(),
		"ideation_exploit_ratio":   h.IdeationExploitRatio(),
		"ideation_categories":      h.runner.IdeationCategories(),
		"ideation_ignore_patterns": h.runner.IdeationIgnorePatterns(),
		"default_model":            "",
		"payload_limits":           payloadLimits,
		"watcher_health":           watcherHealth,
		"active_groups":            h.activeGroupInfos(ctx),
		"terminal_enabled":         true,
		"agent_session_window_days":     30,
		"auth_enabled":             h.auth != nil,
	}
	if h.authURL != "" {
		resp["auth_url"] = h.authURL
	}
	if cfg == nil {
		return resp
	}

	sandboxes := availableSandboxes(*cfg)
	sandboxUsable := allSandboxesUsable()
	sandboxReasons := map[string]string{}

	// Credential checks.
	for _, sbox := range sandboxes {
		if !sandboxUsable[sbox] {
			continue
		}
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
	resp["agent_session_window_days"] = cfg.AgentSessionWindowDays
	return resp
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
		Autotest             *bool             `json:"autotest"`
		Autosubmit           *bool             `json:"autosubmit"`
		Autosync             *bool             `json:"autosync"`
		Autopush             *bool             `json:"autopush"`
		Ideation             *bool             `json:"ideation"`               // retired; accepted for old clients but ignored
		IdeationInterval     *int              `json:"ideation_interval"`      // retired; accepted for old clients but ignored
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
		h.reloadGroupLimits()
	}
	// Background passes outlive the HTTP request, so they must run on a
	// detached context: r.Context() is cancelled the moment UpdateConfig
	// returns (mirrors the planning exec goroutine).
	applyBoolToggle(context.Background(), req.Autopilot, h.SetAutopilot, h.AutopilotEnabled, h.tryAutoPromote)
	applyBoolToggle(context.Background(), req.Autotest, h.SetAutotest, h.AutotestEnabled, h.tryAutoTest)
	applyBoolToggle(context.Background(), req.Autosubmit, h.SetAutosubmit, h.AutosubmitEnabled, h.tryAutoSubmit)
	applyBoolToggle(context.Background(), req.Autosync, h.SetAutosync, h.AutosyncEnabled, h.checkAndSyncWaitingTasks)
	// Persist any toggle changes to the viewed group so switching away
	// and back does not reset the user's automation choices, and a
	// different group on this server stays manual unless the user turns
	// automation on for it explicitly.
	if req.Autopilot != nil || req.Autotest != nil ||
		req.Autosubmit != nil || req.Autosync != nil {
		h.persistCurrentGroupToggles()
	}
	// Auto-push: update both the in-memory toggle and the .env file so the
	// runner (which reads .env on every commit) picks it up immediately.
	if req.Autopush != nil {
		h.SetAutopush(*req.Autopush)
		if h.envFile != "" {
			v := "false"
			if *req.Autopush {
				v = "true"
			}
			if err := envconfig.Update(h.envFile, envconfig.Updates{AutoPush: &v}); err != nil {
				logger.Handler.Warn("config: failed to persist autopush setting to .env", "error", err)
			}
		}
	}
	if req.IdeationExploitRatio != nil {
		h.SetIdeationExploitRatio(*req.IdeationExploitRatio)
	}
	resp := map[string]any{
		"autopilot":              h.AutopilotEnabled(),
		"autotest":               h.AutotestEnabled(),
		"autosubmit":             h.AutosubmitEnabled(),
		"autosync":               h.AutosyncEnabled(),
		"autopush":               h.AutopushEnabled(),
		"ideation_exploit_ratio": h.IdeationExploitRatio(),
		"ideation_categories":    h.runner.IdeationCategories(),
	}
	httpjson.Write(w, http.StatusOK, resp)
}

// applyBoolToggle applies a tri-state bool field from a config update: when
// reqVal is non-nil it stores the value, and if the feature is then enabled it
// kicks an immediate background pass via onEnable. ctx must be detached from
// the HTTP request because the pass outlives the request; callers pass
// context.Background().
func applyBoolToggle(ctx context.Context, reqVal *bool, set func(bool), enabled func() bool, onEnable func(context.Context)) {
	if reqVal == nil {
		return
	}
	set(*reqVal)
	if enabled() {
		go onEnable(ctx)
	}
}
