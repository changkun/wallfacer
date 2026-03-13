package sandbox

import "testing"

func TestAll_ReturnsBothTypes(t *testing.T) {
	all := All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d types, want 2", len(all))
	}
	found := map[Type]bool{}
	for _, t := range all {
		found[t] = true
	}
	if !found[Claude] {
		t.Error("All() did not include Claude")
	}
	if !found[Codex] {
		t.Error("All() did not include Codex")
	}
}

func TestAll_ReturnsCopy(t *testing.T) {
	a := All()
	b := All()
	a[0] = "mutated"
	if b[0] == "mutated" {
		t.Error("All() should return a copy, not the original slice")
	}
}

func TestParse_ValidClaude(t *testing.T) {
	tests := []string{"claude", "CLAUDE", "Claude", "  claude  ", " CLAUDE "}
	for _, input := range tests {
		got, ok := Parse(input)
		if !ok {
			t.Errorf("Parse(%q) returned ok=false, want true", input)
		}
		if got != Claude {
			t.Errorf("Parse(%q) = %q, want %q", input, got, Claude)
		}
	}
}

func TestParse_ValidCodex(t *testing.T) {
	tests := []string{"codex", "CODEX", "Codex", "  codex  ", " CODEX "}
	for _, input := range tests {
		got, ok := Parse(input)
		if !ok {
			t.Errorf("Parse(%q) returned ok=false, want true", input)
		}
		if got != Codex {
			t.Errorf("Parse(%q) = %q, want %q", input, got, Codex)
		}
	}
}

func TestParse_Invalid(t *testing.T) {
	tests := []string{"", "unknown", "docker", "openai", "gpt4", "  "}
	for _, input := range tests {
		got, ok := Parse(input)
		if ok {
			t.Errorf("Parse(%q) returned ok=true, want false", input)
		}
		if got != "" {
			t.Errorf("Parse(%q) returned %q, want empty string", input, got)
		}
	}
}

func TestNormalize_ValidInputs(t *testing.T) {
	tests := []struct {
		input string
		want  Type
	}{
		{"claude", Claude},
		{"CLAUDE", Claude},
		{"  claude  ", Claude},
		{"codex", Codex},
		{"CODEX", Codex},
	}
	for _, tc := range tests {
		got := Normalize(tc.input)
		if got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalize_InvalidInputs(t *testing.T) {
	// For invalid inputs, Normalize returns lowercase-trimmed value (not empty string)
	tests := []struct {
		input string
		want  Type
	}{
		{"UNKNOWN", Type("unknown")},
		{"  Docker  ", Type("docker")},
		{"OpenAI", Type("openai")},
	}
	for _, tc := range tests {
		got := Normalize(tc.input)
		if got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDefault_ValidInputs(t *testing.T) {
	if got := Default("claude"); got != Claude {
		t.Errorf("Default(%q) = %q, want %q", "claude", got, Claude)
	}
	if got := Default("codex"); got != Codex {
		t.Errorf("Default(%q) = %q, want %q", "codex", got, Codex)
	}
	if got := Default("CLAUDE"); got != Claude {
		t.Errorf("Default(%q) = %q, want %q", "CLAUDE", got, Claude)
	}
}

func TestDefault_InvalidFallsBackToClaude(t *testing.T) {
	tests := []string{"", "unknown", "docker", "  "}
	for _, input := range tests {
		got := Default(input)
		if got != Claude {
			t.Errorf("Default(%q) = %q, want %q (Claude fallback)", input, got, Claude)
		}
	}
}

func TestIsValid_ValidTypes(t *testing.T) {
	if !Claude.IsValid() {
		t.Error("Claude.IsValid() = false, want true")
	}
	if !Codex.IsValid() {
		t.Error("Codex.IsValid() = false, want true")
	}
}

func TestIsValid_InvalidTypes(t *testing.T) {
	// Note: "CLAUDE" is NOT invalid because Parse() lowercases input before matching.
	tests := []Type{"", "unknown", "docker"}
	for _, tp := range tests {
		if tp.IsValid() {
			t.Errorf("Type(%q).IsValid() = true, want false", tp)
		}
	}
}

func TestOrDefault_ValidTypes(t *testing.T) {
	if got := Claude.OrDefault(); got != Claude {
		t.Errorf("Claude.OrDefault() = %q, want %q", got, Claude)
	}
	if got := Codex.OrDefault(); got != Codex {
		t.Errorf("Codex.OrDefault() = %q, want %q", got, Codex)
	}
}

func TestOrDefault_InvalidFallsBackToClaude(t *testing.T) {
	// Note: "CLAUDE" is treated as valid (Parse lowercases), so it's excluded here.
	tests := []Type{"", "unknown", "docker"}
	for _, tp := range tests {
		got := tp.OrDefault()
		if got != Claude {
			t.Errorf("Type(%q).OrDefault() = %q, want %q (Claude fallback)", tp, got, Claude)
		}
	}
}
