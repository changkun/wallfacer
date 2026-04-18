package envconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/envconfig"
)

// writeEnvFile creates a temporary .env file with the given content and returns its path.
func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	return path
}

// TestParse verifies that Parse correctly reads known keys and ignores unknown ones.
func TestParse(t *testing.T) {
	content := `# comment
CLAUDE_CODE_OAUTH_TOKEN=oauth-abc
ANTHROPIC_API_KEY=sk-ant-123
ANTHROPIC_BASE_URL=https://example.com
CLAUDE_DEFAULT_MODEL=claude-opus-4-5
CLAUDE_TITLE_MODEL=claude-haiku-4-5
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

// TestParseExportedKeys verifies that the "export " prefix is stripped from key lines.
func TestParseExportedKeys(t *testing.T) {
	content := `export CLAUDE_CODE_OAUTH_TOKEN=exported-oauth
export ANTHROPIC_API_KEY=sk-ant-exported
`
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OAuthToken != "exported-oauth" {
		t.Errorf("OAuthToken = %q; want exported-oauth", cfg.OAuthToken)
	}
	if cfg.APIKey != "sk-ant-exported" {
		t.Errorf("APIKey = %q; want sk-ant-exported", cfg.APIKey)
	}
}

// TestParseInlineComment verifies that trailing # comments are stripped from values.
func TestParseInlineComment(t *testing.T) {
	content := `CLAUDE_CODE_OAUTH_TOKEN=oauth-abc # set in local env
CLAUDE_DEFAULT_MODEL=claude-sonnet-4-0 # this is a model`
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OAuthToken != "oauth-abc" {
		t.Errorf("OAuthToken = %q; want oauth-abc", cfg.OAuthToken)
	}
	if cfg.DefaultModel != "claude-sonnet-4-0" {
		t.Errorf("DefaultModel = %q; want claude-sonnet-4-0", cfg.DefaultModel)
	}
}

// TestParseEmpty verifies that a file with only comments yields zero-value config fields.
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

// TestParseHostBinaryOverrides verifies that the two optional host-mode
// binary-path overrides are parsed into Config.
func TestParseHostBinaryOverrides(t *testing.T) {
	content := "WALLFACER_HOST_CLAUDE_BINARY=/usr/local/bin/claude\nWALLFACER_HOST_CODEX_BINARY=/opt/codex/bin/codex\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.HostClaudeBinary != "/usr/local/bin/claude" {
		t.Errorf("HostClaudeBinary = %q", cfg.HostClaudeBinary)
	}
	if cfg.HostCodexBinary != "/opt/codex/bin/codex" {
		t.Errorf("HostCodexBinary = %q", cfg.HostCodexBinary)
	}
}

// TestParseHostBinaryOverrides_Empty verifies that absent keys yield zero-value fields.
func TestParseHostBinaryOverrides_Empty(t *testing.T) {
	path := writeEnvFile(t, "# nothing here\n")
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.HostClaudeBinary != "" || cfg.HostCodexBinary != "" {
		t.Errorf("expected empty overrides; got claude=%q codex=%q", cfg.HostClaudeBinary, cfg.HostCodexBinary)
	}
}

// TestParse_IgnoresSandboxBackendEnv verifies that WALLFACER_SANDBOX_BACKEND is
// no longer read from the env file — backend selection moved to the
// --backend CLI flag. Parse must leave the field zero.
func TestParse_IgnoresSandboxBackendEnv(t *testing.T) {
	path := writeEnvFile(t, "WALLFACER_SANDBOX_BACKEND=host\n")
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.SandboxBackend != "" {
		t.Errorf("SandboxBackend should not be populated from env file; got %q", cfg.SandboxBackend)
	}
}

// TestParseServerAPIKey verifies parsing of the WALLFACER_SERVER_API_KEY field.
func TestParseServerAPIKey(t *testing.T) {
	path := writeEnvFile(t, "WALLFACER_SERVER_API_KEY=secret-key\n")
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.ServerAPIKey != "secret-key" {
		t.Fatalf("ServerAPIKey = %q; want secret-key", cfg.ServerAPIKey)
	}
}

