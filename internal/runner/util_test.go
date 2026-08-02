package runner

import "testing"

func TestTruncate_ShortString(t *testing.T) {
	got := truncate("hello", 10)
	if got != "hello" {
		t.Errorf("truncate(%q, 10) = %q, want %q", "hello", got, "hello")
	}
}

func TestTruncate_EmptyString(t *testing.T) {
	got := truncate("", 10)
	if got != "" {
		t.Errorf("truncate(%q, 10) = %q, want %q", "", got, "")
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("truncate(%q, 5) = %q, want %q", "hello", got, "hello")
	}
}

func TestTruncate_ExceedsLength(t *testing.T) {
	got := truncate("hello world", 5)
	if got != "hello…" {
		t.Errorf("truncate(%q, 5) = %q, want %q", "hello world", got, "hello…")
	}
}

func TestTruncate_SingleChar(t *testing.T) {
	got := truncate("abc", 1)
	if got != "a…" {
		t.Errorf("truncate(%q, 1) = %q, want %q", "abc", got, "a…")
	}
}

func TestTruncate_ZeroLimit(t *testing.T) {
	got := truncate("abc", 0)
	if got != "…" {
		t.Errorf("truncate(%q, 0) = %q, want %q", "abc", got, "…")
	}
}

func TestSlugifyPrompt_LeadingTrailingSpecial(t *testing.T) {
	got := slugifyPrompt("--hello--", 64)
	if got != "hello" {
		t.Errorf("slugifyPrompt(%q, 64) = %q, want %q", "--hello--", got, "hello")
	}
}

func TestSlugifyPrompt_MixedInput(t *testing.T) {
	got := slugifyPrompt("Fix bug in v1.2.3!", 64)
	if got != "fix-bug-in-v1-2-3" {
		t.Errorf("slugifyPrompt(%q, 64) = %q, want %q", "Fix bug in v1.2.3!", got, "fix-bug-in-v1-2-3")
	}
}
