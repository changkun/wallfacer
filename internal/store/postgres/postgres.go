// Package postgres is the single owner of the wallfacer Postgres pool. It opens
// that pool on the serving endpoint and runs embedded golang-migrate versioned
// migrations on the direct one at New, then hands the live pool
// to each durable domain store (spec comments today; projection rollups and
// future storage next). Domain stores take the pool and never open or close it;
// the migration sequence is one linear, embedded set of numbered files with one
// schema_migrations table. "Generic for extension" is exactly this shape: a
// shared pool plus one sequence, no per-module registry or namespace. A new
// durable consumer adds a numbered migration file and a constructor that takes
// *pgxpool.Pool.
package postgres

import (
	"context"
	"embed"
	"fmt"
	"strings"

	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// driver
	"github.com/jackc/pgx/v5/pgxpool"

	"latere.ai/x/pkg/pgxmigrate"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store owns the wallfacer Postgres pool and its migrations.
type Store struct {
	pool *pgxpool.Pool
}

// New opens the serving pool on servingURL, runs embedded migrations to the
// latest version over migrationURL, then pings the pool. Migration failure is
// fatal: the caller refuses to start with an unknown schema state rather than
// running against a half-applied schema.
//
// The two strings are separate because migrations take a session-scoped
// advisory lock and hold it across statements, while a transaction-mode pooler
// reassigns the backend between transactions. So the migrator stays on the
// direct endpoint and serving traffic goes through the pooler, where the pool
// size and not the replica count is this service's claim on the shared
// cluster. The pool is opened first because pgxpool.New parses the DSN and
// hands back a handle without dialing: with MinConns at the pgx default of 0
// no backend is taken until the first query, which is after the migrations.
func New(ctx context.Context, servingURL, migrationURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, servingURL)
	if err != nil {
		return nil, fmt.Errorf("pool: %w", err)
	}
	if err := runMigrations(migrationURL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func runMigrations(migrationURL string) error {
	// golang-migrate's pgx/v5 driver expects a pgx5:// scheme. The shared
	// pgxmigrate helper runs Up and closes migrate's own pool; the pgx5
	// driver is blank-imported above since pgxmigrate selects it by scheme.
	return pgxmigrate.Up(pgxScheme(migrationURL), migrationsFS, "migrations")
}

// pgxScheme rewrites a postgres:// (or postgresql://) DSN to the pgx5:// scheme
// golang-migrate uses for the pgx/v5 driver.
func pgxScheme(dsn string) string {
	for _, p := range []string{"postgres://", "postgresql://"} {
		if rest, ok := strings.CutPrefix(dsn, p); ok {
			return "pgx5://" + rest
		}
	}
	return dsn
}

// Pool exposes the underlying pgx pool so domain stores share one pool and one
// migration sequence. Callers must not close it; the Store owns its lifecycle.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Close releases the pool. The Store is the sole owner; domain stores built from
// Pool() never close it.
func (s *Store) Close() { s.pool.Close() }