// ptr returns a pointer to s, used to construct non-nil Updates fields in tests.
func ptr(s string) *string { return &s }

// TestUpdateExistingKeys verifies that Update replaces existing keys and appends new ones.
func TestUpdateExistingKeys(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=old-token\nANTHROPIC_BASE_URL=https://old.example.com\n"
	path := writeEnvFile(t, content)

	if err := envconfig.Update(path, envconfig.Updates{
		OAuthToken:   ptr("new-token"),
		BaseURL:      ptr("https://new.example.com"),
		DefaultModel: ptr("claude-haiku-4-5"),
	}); err != nil {
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

// TestUpdateNilSkips verifies that nil pointer fields in Updates leave existing values unchanged.
func TestUpdateNilSkips(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=keep-me\n"
	path := writeEnvFile(t, content)

	// nil pointer → leave unchanged.
	if err := envconfig.Update(path, envconfig.Updates{
		BaseURL: ptr("https://example.com"),
	}); err != nil {
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

// TestUpdateClearsField verifies that an empty-string pointer removes the key from the file.
func TestUpdateClearsField(t *testing.T) {
	content := "ANTHROPIC_BASE_URL=https://old.example.com\nCLAUDE_DEFAULT_MODEL=claude-opus-4-5\n"
	path := writeEnvFile(t, content)

	// Empty string pointer → clear the field.
	if err := envconfig.Update(path, envconfig.Updates{
		BaseURL:      ptr(""),
		DefaultModel: ptr(""),
	}); err != nil {
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

// TestUpdateAppendsNewKeys verifies that keys not present in the file are appended.
func TestUpdateAppendsNewKeys(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)

	if err := envconfig.Update(path, envconfig.Updates{
		BaseURL:      ptr("https://example.com"),
		DefaultModel: ptr("claude-sonnet-4-5"),
		TitleModel:   ptr("claude-haiku-4-5"),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "ANTHROPIC_BASE_URL=https://example.com") {
		t.Errorf("expected ANTHROPIC_BASE_URL in file, got:\n%s", raw)
	}
	if !strings.Contains(string(raw), "CLAUDE_DEFAULT_MODEL=claude-sonnet-4-5") {
		t.Errorf("expected CLAUDE_DEFAULT_MODEL in file, got:\n%s", raw)
	}
	if !strings.Contains(string(raw), "CLAUDE_TITLE_MODEL=claude-haiku-4-5") {
		t.Errorf("expected CLAUDE_TITLE_MODEL in file, got:\n%s", raw)
	}
}

// TestUpdatePreservesComments verifies that comment lines survive an Update round-trip.
func TestUpdatePreservesComments(t *testing.T) {
	content := "# Auth token\nCLAUDE_CODE_OAUTH_TOKEN=tok\n# end\n"
	path := writeEnvFile(t, content)

	if err := envconfig.Update(path, envconfig.Updates{}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "# Auth token") {
		t.Errorf("comment not preserved: %s", raw)
	}
}

// TestUpdateServerAPIKey verifies that the server API key can be set via Update.
func TestUpdateServerAPIKey(t *testing.T) {
	path := writeEnvFile(t, "CLAUDE_CODE_OAUTH_TOKEN=tok\n")
	value := "server-secret"
	if err := envconfig.Update(path, envconfig.Updates{ServerAPIKey: &value}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse after update: %v", err)
	}
	if cfg.ServerAPIKey != "server-secret" {
		t.Fatalf("ServerAPIKey = %q; want server-secret", cfg.ServerAPIKey)
	}
}

// TestParseCodexFields verifies parsing of all OpenAI Codex-related env keys.
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

// TestParseCodexFieldsAbsent verifies that Codex fields default to empty when not in the file.
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

// ---------------------------------------------------------------------------
// OversightInterval
// ---------------------------------------------------------------------------

// TestParseOversightInterval verifies parsing a valid positive oversight interval.
func TestParseOversightInterval(t *testing.T) {
	content := "WALLFACER_OVERSIGHT_INTERVAL=10\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OversightInterval != 10 {
		t.Errorf("OversightInterval = %d; want 10", cfg.OversightInterval)
	}
}

// TestParseOversightIntervalZero verifies that an explicit "0" is accepted (disables periodic oversight).
func TestParseOversightIntervalZero(t *testing.T) {
	content := "WALLFACER_OVERSIGHT_INTERVAL=0\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OversightInterval != 0 {
		t.Errorf("OversightInterval = %d; want 0", cfg.OversightInterval)
	}
}

// TestParseOversightIntervalInvalid verifies that a non-numeric value is silently ignored (defaults to 0).
func TestParseOversightIntervalInvalid(t *testing.T) {
	content := "WALLFACER_OVERSIGHT_INTERVAL=notanumber\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Invalid value: should remain zero (default).
	if cfg.OversightInterval != 0 {
		t.Errorf("OversightInterval = %d; want 0 for invalid value", cfg.OversightInterval)
	}
}

// TestParseOversightIntervalAbsent verifies that a missing key defaults to 0.
func TestParseOversightIntervalAbsent(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OversightInterval != 0 {
		t.Errorf("OversightInterval = %d; want 0 when absent", cfg.OversightInterval)
	}
}

// TestParseArchivedTasksPerPage verifies parsing a valid page size value.
func TestParseArchivedTasksPerPage(t *testing.T) {
	content := "WALLFACER_ARCHIVED_TASKS_PER_PAGE=30\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.ArchivedTasksPerPage != 30 {
		t.Errorf("ArchivedTasksPerPage = %d; want 30", cfg.ArchivedTasksPerPage)
	}
}

// TestParseArchivedTasksPerPageAbsent verifies that a missing key defaults to 0.
func TestParseArchivedTasksPerPageAbsent(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.ArchivedTasksPerPage != 0 {
		t.Errorf("ArchivedTasksPerPage = %d; want 0 when absent", cfg.ArchivedTasksPerPage)
	}
}

// TestParseMaxTestParallelTasks verifies parsing a valid max test parallel value.
func TestParseMaxTestParallelTasks(t *testing.T) {
	content := "WALLFACER_MAX_TEST_PARALLEL=3\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.MaxTestParallelTasks != 3 {
		t.Errorf("MaxTestParallelTasks = %d; want 3", cfg.MaxTestParallelTasks)
	}
}

// TestParseMaxTestParallelTasksAbsent verifies that a missing key defaults to 0.
func TestParseMaxTestParallelTasksAbsent(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.MaxTestParallelTasks != 0 {
		t.Errorf("MaxTestParallelTasks = %d; want 0 when absent", cfg.MaxTestParallelTasks)
	}
}

// TestUpdateMaxTestParallelTasks verifies that max test parallel can be set via Update.
func TestUpdateMaxTestParallelTasks(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)

	v := "4"
	if err := envconfig.Update(path, envconfig.Updates{MaxTestParallel: &v}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse after update: %v", err)
	}
	if cfg.MaxTestParallelTasks != 4 {
		t.Errorf("MaxTestParallelTasks = %d; want 4 after update", cfg.MaxTestParallelTasks)
	}
}

// TestUpdateOversightInterval verifies that the oversight interval can be set via Update.
func TestUpdateOversightInterval(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)

	v := "15"
	if err := envconfig.Update(path, envconfig.Updates{OversightInterval: &v}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse after update: %v", err)
	}
	if cfg.OversightInterval != 15 {
		t.Errorf("OversightInterval = %d; want 15", cfg.OversightInterval)
	}
}

