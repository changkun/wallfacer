package envconfig

import (
	"os"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

func TestParseEnvLinePreservesHashesInsideQuotes(t *testing.T) {
	key, value, ok := parseEnvLine(`PROMPT="Improve # parser behavior" # top-level comment`)
	if !ok {
		t.Fatal("expected env line to parse")
	}
	if key != "PROMPT" {
		t.Fatalf("key = %q, want %q", key, "PROMPT")
	}
	if value != "Improve # parser behavior" {
		t.Fatalf("value = %q, want %q", value, "Improve # parser behavior")
	}

	_, value, ok = parseEnvLine(`PROMPT='A#b # inner comment should stay' # keep outer comment`)
	if !ok {
		t.Fatal("expected single-quoted env line to parse")
	}
	if value != "A#b # inner comment should stay" {
		t.Fatalf("value = %q, want %q", value, "A#b # inner comment should stay")
	}
}

func TestStripEnvInlineComment(t *testing.T) {
	if got := stripEnvInlineComment("value # trailing comment"); got != "value" {
		t.Fatalf("stripEnvInlineComment = %q, want %q", got, "value")
	}
	if got := stripEnvInlineComment(`"value with hash # and escaped \#` + ` chars"`); got != `"value with hash # and escaped \# chars"` {
		t.Fatalf("stripEnvInlineComment double-quoted value = %q, want original", got)
	}
	if got := stripEnvInlineComment("  plain # comment"); got != "plain" {
		t.Fatalf("stripEnvInlineComment = %q, want %q", got, "plain")
	}
}

func TestSandboxByActivity(t *testing.T) {
	cfg := Config{
		ImplementationSandbox: "claude",
		TestingSandbox:        "codex",
		RefinementSandbox:     "claude",
		TitleSandbox:          "claude",
		OversightSandbox:      "codex",
		CommitMessageSandbox:  "codex",
		IdeaAgentSandbox:      "claude",
	}
	got := cfg.SandboxByActivity()
	want := map[store.SandboxActivity]string{
		store.SandboxActivityImplementation: "claude",
		store.SandboxActivityTesting:        "codex",
		store.SandboxActivityRefinement:     "claude",
		store.SandboxActivityTitle:          "claude",
		store.SandboxActivityOversight:      "codex",
		store.SandboxActivityCommitMessage:  "codex",
		store.SandboxActivityIdeaAgent:      "claude",
	}
	if len(got) != len(want) {
		t.Fatalf("SandboxByActivity size = %d, want %d", len(got), len(want))
	}
	for k, v := range want {
		if string(got[k]) != v {
			t.Errorf("SandboxByActivity[%q] = %q, want %q", k, got[k], v)
		}
	}

	emptyCfg := Config{}
	if got := emptyCfg.SandboxByActivity(); got != nil {
		t.Fatalf("expected nil for empty sandbox config, got %#v", got)
	}
}

func TestUpdateSandboxSettings(t *testing.T) {
	path := t.TempDir() + "/.env"
	initial := strings.Join([]string{
		"WALLFACER_DEFAULT_SANDBOX=claude",
		"WALLFACER_SANDBOX_IMPLEMENTATION=claude",
		"WALLFACER_SANDBOX_TESTING=codex",
		"WALLFACER_SANDBOX_REFINEMENT=claude",
		"UNRELATED=keep",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	defaultSandbox := sandbox.Codex
	if err := UpdateSandboxSettings(path, &defaultSandbox, map[store.SandboxActivity]sandbox.Type{
		store.SandboxActivityImplementation: sandbox.Codex,
		store.SandboxActivityIdeaAgent:      sandbox.Claude,
		store.SandboxActivityCommitMessage:  "",
	}); err != nil {
		t.Fatalf("UpdateSandboxSettings: %v", err)
	}

	cfg, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.DefaultSandbox != "codex" {
		t.Errorf("DefaultSandbox = %q, want codex", cfg.DefaultSandbox)
	}
	if cfg.ImplementationSandbox != "codex" {
		t.Errorf("ImplementationSandbox = %q, want codex", cfg.ImplementationSandbox)
	}
	if cfg.TestingSandbox != "" {
		t.Errorf("TestingSandbox should be cleared, got %q", cfg.TestingSandbox)
	}
	if cfg.RefinementSandbox != "" {
		t.Errorf("RefinementSandbox should be cleared, got %q", cfg.RefinementSandbox)
	}
	if cfg.CommitMessageSandbox != "" {
		t.Errorf("CommitMessageSandbox should be cleared, got %q", cfg.CommitMessageSandbox)
	}
	if cfg.IdeaAgentSandbox != "claude" {
		t.Errorf("IdeaAgentSandbox = %q, want claude", cfg.IdeaAgentSandbox)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated env file: %v", err)
	}
	if !strings.Contains(string(raw), "UNRELATED=keep") {
		t.Fatalf("expected unrelated env key to remain, got: %s", raw)
	}
}
