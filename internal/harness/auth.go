package harness

// AuthConfig is the flat credentials bundle passed to every harness's
// AuthEnv method. Fields are named per credential, not per harness,
// so multiple harnesses sharing a key (e.g. ANTHROPIC_API_KEY) read
// from the same field.
type AuthConfig struct {
	// Anthropic — used by Claude.
	AnthropicAPIKey  string
	ClaudeOAuthToken string

	// OpenAI — used by Codex.
	OpenAIAPIKey  string
	CodexAuthFile string

	// Cursor.
	CursorAPIKey string

	// OpenCode — server-mode for warm-start; provider auth is managed
	// by the opencode CLI itself.
	OpenCodeServerURL      string
	OpenCodeServerPassword string

	// Pi — reserved for a future Pi-specific subscription provider;
	// per-provider keys (ANTHROPIC_API_KEY etc.) are read by the Pi
	// harness directly from the host environment.
	PiAPIKey string
}