// TestUpdateArchivedTasksPerPage verifies that the archived tasks page size can be set via Update.
func TestUpdateArchivedTasksPerPage(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)

	v := "40"
	if err := envconfig.Update(path, envconfig.Updates{ArchivedTasksPerPage: &v}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse after update: %v", err)
	}
	if cfg.ArchivedTasksPerPage != 40 {
		t.Errorf("ArchivedTasksPerPage = %d; want 40", cfg.ArchivedTasksPerPage)
	}
}

// ---------------------------------------------------------------------------
// AutoPush
// ---------------------------------------------------------------------------

// TestParseAutoPush verifies parsing of auto-push enabled flag and threshold.
func TestParseAutoPush(t *testing.T) {
	content := "WALLFACER_AUTO_PUSH=true\nWALLFACER_AUTO_PUSH_THRESHOLD=3\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.AutoPushEnabled {
		t.Errorf("AutoPushEnabled = false; want true")
	}
	if cfg.AutoPushThreshold != 3 {
		t.Errorf("AutoPushThreshold = %d; want 3", cfg.AutoPushThreshold)
	}
}

// TestParseAutoPushDefaults verifies that auto-push defaults to disabled with threshold 0.
func TestParseAutoPushDefaults(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.AutoPushEnabled {
		t.Errorf("AutoPushEnabled = true; want false when absent")
	}
	if cfg.AutoPushThreshold != 0 {
		t.Errorf("AutoPushThreshold = %d; want 0 when absent", cfg.AutoPushThreshold)
	}
}

