package handler

import (
	"errors"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/store"
)

func normalizeSandbox(s string) constants.SandboxType {
	return constants.DefaultSandboxType(s)
}

func (h *Handler) sandboxUsable(sb constants.SandboxType) (bool, string) {
	s := sb.OrDefault()
	if s != constants.SandboxCodex {
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
	if !h.sandboxTestPassedState(constants.SandboxCodex) {
		return false, "Codex unavailable: run Settings -> API Configuration -> Test (Codex) first."
	}
	return true, ""
}

func (h *Handler) validateRequestedSandboxes(taskSandbox constants.SandboxType, byActivity map[store.SandboxActivity]constants.SandboxType) error {
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
