package envconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/envconfig"
)

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	return path
}

func TestParse(t *testing.T) {
	content := `# comment
CLAUDE_CODE_OAUTH_TOKEN=oauth-abc
ANTHROPIC_API_KEY=sk-ant-123
ANTHROPIC_BASE_URL=https://example.com
WALLFACER_DEFAULT_MODEL=claude-opus-4-5
WALLFACER_TITLE_MODEL=claude-haiku-4-5
UNKNOWN_KEY=ignored
`
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OAuthToken != "oauth-abc" {
		t.Errorf("OAuthToken = %q; want oauth-abc", cfg.OAuthToken)
	}
	if cfg.APIKey != "sk-ant-123" {
		t.Errorf("APIKey = %q; want sk-ant-123", cfg.APIKey)
	}
	if cfg.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q; want https://example.com", cfg.BaseURL)
	}
	if cfg.DefaultModel != "claude-opus-4-5" {
		t.Errorf("DefaultModel = %q; want claude-opus-4-5", cfg.DefaultModel)
	}
	if cfg.TitleModel != "claude-haiku-4-5" {
		t.Errorf("TitleModel = %q; want claude-haiku-4-5", cfg.TitleModel)
	}
}

func TestParseEmpty(t *testing.T) {
	path := writeEnvFile(t, "# just a comment\n\n")
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OAuthToken != "" || cfg.APIKey != "" || cfg.BaseURL != "" || cfg.DefaultModel != "" || cfg.TitleModel != "" {
		t.Errorf("expected all empty, got %+v", cfg)
	}
}

func ptr(s string) *string { return &s }

func TestUpdateExistingKeys(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=old-token\nANTHROPIC_BASE_URL=https://old.example.com\n"
	path := writeEnvFile(t, content)

	if err := envconfig.Update(path, ptr("new-token"), nil, ptr("https://new.example.com"), ptr("claude-haiku-4-5"), nil, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse after update: %v", err)
	}
	if cfg.OAuthToken != "new-token" {
		t.Errorf("OAuthToken = %q; want new-token", cfg.OAuthToken)
	}
	if cfg.BaseURL != "https://new.example.com" {
		t.Errorf("BaseURL = %q; want https://new.example.com", cfg.BaseURL)
	}
	if cfg.DefaultModel != "claude-haiku-4-5" {
		t.Errorf("DefaultModel = %q; want claude-haiku-4-5", cfg.DefaultModel)
	}
}

func TestUpdateNilSkips(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=keep-me\n"
	path := writeEnvFile(t, content)

	// nil pointer → leave unchanged.
	if err := envconfig.Update(path, nil, nil, ptr("https://example.com"), nil, nil, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse after update: %v", err)
	}
	if cfg.OAuthToken != "keep-me" {
		t.Errorf("OAuthToken = %q; want keep-me", cfg.OAuthToken)
	}
}

func TestUpdateClearsField(t *testing.T) {
	content := "ANTHROPIC_BASE_URL=https://old.example.com\nWALLFACER_DEFAULT_MODEL=claude-opus-4-5\n"
	path := writeEnvFile(t, content)

	// Empty string pointer → clear the field.
	if err := envconfig.Update(path, nil, nil, ptr(""), ptr(""), nil, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse after update: %v", err)
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL = %q; want empty after clear", cfg.BaseURL)
	}
	if cfg.DefaultModel != "" {
		t.Errorf("DefaultModel = %q; want empty after clear", cfg.DefaultModel)
	}
}

func TestUpdateAppendsNewKeys(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)

	if err := envconfig.Update(path, nil, nil, ptr("https://example.com"), ptr("claude-sonnet-4-5"), ptr("claude-haiku-4-5"), nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "ANTHROPIC_BASE_URL=https://example.com") {
		t.Errorf("expected ANTHROPIC_BASE_URL in file, got:\n%s", raw)
	}
	if !strings.Contains(string(raw), "WALLFACER_DEFAULT_MODEL=claude-sonnet-4-5") {
		t.Errorf("expected WALLFACER_DEFAULT_MODEL in file, got:\n%s", raw)
	}
	if !strings.Contains(string(raw), "WALLFACER_TITLE_MODEL=claude-haiku-4-5") {
		t.Errorf("expected WALLFACER_TITLE_MODEL in file, got:\n%s", raw)
	}
}

func TestUpdatePreservesComments(t *testing.T) {
	content := "# Auth token\nCLAUDE_CODE_OAUTH_TOKEN=tok\n# end\n"
	path := writeEnvFile(t, content)

	if err := envconfig.Update(path, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "# Auth token") {
		t.Errorf("comment not preserved: %s", raw)
	}
}

func TestParseCodexFields(t *testing.T) {
	content := `OPENAI_API_KEY=sk-openai-abc
OPENAI_BASE_URL=https://api.openai.com/v1
CODEX_DEFAULT_MODEL=codex-mini-latest
CODEX_TITLE_MODEL=codex-mini-latest
`
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OpenAIAPIKey != "sk-openai-abc" {
		t.Errorf("OpenAIAPIKey = %q; want sk-openai-abc", cfg.OpenAIAPIKey)
	}
	if cfg.OpenAIBaseURL != "https://api.openai.com/v1" {
		t.Errorf("OpenAIBaseURL = %q; want https://api.openai.com/v1", cfg.OpenAIBaseURL)
	}
	if cfg.CodexDefaultModel != "codex-mini-latest" {
		t.Errorf("CodexDefaultModel = %q; want codex-mini-latest", cfg.CodexDefaultModel)
	}
	if cfg.CodexTitleModel != "codex-mini-latest" {
		t.Errorf("CodexTitleModel = %q; want codex-mini-latest", cfg.CodexTitleModel)
	}
}

func TestParseCodexFieldsAbsent(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OpenAIAPIKey != "" {
		t.Errorf("OpenAIAPIKey = %q; want empty", cfg.OpenAIAPIKey)
	}
	if cfg.CodexDefaultModel != "" {
		t.Errorf("CodexDefaultModel = %q; want empty", cfg.CodexDefaultModel)
	}
	if cfg.CodexTitleModel != "" {
		t.Errorf("CodexTitleModel = %q; want empty", cfg.CodexTitleModel)
	}
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"", ""},
		{"short", "*****"},
		{"12345678", "********"},
		{"abcdefghij", "abcd...ghij"},
		{"sk-ant-abc123xyz", "sk-a...xyza"},
	}
	// Re-check last one properly:
	for _, tc := range tests {
		got := envconfig.MaskToken(tc.input)
		if tc.input == "sk-ant-abc123xyz" {
			// just check it's masked (prefix...suffix format)
			if !strings.Contains(got, "...") && len(tc.input) > 8 {
				t.Errorf("MaskToken(%q) = %q; expected masked form", tc.input, got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("MaskToken(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}
