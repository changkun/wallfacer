package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"changkun.de/wallfacer/internal/envconfig"
)

// envConfigResponse is the JSON representation of the env config sent to the UI.
// Sensitive tokens are masked so they are never exposed in full over HTTP.
type envConfigResponse struct {
	OAuthToken       string `json:"oauth_token"`        // masked
	APIKey           string `json:"api_key"`             // masked
	BaseURL          string `json:"base_url"`
	DefaultModel     string `json:"default_model"`
	TitleModel       string `json:"title_model"`
	MaxParallelTasks int    `json:"max_parallel_tasks"`
}

// GetEnvConfig returns the current env configuration with tokens masked.
func (h *Handler) GetEnvConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil {
		http.Error(w, "failed to read env file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	maxParallel := cfg.MaxParallelTasks
	if maxParallel <= 0 {
		maxParallel = defaultMaxConcurrentTasks
	}
	writeJSON(w, http.StatusOK, envConfigResponse{
		OAuthToken:       envconfig.MaskToken(cfg.OAuthToken),
		APIKey:           envconfig.MaskToken(cfg.APIKey),
		BaseURL:          cfg.BaseURL,
		DefaultModel:     cfg.DefaultModel,
		TitleModel:       cfg.TitleModel,
		MaxParallelTasks: maxParallel,
	})
}

// UpdateEnvConfig writes changes to the env file.
//
// Pointer semantics per field:
//   - field absent from JSON body (null) → leave unchanged
//   - field present with a value          → update
//   - field present with ""               → clear (for non-secret fields)
//
// For the two token fields (oauth_token, api_key), an empty value is treated
// as "no change" to prevent accidental token deletion.
func (h *Handler) UpdateEnvConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OAuthToken       *string `json:"oauth_token"`
		APIKey           *string `json:"api_key"`
		BaseURL          *string `json:"base_url"`
		DefaultModel     *string `json:"default_model"`
		TitleModel       *string `json:"title_model"`
		MaxParallelTasks *int    `json:"max_parallel_tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Guard: treat empty-string tokens as "no change" to avoid accidental clears.
	if req.OAuthToken != nil && *req.OAuthToken == "" {
		req.OAuthToken = nil
	}
	if req.APIKey != nil && *req.APIKey == "" {
		req.APIKey = nil
	}

	// Convert max_parallel_tasks int to string for the env file.
	var maxParallel *string
	if req.MaxParallelTasks != nil {
		v := *req.MaxParallelTasks
		if v < 1 {
			v = 1
		}
		s := fmt.Sprintf("%d", v)
		maxParallel = &s
	}

	if err := envconfig.Update(h.envFile, req.OAuthToken, req.APIKey, req.BaseURL, req.DefaultModel, req.TitleModel, maxParallel); err != nil {
		http.Error(w, "failed to update env file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
