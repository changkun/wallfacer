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
	OAuthToken       string // CLAUDE_CODE_OAUTH_TOKEN
	APIKey           string // ANTHROPIC_API_KEY
	AuthToken        string // ANTHROPIC_AUTH_TOKEN (gateway proxy token)
	BaseURL          string // ANTHROPIC_BASE_URL
	DefaultModel     string // WALLFACER_DEFAULT_MODEL
	TitleModel       string // WALLFACER_TITLE_MODEL
	MaxParallelTasks int    // WALLFACER_MAX_PARALLEL (0 means use default)
}

// knownKeys is the ordered list of keys managed by this package.
var knownKeys = []string{
	"CLAUDE_CODE_OAUTH_TOKEN",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_BASE_URL",
	"WALLFACER_DEFAULT_MODEL",
	"WALLFACER_TITLE_MODEL",
	"WALLFACER_MAX_PARALLEL",
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
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = unquote(strings.TrimSpace(v))
		switch k {
		case "CLAUDE_CODE_OAUTH_TOKEN":
			cfg.OAuthToken = v
		case "ANTHROPIC_API_KEY":
			cfg.APIKey = v
		case "ANTHROPIC_AUTH_TOKEN":
			cfg.AuthToken = v
		case "ANTHROPIC_BASE_URL":
			cfg.BaseURL = v
		case "WALLFACER_DEFAULT_MODEL":
			cfg.DefaultModel = v
		case "WALLFACER_TITLE_MODEL":
			cfg.TitleModel = v
		case "WALLFACER_MAX_PARALLEL":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.MaxParallelTasks = n
			}
		}
	}
	return cfg, nil
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
func Update(path string, oauthToken, apiKey, baseURL, defaultModel, titleModel, maxParallel *string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}

	updates := map[string]*string{
		"CLAUDE_CODE_OAUTH_TOKEN": oauthToken,
		"ANTHROPIC_API_KEY":      apiKey,
		"ANTHROPIC_BASE_URL":     baseURL,
		"WALLFACER_DEFAULT_MODEL": defaultModel,
		"WALLFACER_TITLE_MODEL":   titleModel,
		"WALLFACER_MAX_PARALLEL":  maxParallel,
	}

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
		ptr := updates[k]
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
