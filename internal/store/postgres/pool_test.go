package postgres

import (
	"context"
	"strings"
	"testing"
)

// poolHost and directHost stand in for the two endpoints. They are distinct so
// an error that names one proves which DSN the failing client was handed.
const (
	poolHost   = "pooled.invalid"
	directHost = "directhost.invalid"
)

// TestNewOpensServingOnTheFirstURL proves the first argument reaches pgxpool.
// An unparseable serving DSN fails while opening the pool, before the migrator
// is consulted at all.
func TestNewOpensServingOnTheFirstURL(t *testing.T) {
	t.Parallel()
	_, err := New(context.Background(), "://not-a-dsn", "postgres://u:p@"+directHost+":5432/wallfacer")
	if err == nil {
		t.Fatal("New() = nil error, want a failure from the serving DSN")
	}
	if !strings.Contains(err.Error(), "pool:") {
		t.Fatalf("New() error = %v, want it to name the pool open", err)
	}
	if strings.Contains(err.Error(), directHost) {
		t.Fatalf("New() error = %v, want the pool never to open the direct host %q", err, directHost)
	}
}

// TestNewMigratesOnTheSecondURL proves the migrator is handed the direct DSN
// and never the pooled one. golang-migrate holds a session-scoped advisory
// lock across statements, which a transaction-mode pooler cannot keep on one
// backend, so a migrator pointed at the pool loses the lock it believes it
// holds and two of them can write the schema version at once. The serving DSN
// here is parseable and would be dialed: if it reached the migrator, the
// error would name the pooled host. This runs for the ten seconds pgxmigrate
// retries the database open before it gives up.
func TestNewMigratesOnTheSecondURL(t *testing.T) {
	t.Parallel()
	_, err := New(context.Background(), "postgres://u:p@"+poolHost+":25061/wallfacer-pool", "://not-a-dsn")
	if err == nil {
		t.Fatal("New() = nil error, want a failure from the migration DSN")
	}
	if !strings.Contains(err.Error(), "migrate:") {
		t.Fatalf("New() error = %v, want it to name the migration step", err)
	}
	if strings.Contains(err.Error(), poolHost) {
		t.Fatalf("New() error = %v, want the migrator never to see the pooled host %q", err, poolHost)
	}
}
