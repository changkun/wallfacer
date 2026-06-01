package harness

import "testing"

func TestNormalizeID(t *testing.T) {
	cases := map[string]ID{
		"claude":    Claude,
		"  Claude ": Claude,
		"CODEX":     Codex,
		"weird":     ID("weird"),
		"":          ID(""),
	}
	for in, want := range cases {
		if got := NormalizeID(in); got != want {
			t.Errorf("NormalizeID(%q) = %q, want %q", in, got, want)
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
	// Unregistered harness ID: zero value, not ok.
	if id, ok := ParseID("cursor"); ok || id != "" {
		t.Errorf("ParseID(cursor) = %q,%v; want \"\",false (not registered)", id, ok)
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
	if Cursor.IsValid() {
		t.Error("Cursor not implemented yet, should be invalid")
	}
	if got := Cursor.OrDefault(); got != Default() {
		t.Errorf("Cursor.OrDefault() = %q, want %q", got, Default())
	}
	if got := Codex.OrDefault(); got != Codex {
		t.Errorf("Codex.OrDefault() = %q", got)
	}
}
