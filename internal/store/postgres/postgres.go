// Package postgres is the single owner of the wallfacer Postgres pool. It runs
// embedded golang-migrate versioned migrations at New, then hands the live pool
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

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store owns the wallfacer Postgres pool and its migrations.
type Store struct {
	pool *pgxpool.Pool
}

// New runs embedded migrations to the latest version, then opens and pings a
// pool against dsn. Migration failure is fatal: the caller refuses to start with
// an unknown schema state rather than running against a half-applied schema.
func New(ctx context.Context, dsn string) (*Store, error) {
	if err := runMigrations(dsn); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func runMigrations(dsn string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	// golang-migrate's pgx/v5 driver expects a pgx5:// scheme.
	m, err := migrate.NewWithSourceInstance("iofs", src, pgxScheme(dsn))
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

// pgxScheme rewrites a postgres:// (or postgresql://) DSN to the pgx5:// scheme
// golang-migrate uses for the pgx/v5 driver.
func pgxScheme(dsn string) string {
	for _, p := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(dsn, p) {
			return "pgx5://" + dsn[len(p):]
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
