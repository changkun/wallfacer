package cli

import (
	"errors"
	"strings"
	"testing"
)

// TestCommentStoreFallbackReason locks in the loud-on-non-durable contract: a
// missing DSN must yield a non-empty operator reason (the silent path that let a
// non-durable comment store reach prod unnoticed), a Postgres open error must
// surface, and a healthy DSN must report durable ("").
func TestCommentStoreFallbackReason(t *testing.T) {
	t.Run("unset DSN is a loud non-durable reason", func(t *testing.T) {
		got := commentStoreFallbackReason("", nil)
		if got == "" {
			t.Fatal("unset DSN returned no reason; the non-durable fallback would be silent")
		}
		if !strings.Contains(got, "WALLFACER_DATABASE_URL") || !strings.Contains(got, "in-memory") {
			t.Errorf("reason = %q, want it to name the missing env and the in-memory consequence", got)
		}
	})

	t.Run("Postgres open error surfaces", func(t *testing.T) {
		got := commentStoreFallbackReason("postgres://x", errors.New("dial timeout"))
		if !strings.Contains(got, "dial timeout") {
			t.Errorf("reason = %q, want it to carry the open error", got)
		}
	})

	t.Run("healthy DSN reports durable", func(t *testing.T) {
		if got := commentStoreFallbackReason("postgres://x", nil); got != "" {
			t.Errorf("reason = %q, want empty (durable) when the store opened cleanly", got)
		}
	})
}
