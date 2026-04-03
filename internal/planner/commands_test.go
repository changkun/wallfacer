package planner

import (
	"strings"
	"testing"
)

func TestCommandRegistry_Commands(t *testing.T) {
	r := NewCommandRegistry()
	cmds := r.Commands()

	if len(cmds) != 7 {
		t.Fatalf("len(Commands) = %d, want 7", len(cmds))
	}

	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
		if c.Description == "" {
			t.Errorf("command %q has empty description", c.Name)
		}
		if c.Usage == "" {
			t.Errorf("command %q has empty usage", c.Name)
		}
	}

	for _, want := range []string{"summarize", "break-down", "create", "status", "validate", "impact", "dispatch"} {
		if !names[want] {
			t.Errorf("missing command %q", want)
		}
	}
}

func TestCommandRegistry_Expand_SlashCommand(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/summarize 100", "specs/local/foo.md")
	if !ok {
		t.Fatal("expected slash command to be recognized")
	}
	if !strings.Contains(expanded, "specs/local/foo.md") {
		t.Errorf("expanded prompt missing focused spec path: %q", expanded)
	}
	if !strings.Contains(expanded, "100") {
		t.Errorf("expanded prompt missing word limit: %q", expanded)
	}
}

func TestCommandRegistry_Expand_DefaultWordLimit(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/summarize", "specs/local/foo.md")
	if !ok {
		t.Fatal("expected slash command to be recognized")
	}
	if !strings.Contains(expanded, "200") {
		t.Errorf("expected default word limit 200 in: %q", expanded)
	}
}

func TestCommandRegistry_Expand_NotSlashCommand(t *testing.T) {
	r := NewCommandRegistry()

	result, ok := r.Expand("hello world", "specs/foo.md")
	if ok {
		t.Error("expected ok=false for non-slash input")
	}
	if result != "hello world" {
		t.Errorf("expected original input returned, got %q", result)
	}
}

func TestCommandRegistry_Expand_UnknownCommand(t *testing.T) {
	r := NewCommandRegistry()

	result, ok := r.Expand("/unknown arg", "specs/foo.md")
	if ok {
		t.Error("expected ok=false for unknown command")
	}
	if result != "/unknown arg" {
		t.Errorf("expected original input returned, got %q", result)
	}
}

func TestCommandRegistry_Expand_BreakDown(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/break-down", "specs/local/bar.md")
	if !ok {
		t.Fatal("expected break-down to be recognized")
	}
	if !strings.Contains(expanded, "specs/local/bar.md") {
		t.Errorf("expanded prompt missing focused spec: %q", expanded)
	}
}

func TestCommandRegistry_Expand_Create(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/create My New Spec", "")
	if !ok {
		t.Fatal("expected create to be recognized")
	}
	if !strings.Contains(expanded, "My New Spec") {
		t.Errorf("expanded prompt missing title: %q", expanded)
	}
}

func TestCommandRegistry_Expand_Status(t *testing.T) {
	r := NewCommandRegistry()

	expanded, ok := r.Expand("/status validated", "specs/local/foo.md")
	if !ok {
		t.Fatal("expected status to be recognized")
	}
	if !strings.Contains(expanded, "validated") {
		t.Errorf("expanded prompt missing state: %q", expanded)
	}
}
