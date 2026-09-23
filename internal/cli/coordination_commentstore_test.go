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

// TestServingDatabaseURLPrefersThePool pins which endpoint the coordinator's
// pool opens. The pooled DSN is the claim on the cluster that does not move
// with the replica count, so it wins whenever it is set.
func TestServingDatabaseURLPrefersThePool(t *testing.T) {
	t.Setenv("WALLFACER_DATABASE_URL", "postgres://u:p@directhost.invalid:5432/wallfacer")
	t.Setenv("WALLFACER_DATABASE_POOL_URL", "postgres://u:p@pooled.invalid:25061/wallfacer-pool")
	want := "postgres://u:p@pooled.invalid:25061/wallfacer-pool"
	if got := servingDatabaseURL(); got != want {
		t.Fatalf("servingDatabaseURL() = %q, want the pooled endpoint %q", got, want)
	}
}

// TestServingDatabaseURLFallsBackToDirect pins the behavior of a deployment
// whose Secret does not carry the pooled key: it serves on the direct endpoint
// exactly as it did before the pooler existed.
func TestServingDatabaseURLFallsBackToDirect(t *testing.T) {
	t.Setenv("WALLFACER_DATABASE_URL", "postgres://u:p@directhost.invalid:5432/wallfacer")
	t.Setenv("WALLFACER_DATABASE_POOL_URL", "")
	want := "postgres://u:p@directhost.invalid:5432/wallfacer"
	if got := servingDatabaseURL(); got != want {
		t.Fatalf("servingDatabaseURL() = %q, want the direct endpoint %q", got, want)
	}
}
