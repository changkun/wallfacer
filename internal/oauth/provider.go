package oauth

// Provider holds the OAuth 2.0 configuration for an identity provider.
type Provider struct {
	Name         string   // display name (e.g. "claude", "codex")
	AuthorizeURL string   // authorization endpoint
	TokenURL     string   // token exchange endpoint
	ClientID     string   // OAuth client ID
	Scopes       []string // requested scopes (empty if none needed)
	TokenEnvKey  string   // env var name to write the token to (e.g. "CLAUDE_CODE_OAUTH_TOKEN")
	FixedPort    int      // fixed callback port (0 = random); some providers require a specific port
	CallbackPath string   // callback URL path (default "/callback")
}

// ClaudeProvider is the OAuth configuration for Claude.
// Uses the same client ID and fixed port as the Pi coding agent
// (claude.ai OAuth with port 53692).
var ClaudeProvider = Provider{
	Name:         "claude",
	AuthorizeURL: "https://claude.ai/oauth/authorize",
	TokenURL:     "https://platform.claude.com/v1/oauth/token",
	ClientID:     "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
	Scopes:       []string{"org:create_api_key", "user:profile", "user:inference"},
	TokenEnvKey:  "CLAUDE_CODE_OAUTH_TOKEN",
	FixedPort:    53692,
}

// CodexProvider is the OAuth configuration for OpenAI Codex.
// Uses fixed port 1455 and path /auth/callback to match the redirect URI
// registered with OpenAI for the Codex CLI client ID.
var CodexProvider = Provider{
	Name:         "codex",
	AuthorizeURL: "https://auth.openai.com/oauth/authorize",
	TokenURL:     "https://auth.openai.com/oauth/token",
	ClientID:     "app_EMoamEEZ73f0CkXaXp7hrann",
	Scopes:       []string{"openid", "profile", "email", "offline_access"},
	TokenEnvKey:  "OPENAI_API_KEY",
	FixedPort:    1455,
	CallbackPath: "/auth/callback",
}
