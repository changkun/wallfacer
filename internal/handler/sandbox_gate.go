package handler

import (
	"errors"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// normalizeSandbox maps an arbitrary string to a canonical sandbox.Type,
// applying the same default logic as sandbox.Default (empty → Claude).
func normalizeSandbox(s string) sandbox.Type {
	return sandbox.Default(s)
}

// sandboxUsable reports whether the given sandbox type can accept tasks.
// For Claude, it is always usable. For Codex, the check follows a priority
// chain: host auth (~/.codex/auth.json) > OPENAI_API_KEY > prior test pass.
// Returns (usable, reason) where reason explains why it is not usable.
func (h *Handler) sandboxUsable(sb sandbox.Type) (bool, string) {
	s := sb.OrDefault()
	// Claude sandbox is always usable (uses local OAuth or API key from env).
	if s != sandbox.Codex {
		return true, ""
	}
	// Check 1: host-level Codex auth cache (highest priority — no API key needed).
	hasHostAuth := false
	hostAuthReason := ""
	if h.runner != nil {
		hasHostAuth, hostAuthReason = h.runner.HostCodexAuthStatus(time.Now())
	}
	if hasHostAuth {
		return true, ""
	}
	// Check 2: explicit OPENAI_API_KEY in the env file.
	hasAPIKey := false
	if h.envFile != "" {
		cfg, err := envconfig.Parse(h.envFile)
		if err != nil {
			return false, "Codex unavailable: failed to read env configuration."
		}
		hasAPIKey = strings.TrimSpace(cfg.OpenAIAPIKey) != ""
	}
	if !hasAPIKey {
		reason := "Codex unavailable: configure OPENAI_API_KEY or sign in via host Codex auth cache (~/.codex/auth.json)."
		if strings.TrimSpace(hostAuthReason) != "" {
			reason += " Host auth status: " + hostAuthReason + "."
		}
		return false, reason
	}
	// Check 3: API key present but not yet validated — require a smoke test first.
	if !h.sandboxTestPassedState(sandbox.Codex) {
		return false, "Codex unavailable: run Settings -> API Configuration -> Test (Codex) first."
	}
	return true, ""
}

// validateRequestedSandboxes checks that both the task-level sandbox and all
// per-activity sandbox overrides are usable. Returns the first failure reason.
func (h *Handler) validateRequestedSandboxes(taskSandbox sandbox.Type, byActivity map[store.SandboxActivity]sandbox.Type) error {
	if ok, reason := h.sandboxUsable(taskSandbox); !ok {
		return errors.New(reason)
	}
	for _, sandbox := range byActivity {
		if ok, reason := h.sandboxUsable(sandbox); !ok {
			return errors.New(reason)
		}
	}
	return nil
}
