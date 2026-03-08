// Package envconfig provides helpers for reading and updating the wallfacer
// .env file that is passed to task containers via --env-file.
package envconfig

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds the known configuration values from the .env file.
type Config struct {
	OAuthToken        string // CLAUDE_CODE_OAUTH_TOKEN
	APIKey            string // ANTHROPIC_API_KEY
	AuthToken         string // ANTHROPIC_AUTH_TOKEN (gateway proxy token)
	BaseURL           string // ANTHROPIC_BASE_URL
	DefaultModel      string // CLAUDE_DEFAULT_MODEL
	TitleModel        string // CLAUDE_TITLE_MODEL
	MaxParallelTasks  int    // WALLFACER_MAX_PARALLEL (0 means use default)
	OversightInterval int    // WALLFACER_OVERSIGHT_INTERVAL in minutes (0 = disabled)
	AutoPushEnabled   bool   // WALLFACER_AUTO_PUSH ("true"/"false")
	AutoPushThreshold int    // WALLFACER_AUTO_PUSH_THRESHOLD (0 means use default of 1)

	// OpenAI Codex sandbox fields.
	OpenAIAPIKey      string // OPENAI_API_KEY
	OpenAIBaseURL     string // OPENAI_BASE_URL
	CodexDefaultModel string // CODEX_DEFAULT_MODEL
	CodexTitleModel   string // CODEX_TITLE_MODEL

	DefaultSandbox                string // WALLFACER_DEFAULT_SANDBOX
	ImplementationSandbox         string // WALLFACER_SANDBOX_IMPLEMENTATION
	TestingSandbox                string // WALLFACER_SANDBOX_TESTING
	RefinementSandbox             string // WALLFACER_SANDBOX_REFINEMENT
	TitleSandbox                  string // WALLFACER_SANDBOX_TITLE
	OversightSandbox              string // WALLFACER_SANDBOX_OVERSIGHT
	CommitMessageSandbox          string // WALLFACER_SANDBOX_COMMIT_MESSAGE
	IdeaAgentSandbox              string // WALLFACER_SANDBOX_IDEA_AGENT
}

// knownKeys is the ordered list of keys managed by this package.
var knownKeys = []string{
	"CLAUDE_CODE_OAUTH_TOKEN",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_BASE_URL",
	"OPENAI_API_KEY",
	"OPENAI_BASE_URL",
	"CLAUDE_DEFAULT_MODEL",
	"CLAUDE_TITLE_MODEL",
	"CODEX_DEFAULT_MODEL",
	"CODEX_TITLE_MODEL",
	"WALLFACER_MAX_PARALLEL",
	"WALLFACER_OVERSIGHT_INTERVAL",
	"WALLFACER_AUTO_PUSH",
	"WALLFACER_AUTO_PUSH_THRESHOLD",
	"WALLFACER_DEFAULT_SANDBOX",
	"WALLFACER_SANDBOX_IMPLEMENTATION",
	"WALLFACER_SANDBOX_TESTING",
	"WALLFACER_SANDBOX_REFINEMENT",
	"WALLFACER_SANDBOX_TITLE",
	"WALLFACER_SANDBOX_OVERSIGHT",
	"WALLFACER_SANDBOX_COMMIT_MESSAGE",
	"WALLFACER_SANDBOX_IDEA_AGENT",
}

// Parse reads the env file at path and returns the known configuration values.
// Lines that are blank or start with "#" are ignored. Unknown keys are skipped.
func Parse(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		switch k {
		case "CLAUDE_CODE_OAUTH_TOKEN":
			cfg.OAuthToken = v
		case "ANTHROPIC_API_KEY":
			cfg.APIKey = v
		case "ANTHROPIC_AUTH_TOKEN":
			cfg.AuthToken = v
		case "ANTHROPIC_BASE_URL":
			cfg.BaseURL = v
		case "CLAUDE_DEFAULT_MODEL":
			cfg.DefaultModel = v
		case "CLAUDE_TITLE_MODEL":
			cfg.TitleModel = v
		case "WALLFACER_MAX_PARALLEL":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.MaxParallelTasks = n
			}
		case "WALLFACER_OVERSIGHT_INTERVAL":
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				cfg.OversightInterval = n
			}
		case "WALLFACER_AUTO_PUSH":
			cfg.AutoPushEnabled = v == "true"
		case "WALLFACER_AUTO_PUSH_THRESHOLD":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.AutoPushThreshold = n
			}
		case "OPENAI_API_KEY":
			cfg.OpenAIAPIKey = v
		case "OPENAI_BASE_URL":
			cfg.OpenAIBaseURL = v
		case "CODEX_DEFAULT_MODEL":
			cfg.CodexDefaultModel = v
		case "CODEX_TITLE_MODEL":
			cfg.CodexTitleModel = v
		case "WALLFACER_DEFAULT_SANDBOX":
			cfg.DefaultSandbox = strings.ToLower(strings.TrimSpace(v))
		case "WALLFACER_SANDBOX_IMPLEMENTATION":
			cfg.ImplementationSandbox = strings.ToLower(strings.TrimSpace(v))
		case "WALLFACER_SANDBOX_TESTING":
			cfg.TestingSandbox = strings.ToLower(strings.TrimSpace(v))
		case "WALLFACER_SANDBOX_REFINEMENT":
			cfg.RefinementSandbox = strings.ToLower(strings.TrimSpace(v))
		case "WALLFACER_SANDBOX_TITLE":
			cfg.TitleSandbox = strings.ToLower(strings.TrimSpace(v))
		case "WALLFACER_SANDBOX_OVERSIGHT":
			cfg.OversightSandbox = strings.ToLower(strings.TrimSpace(v))
		case "WALLFACER_SANDBOX_COMMIT_MESSAGE":
			cfg.CommitMessageSandbox = strings.ToLower(strings.TrimSpace(v))
		case "WALLFACER_SANDBOX_IDEA_AGENT":
			cfg.IdeaAgentSandbox = strings.ToLower(strings.TrimSpace(v))
		}
	}
	return cfg, nil
}

