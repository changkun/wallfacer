package sandbox

import "testing"

// TestAll_ReturnsBothTypes verifies that All() returns exactly Claude and Codex.
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

// TestAll_ReturnsCopy verifies that mutating the returned slice does not
// affect the internal canonical list.
func TestAll_ReturnsCopy(t *testing.T) {
	a := All()
	b := All()
	a[0] = "mutated"
	if b[0] == "mutated" {
		t.Error("All() should return a copy, not the original slice")
	}
}

// TestParse_ValidClaude verifies that Parse accepts "claude" in various cases
// and whitespace variants.
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

// TestParse_ValidCodex verifies that Parse accepts "codex" in various cases
// and whitespace variants.
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

// TestParse_Invalid verifies that Parse rejects unknown sandbox names
// and returns an empty Type with ok=false.
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

// TestNormalize_ValidInputs verifies that Normalize returns the canonical Type
// for recognized sandbox names regardless of case or whitespace.
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

// TestNormalize_InvalidInputs verifies that unrecognized values are lowercased
// and trimmed rather than rejected, so they can be stored as-is.
func TestNormalize_InvalidInputs(t *testing.T) {
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

// TestDefault_ValidInputs verifies that Default returns the parsed Type for
// recognized inputs.
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

// TestDefault_InvalidFallsBackToClaude verifies that unrecognized values
// fall back to Claude as the default sandbox type.
func TestDefault_InvalidFallsBackToClaude(t *testing.T) {
	tests := []string{"", "unknown", "docker", "  "}
	for _, input := range tests {
		got := Default(input)
		if got != Claude {
			t.Errorf("Default(%q) = %q, want %q (Claude fallback)", input, got, Claude)
		}
	}
}

// TestIsValid_ValidTypes verifies that the two known sandbox types report as valid.
func TestIsValid_ValidTypes(t *testing.T) {
	if !Claude.IsValid() {
		t.Error("Claude.IsValid() = false, want true")
	}
	if !Codex.IsValid() {
		t.Error("Codex.IsValid() = false, want true")
	}
}

// TestIsValid_InvalidTypes verifies that unknown strings are reported as invalid.
// Note: IsValid delegates to Parse which lowercases the input, so "CLAUDE" would
// actually be valid -- only truly unknown names are tested here.
func TestIsValid_InvalidTypes(t *testing.T) {
	tests := []Type{"", "unknown", "docker"}
	for _, tp := range tests {
		if tp.IsValid() {
			t.Errorf("Type(%q).IsValid() = true, want false", tp)
		}
	}
}

// TestOrDefault_ValidTypes verifies that OrDefault is a no-op for known types.
func TestOrDefault_ValidTypes(t *testing.T) {
	if got := Claude.OrDefault(); got != Claude {
		t.Errorf("Claude.OrDefault() = %q, want %q", got, Claude)
	}
	if got := Codex.OrDefault(); got != Codex {
		t.Errorf("Codex.OrDefault() = %q, want %q", got, Codex)
	}
}

// TestOrDefault_InvalidFallsBackToClaude verifies that OrDefault returns Claude
// for unknown Type values.
func TestOrDefault_InvalidFallsBackToClaude(t *testing.T) {
	tests := []Type{"", "unknown", "docker"}
	for _, tp := range tests {
		got := tp.OrDefault()
		if got != Claude {
			t.Errorf("Type(%q).OrDefault() = %q, want %q (Claude fallback)", tp, got, Claude)
		}
	}
}
