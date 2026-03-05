package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/instructions"
	"changkun.de/wallfacer/internal/logger"
)

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

	client := &http.Client{Timeout: 5 * time.Second}
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

// GetConfig returns the server configuration (workspaces, instructions path).
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	// Read the current default model from the env file.
	defaultModel := ""
	var models []string
	if h.envFile != "" {
		if cfg, err := envconfig.Parse(h.envFile); err == nil {
			defaultModel = cfg.Model
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

	writeJSON(w, http.StatusOK, map[string]any{
		"workspaces":        h.runner.Workspaces(),
		"instructions_path": instructions.FilePath(h.configDir, h.workspaces),
		"autopilot":         h.AutopilotEnabled(),
		"models":            models,
		"default_model":     defaultModel,
	})
}

// UpdateConfig handles PUT /api/config to update server-level settings.
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Autopilot *bool `json:"autopilot"`
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
	writeJSON(w, http.StatusOK, map[string]any{
		"autopilot": h.AutopilotEnabled(),
	})
}