func (c Config) SandboxByActivity() map[string]string {
	out := map[string]string{}
	if c.ImplementationSandbox != "" {
		out["implementation"] = c.ImplementationSandbox
	}
	if c.TestingSandbox != "" {
		out["testing"] = c.TestingSandbox
	}
	if c.RefinementSandbox != "" {
		out["refinement"] = c.RefinementSandbox
	}
	if c.TitleSandbox != "" {
		out["title"] = c.TitleSandbox
	}
	if c.OversightSandbox != "" {
		out["oversight"] = c.OversightSandbox
	}
	if c.CommitMessageSandbox != "" {
		out["commit_message"] = c.CommitMessageSandbox
	}
	if c.IdeaAgentSandbox != "" {
		out["idea_agent"] = c.IdeaAgentSandbox
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseEnvLine parses a single .env line in a permissive way:
// - trims whitespace
// - ignores blank and comment-only lines
// - accepts leading "export " prefix
// - supports inline comments after quoted/unquoted values
// - preserves literal '#' inside quoted strings
func parseEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}

	k, v, hasEquals := strings.Cut(line, "=")
	if !hasEquals {
		return "", "", false
	}

	k = strings.TrimSpace(k)
	v = strings.TrimSpace(stripEnvInlineComment(v))
	return k, unquote(v), true
}

func stripEnvInlineComment(v string) string {
	inSingleQuote := false
	inDoubleQuote := false
	escapeNext := false

	for i := 0; i < len(v); i++ {
		c := v[i]

		if escapeNext {
			escapeNext = false
			continue
		}
		if c == '\\' && inDoubleQuote {
			escapeNext = true
			continue
		}

		switch c {
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
		case '#':
			if !inSingleQuote && !inDoubleQuote {
				return strings.TrimSpace(v[:i])
			}
		}
	}

	return strings.TrimSpace(v)
}

// Update merges changes into the env file at path.
//
// Each pointer field controls how the corresponding key is handled:
//   - nil → leave the existing line unchanged
//   - non-nil, non-empty → set to the provided value
//   - non-nil, empty → remove the line (clear the value)
//
// Keys not already present in the file are appended when non-empty.
// Comments and unrecognized keys are preserved verbatim.
func Update(
	path string,
	oauthToken,
	apiKey,
	baseURL,
	openAIAPIKey,
	openAIBaseURL,
	defaultModel,
	titleModel,
	codexDefaultModel,
	codexTitleModel,
	maxParallel,
	oversightInterval,
	autoPush,
	autoPushThreshold *string,
) error {
	updates := map[string]*string{
		"CLAUDE_CODE_OAUTH_TOKEN":       oauthToken,
		"ANTHROPIC_API_KEY":             apiKey,
		"ANTHROPIC_BASE_URL":            baseURL,
		"OPENAI_API_KEY":                openAIAPIKey,
		"OPENAI_BASE_URL":               openAIBaseURL,
		"CLAUDE_DEFAULT_MODEL":          defaultModel,
		"CLAUDE_TITLE_MODEL":            titleModel,
		"CODEX_DEFAULT_MODEL":           codexDefaultModel,
		"CODEX_TITLE_MODEL":             codexTitleModel,
		"WALLFACER_MAX_PARALLEL":        maxParallel,
		"WALLFACER_OVERSIGHT_INTERVAL":  oversightInterval,
		"WALLFACER_AUTO_PUSH":           autoPush,
		"WALLFACER_AUTO_PUSH_THRESHOLD": autoPushThreshold,
	}
	return updateFile(path, updates)
}