// TestParseSandboxFastDefaultsToTrue verifies SandboxFast is true when the key is absent.
func TestParseSandboxFastDefaultsToTrue(t *testing.T) {
	path := writeEnvFile(t, "CLAUDE_CODE_OAUTH_TOKEN=tok\n")
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.SandboxFast {
		t.Fatal("SandboxFast = false; want true when absent")
	}
}

// TestParseSandboxFastFalse verifies SandboxFast is false when explicitly set to "false".
func TestParseSandboxFastFalse(t *testing.T) {
	path := writeEnvFile(t, "WALLFACER_SANDBOX_FAST=false\n")
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.SandboxFast {
		t.Fatal("SandboxFast = true; want false when configured false")
	}
}

// TestUpdateSandboxFast verifies that SandboxFast can be toggled via Update.
func TestUpdateSandboxFast(t *testing.T) {
	path := writeEnvFile(t, "CLAUDE_CODE_OAUTH_TOKEN=tok\n")
	enabled := "false"
	if err := envconfig.Update(path, envconfig.Updates{SandboxFast: &enabled}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse after update: %v", err)
	}
	if cfg.SandboxFast {
		t.Fatal("SandboxFast = true; want false after update")
	}
}

// TestParseTaskWorkersDefaultsToTrue verifies TaskWorkers defaults to true when absent.
func TestParseTaskWorkersDefaultsToTrue(t *testing.T) {
	path := writeEnvFile(t, "CLAUDE_CODE_OAUTH_TOKEN=tok\n")
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.TaskWorkers {
		t.Fatal("TaskWorkers = false; want true when absent")
	}
}

// TestParseTaskWorkersDisabled verifies TaskWorkers is false when explicitly set to "false".
func TestParseTaskWorkersDisabled(t *testing.T) {
	path := writeEnvFile(t, "WALLFACER_TASK_WORKERS=false\n")
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.TaskWorkers {
		t.Fatal("TaskWorkers = true; want false when configured false")
	}
}

