package handler

import (
	"net/http"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/oauth"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// providerByName maps URL path values to OAuth provider configs.
var providerByName = map[string]oauth.Provider{
	"claude": oauth.ClaudeProvider,
	"codex":  oauth.CodexProvider,
}

// StartOAuth begins an OAuth authorization flow for the given provider.
func (h *Handler) StartOAuth(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	provider, ok := providerByName[name]
	if !ok {
		httpjson.Write(w, http.StatusBadRequest, map[string]string{"error": "unknown provider: " + name})
		return
	}

	authorizeURL, err := h.oauthManager.Start(r.Context(), provider)
	if err != nil {
		httpjson.Write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	httpjson.Write(w, http.StatusOK, map[string]string{"authorize_url": authorizeURL})
}

// OAuthStatus returns the current status of the OAuth flow for a provider.
func (h *Handler) OAuthStatus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	status := h.oauthManager.Status(name)
	httpjson.Write(w, http.StatusOK, status)
}

// CancelOAuth aborts an in-progress OAuth flow for a provider.
func (h *Handler) CancelOAuth(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	h.oauthManager.Cancel(name)
	w.WriteHeader(http.StatusNoContent)
}

// newOAuthTokenWriter returns a TokenWriter that persists tokens to the env file.
func newOAuthTokenWriter(envFile string) oauth.TokenWriter {
	return func(envKey, token string) error {
		var u envconfig.Updates
		switch envKey {
		case "CLAUDE_CODE_OAUTH_TOKEN":
			u.OAuthToken = &token
		case "OPENAI_API_KEY":
			u.OpenAIAPIKey = &token
		default:
			// Unknown key — write via OAuthToken as a generic fallback.
			u.OAuthToken = &token
		}
		return envconfig.Update(envFile, u)
	}
}
