package handler

import (
	"errors"
	"strings"

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
	if h.envFile == "" {
		return false, "Codex unavailable: env file is not configured."
	}
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil {
		return false, "Codex unavailable: failed to read env configuration."
	}
	if strings.TrimSpace(cfg.OpenAIAPIKey) == "" {
		return false, "Codex unavailable: OPENAI_API_KEY is not configured."
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
