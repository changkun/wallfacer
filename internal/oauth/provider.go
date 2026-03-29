package oauth

// Provider holds the OAuth 2.0 configuration for an identity provider.
type Provider struct {
	Name         string   // display name (e.g. "claude", "codex")
	AuthorizeURL string   // authorization endpoint
	TokenURL     string   // token exchange endpoint
	ClientID     string   // OAuth client ID
	Scopes       []string // requested scopes (empty if none needed)
	TokenEnvKey  string   // env var name to write the token to (e.g. "CLAUDE_CODE_OAUTH_TOKEN")
}

// ClaudeProvider is the OAuth configuration for Claude Code.
var ClaudeProvider = Provider{
	Name:         "claude",
	AuthorizeURL: "https://platform.claude.com/oauth/authorize",
	TokenURL:     "https://platform.claude.com/v1/oauth/token",
	ClientID:     "https://claude.ai/oauth/claude-code-client-metadata",
	TokenEnvKey:  "CLAUDE_CODE_OAUTH_TOKEN",
}

// CodexProvider is the OAuth configuration for OpenAI Codex.
var CodexProvider = Provider{
	Name:         "codex",
	AuthorizeURL: "https://auth.openai.com/oauth/authorize",
	TokenURL:     "https://auth.openai.com/oauth/token",
	ClientID:     "app_EMoamEEZ73f0CkXaXp7hrann",
	Scopes:       []string{"openid", "profile", "email", "offline_access"},
	TokenEnvKey:  "OPENAI_API_KEY",
}