// UpdateSandboxSettings merges global sandbox-routing settings into the env file.
// defaultSandbox controls WALLFACER_DEFAULT_SANDBOX.
// sandboxByActivity supports keys: implementation, testing, refinement, title,
// oversight, commit_message, idea_agent.
func UpdateSandboxSettings(path string, defaultSandbox *string, sandboxByActivity map[string]string) error {
	var impl, test, refine, title, oversight, commit, idea *string
	if sandboxByActivity != nil {
		emptyImpl, emptyTest, emptyRefine := "", "", ""
		emptyTitle, emptyOversight, emptyCommit, emptyIdea := "", "", "", ""
		impl, test, refine = &emptyImpl, &emptyTest, &emptyRefine
		title, oversight, commit, idea = &emptyTitle, &emptyOversight, &emptyCommit, &emptyIdea

		if v, ok := sandboxByActivity["implementation"]; ok {
			s := strings.ToLower(strings.TrimSpace(v))
			impl = &s
		}
		if v, ok := sandboxByActivity["testing"]; ok {
			s := strings.ToLower(strings.TrimSpace(v))
			test = &s
		}
		if v, ok := sandboxByActivity["refinement"]; ok {
			s := strings.ToLower(strings.TrimSpace(v))
			refine = &s
		}
		if v, ok := sandboxByActivity["title"]; ok {
			s := strings.ToLower(strings.TrimSpace(v))
			title = &s
		}
		if v, ok := sandboxByActivity["oversight"]; ok {
			s := strings.ToLower(strings.TrimSpace(v))
			oversight = &s
		}
		if v, ok := sandboxByActivity["commit_message"]; ok {
			s := strings.ToLower(strings.TrimSpace(v))
			commit = &s
		}
		if v, ok := sandboxByActivity["idea_agent"]; ok {
			s := strings.ToLower(strings.TrimSpace(v))
			idea = &s
		}
	}

	if defaultSandbox != nil {
		s := strings.ToLower(strings.TrimSpace(*defaultSandbox))
		defaultSandbox = &s
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}

	updates := map[string]*string{
		"WALLFACER_DEFAULT_SANDBOX":         defaultSandbox,
		"WALLFACER_SANDBOX_IMPLEMENTATION":  impl,
		"WALLFACER_SANDBOX_TESTING":         test,
		"WALLFACER_SANDBOX_REFINEMENT":      refine,
		"WALLFACER_SANDBOX_TITLE":           title,
		"WALLFACER_SANDBOX_OVERSIGHT":       oversight,
		"WALLFACER_SANDBOX_COMMIT_MESSAGE":  commit,
		"WALLFACER_SANDBOX_IDEA_AGENT":      idea,
	}
	return updateRawWithUpdates(path, raw, updates)
}

func updateFile(path string, updates map[string]*string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}
	return updateRawWithUpdates(path, raw, updates)
}

func updateRawWithUpdates(path string, raw []byte, updates map[string]*string) error {
	lines := strings.Split(string(raw), "\n")
	seen := map[string]bool{}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		ptr, known := updates[k]
		if !known {
			continue
		}
		seen[k] = true
		if ptr == nil {
			// No change requested.
			continue
		}
		if *ptr == "" {
			// Clear: remove the line by blanking it.
			lines[i] = ""
		} else {
			lines[i] = k + "=" + *ptr
		}
	}

	// Append new keys (in stable order) that weren't already in the file.
	for _, k := range knownKeys {
		ptr, ok := updates[k]
		if !ok {
			continue
		}
		if seen[k] || ptr == nil || *ptr == "" {
			continue
		}
		lines = append(lines, k+"="+*ptr)
	}

	// Remove blank lines introduced by clearing, then ensure a single trailing newline.
	var kept []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" || !isBlankRemovable(l) {
			kept = append(kept, l)
		}
	}
	content := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"

	// Atomic write via temp file + rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename env file: %w", err)
	}
	return nil
}

// unquote strips matching double or single quotes surrounding a value.
func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// isBlankRemovable returns true for lines that are empty or whitespace-only
// and were not originally blank comment/separator lines. We track this by
// checking whether the original content was literally "".
func isBlankRemovable(l string) bool {
	return strings.TrimSpace(l) == ""
}

// MaskToken returns a redacted representation of a token for display.
// Short or empty tokens are fully masked.
func MaskToken(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 8 {
		return strings.Repeat("*", len(v))
	}
	return v[:4] + "..." + v[len(v)-4:]
}
