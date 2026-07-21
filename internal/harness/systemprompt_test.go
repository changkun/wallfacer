package harness

import "testing"

func TestPrependSystemPrompt(t *testing.T) {
	if got := prependSystemPrompt("user", ""); got != "user" {
		t.Errorf("empty system prompt: got %q, want %q", got, "user")
	}
	if got := prependSystemPrompt("user", "sys"); got != "sys\n\n---\n\nuser" {
		t.Errorf("got %q, want %q", got, "sys\n\n---\n\nuser")
	}
}
