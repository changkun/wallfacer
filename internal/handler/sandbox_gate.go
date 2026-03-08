package handler

import (
	"errors"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
)

func normalizeSandbox(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "claude"
	}
	return s
}

func (h *Handler) sandboxUsable(sandbox string) (bool, string) {
	s := normalizeSandbox(sandbox)
	if s != "codex" {
		return true, ""
	}
	hasHostAuth := false
	hostAuthReason := ""
	if h.runner != nil {
		hasHostAuth, hostAuthReason = h.runner.HostCodexAuthStatus(time.Now())
	}
	if hasHostAuth {
		return true, ""
	}
	hasAPIKey := false
	if h.envFile != "" {
		cfg, err := envconfig.Parse(h.envFile)
		if err != nil {
			return false, "Codex unavailable: failed to read env configuration."
		} else {
			hasAPIKey = strings.TrimSpace(cfg.OpenAIAPIKey) != ""
		}
	}
	if !hasAPIKey {
		reason := "Codex unavailable: configure OPENAI_API_KEY or sign in via host Codex auth cache (~/.codex/auth.json)."
		if strings.TrimSpace(hostAuthReason) != "" {
			reason += " Host auth status: " + hostAuthReason + "."
		}
		return false, reason
	}
	if !h.sandboxTestPassedState("codex") {
		return false, "Codex unavailable: run Settings -> API Configuration -> Test (Codex) first."
	}
	return true, ""
}

func (h *Handler) validateRequestedSandboxes(taskSandbox string, byActivity map[string]string) error {
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
