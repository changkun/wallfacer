package harness

import "testing"

func TestNormalizeID(t *testing.T) {
	// Slice rather than a map so the intentional whitespace input
	// ("  Claude ", which exercises trimming) isn't a map key — gocritic
	// flags suspicious-whitespace keys.
	cases := []struct {
		in   string
		want ID
	}{
		{"claude", Claude},
		{"  Claude ", Claude},
		{"CODEX", Codex},
		{"weird", ID("weird")},
		{"", ID("")},
	}
	for _, c := range cases {
		if got := NormalizeID(c.in); got != c.want {
			t.Errorf("NormalizeID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseID(t *testing.T) {
	if id, ok := ParseID("claude"); !ok || id != Claude {
		t.Errorf("ParseID(claude) = %q,%v", id, ok)
	}
	if id, ok := ParseID(" CODEX "); !ok || id != Codex {
		t.Errorf("ParseID(CODEX) = %q,%v", id, ok)
	}
	// Unregistered harness ID: zero value, not ok. Use a literal that no
	// harness registers, so this stays correct as new harnesses land.
	if id, ok := ParseID("ghost"); ok || id != "" {
		t.Errorf("ParseID(ghost) = %q,%v; want \"\",false (not registered)", id, ok)
	}
}

func TestDefaultFrom(t *testing.T) {
	if got := DefaultFrom("codex"); got != Codex {
		t.Errorf("DefaultFrom(codex) = %q", got)
	}
	if got := DefaultFrom("nope"); got != Default() {
		t.Errorf("DefaultFrom(nope) = %q, want %q", got, Default())
	}
}

func TestIDIsValidOrDefault(t *testing.T) {
	if !Claude.IsValid() {
		t.Error("Claude should be valid (registered)")
	}
	ghost := ID("ghost")
	if ghost.IsValid() {
		t.Error("ghost is not registered, should be invalid")
	}
	if got := ghost.OrDefault(); got != Default() {
		t.Errorf("ghost.OrDefault() = %q, want %q", got, Default())
	}
	if got := Codex.OrDefault(); got != Codex {
		t.Errorf("Codex.OrDefault() = %q", got)
	}
}