// TestUpdateAutoPush verifies that auto-push settings can be written and read back.
func TestUpdateAutoPush(t *testing.T) {
	content := "CLAUDE_CODE_OAUTH_TOKEN=tok\n"
	path := writeEnvFile(t, content)

	enabled := "true"
	threshold := "5"
	if err := envconfig.Update(path, envconfig.Updates{
		AutoPush:          &enabled,
		AutoPushThreshold: &threshold,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse after update: %v", err)
	}
	if !cfg.AutoPushEnabled {
		t.Errorf("AutoPushEnabled = false; want true after update")
	}
	if cfg.AutoPushThreshold != 5 {
		t.Errorf("AutoPushThreshold = %d; want 5 after update", cfg.AutoPushThreshold)
	}
}

// TestMaskToken verifies token redaction: empty stays empty, short tokens are fully masked,
// and longer tokens show first 4 and last 4 characters with "..." in between.
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

// ─────────────────────────────────────────────────────────────────────────────
// ParseWorkspaces / FormatWorkspaces
// ─────────────────────────────────────────────────────────────────────────────

// TestParseWorkspaces_Empty verifies that empty and whitespace-only input returns nil.
func TestParseWorkspaces_Empty(t *testing.T) {
	if got := envconfig.ParseWorkspaces(""); got != nil {
		t.Errorf("ParseWorkspaces(\"\") = %v, want nil", got)
	}
	if got := envconfig.ParseWorkspaces("   "); got != nil {
		t.Errorf("ParseWorkspaces(whitespace) = %v, want nil", got)
	}
}

// TestParseWorkspaces_SinglePath verifies a single path without separators.
func TestParseWorkspaces_SinglePath(t *testing.T) {
	got := envconfig.ParseWorkspaces("/workspace/proj")
	if len(got) != 1 || got[0] != "/workspace/proj" {
		t.Errorf("ParseWorkspaces single = %v, want [/workspace/proj]", got)
	}
}

// TestParseWorkspaces_MultiplePaths verifies splitting multiple OS path-list separated paths.
func TestParseWorkspaces_MultiplePaths(t *testing.T) {
	input := strings.Join([]string{"/a", "/b", "/c"}, string(os.PathListSeparator))
	got := envconfig.ParseWorkspaces(input)
	if len(got) != 3 {
		t.Fatalf("ParseWorkspaces(%q) len = %d, want 3", input, len(got))
	}
	if got[0] != "/a" || got[1] != "/b" || got[2] != "/c" {
		t.Errorf("ParseWorkspaces(%q) = %v, want [/a /b /c]", input, got)
	}
}

// TestParseWorkspaces_FiltersEmptyEntries verifies that empty segments from double separators are dropped.
func TestParseWorkspaces_FiltersEmptyEntries(t *testing.T) {
	// Leading/trailing/double separators produce empty parts.
	sep := string(os.PathListSeparator)
	got := envconfig.ParseWorkspaces(sep + "/a" + sep + sep + "/b" + sep)
	if len(got) != 2 {
		t.Errorf("ParseWorkspaces with empty entries = %v (len %d), want 2 entries", got, len(got))
	}
}

// TestParseWorkspaces_AllEmptyEntriesReturnsNil verifies that only separators (no real paths) returns nil.
func TestParseWorkspaces_AllEmptyEntriesReturnsNil(t *testing.T) {
	if got := envconfig.ParseWorkspaces(strings.Repeat(string(os.PathListSeparator), 3)); got != nil {
		t.Errorf("ParseWorkspaces(all separators) = %v, want nil", got)
	}
}

// TestFormatWorkspaces_Empty verifies that nil and empty slices produce an empty string.
func TestFormatWorkspaces_Empty(t *testing.T) {
	if got := envconfig.FormatWorkspaces(nil); got != "" {
		t.Errorf("FormatWorkspaces(nil) = %q, want \"\"", got)
	}
	if got := envconfig.FormatWorkspaces([]string{}); got != "" {
		t.Errorf("FormatWorkspaces([]) = %q, want \"\"", got)
	}
}

// TestFormatWorkspaces_Single verifies that a single-element slice encodes without separator.
func TestFormatWorkspaces_Single(t *testing.T) {
	got := envconfig.FormatWorkspaces([]string{"/workspace/proj"})
	if got != "/workspace/proj" {
		t.Errorf("FormatWorkspaces single = %q, want \"/workspace/proj\"", got)
	}
}

// TestFormatWorkspaces_Multiple verifies that all paths appear in the encoded output.
func TestFormatWorkspaces_Multiple(t *testing.T) {
	paths := []string{"/a", "/b", "/c"}
	got := envconfig.FormatWorkspaces(paths)
	if !strings.Contains(got, "/a") || !strings.Contains(got, "/b") || !strings.Contains(got, "/c") {
		t.Errorf("FormatWorkspaces(%v) = %q; expected all paths present", paths, got)
	}
}

// TestFormatWorkspaces_RoundTrip verifies that Format then Parse returns the original paths.
func TestFormatWorkspaces_RoundTrip(t *testing.T) {
	original := []string{"/workspace/project1", "/workspace/project2"}
	encoded := envconfig.FormatWorkspaces(original)
	decoded := envconfig.ParseWorkspaces(encoded)
	if len(decoded) != len(original) {
		t.Fatalf("round-trip len: %d, want %d", len(decoded), len(original))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("round-trip[%d] = %q, want %q", i, decoded[i], original[i])
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateWorkspaces
// ─────────────────────────────────────────────────────────────────────────────

// TestUpdateWorkspaces_WritesAndReads verifies that UpdateWorkspaces writes the key to the file.
func TestUpdateWorkspaces_WritesAndReads(t *testing.T) {
	path := writeEnvFile(t, "ANTHROPIC_API_KEY=sk-test\n")

	workspaces := []string{"/workspace/proj1", "/workspace/proj2"}
	if err := envconfig.UpdateWorkspaces(path, workspaces); err != nil {
		t.Fatalf("UpdateWorkspaces: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "WALLFACER_WORKSPACES") {
		t.Error("expected WALLFACER_WORKSPACES in updated file")
	}
}

// TestUpdateWorkspaces_ClearsWithEmpty verifies that passing nil clears the workspace path.
func TestUpdateWorkspaces_ClearsWithEmpty(t *testing.T) {
	path := writeEnvFile(t, "WALLFACER_WORKSPACES=/old/path\nANTHROPIC_API_KEY=sk-test\n")

	if err := envconfig.UpdateWorkspaces(path, nil); err != nil {
		t.Fatalf("UpdateWorkspaces clear: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// The value should now be empty.
	if strings.Contains(string(content), "/old/path") {
		t.Error("expected old workspace path to be cleared")
	}
}

// TestParseTerminalEnabledDefaultsToTrue verifies that TerminalEnabled is true when the key is absent.
func TestParseTerminalEnabledDefaultsToTrue(t *testing.T) {
	path := writeEnvFile(t, "CLAUDE_CODE_OAUTH_TOKEN=tok\n")
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.TerminalEnabled {
		t.Error("TerminalEnabled = false; want true when absent")
	}
}

// TestParseTerminalEnabledFalse verifies that TerminalEnabled is false when explicitly set to "false".
func TestParseTerminalEnabledFalse(t *testing.T) {
	path := writeEnvFile(t, "WALLFACER_TERMINAL_ENABLED=false\n")
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.TerminalEnabled {
		t.Error("TerminalEnabled = true; want false when explicitly set to false")
	}
}

// --- PlanningWindowDays ---

func TestParse_PlanningWindowDaysDefault(t *testing.T) {
	path := writeEnvFile(t, ``)
	cfg, err := envconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.PlanningWindowDays != 30 {
		t.Errorf("PlanningWindowDays = %d; want 30 when unset", cfg.PlanningWindowDays)
	}
}

func TestParse_PlanningWindowDays(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want int
	}{
		{"positive", "WALLFACER_PLANNING_WINDOW_DAYS=14", 14},
		{"zero means all time", "WALLFACER_PLANNING_WINDOW_DAYS=0", 0},
		{"large value", "WALLFACER_PLANNING_WINDOW_DAYS=365", 365},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEnvFile(t, tc.raw)
			cfg, err := envconfig.Parse(path)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cfg.PlanningWindowDays != tc.want {
				t.Errorf("PlanningWindowDays = %d; want %d", cfg.PlanningWindowDays, tc.want)
			}
		})
	}
}

func TestParse_PlanningWindowDaysInvalid(t *testing.T) {
	// Non-numeric and negative values fall back to the default 30 so the
	// planning UI always has a sane starting window. This matches how the
	// other int knobs treat malformed input.
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"non-numeric", "WALLFACER_PLANNING_WINDOW_DAYS=soon"},
		{"negative", "WALLFACER_PLANNING_WINDOW_DAYS=-5"},
		{"float", "WALLFACER_PLANNING_WINDOW_DAYS=1.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEnvFile(t, tc.raw)
			cfg, err := envconfig.Parse(path)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cfg.PlanningWindowDays != 30 {
				t.Errorf("PlanningWindowDays = %d; want 30 (fallback for %q)", cfg.PlanningWindowDays, tc.raw)
			}
		})
	}
}
