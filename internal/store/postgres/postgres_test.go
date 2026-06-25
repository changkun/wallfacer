package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

// preMigrationSchema is the shape a deployment carries if it ran the original
// inline-schema path (before this migration framework): the comment tables exist
// but there is no schema_migrations version marker.
const preMigrationSchema = `
CREATE TABLE IF NOT EXISTS spec_comment_threads (
  id           text PRIMARY KEY,
  org_id       text NOT NULL,
  workspace_id text NOT NULL,
  spec_path    text NOT NULL,
  anchor       jsonb NOT NULL,
  author_sub   text NOT NULL,
  created_at   timestamptz NOT NULL,
  resolved     boolean NOT NULL DEFAULT false,
  resolved_by  text NOT NULL DEFAULT '',
  resolved_at  timestamptz,
  status       text NOT NULL
);
CREATE TABLE IF NOT EXISTS spec_comments (
  id         text PRIMARY KEY,
  thread_id  text NOT NULL REFERENCES spec_comment_threads(id) ON DELETE CASCADE,
  parent_id  text NOT NULL DEFAULT '',
  author_sub text NOT NULL,
  body       text NOT NULL,
  created_at timestamptz NOT NULL,
  edited_at  timestamptz
);
`

// testDSN returns the throwaway-database DSN or skips. CI provisions no Postgres,
// so these run only when WALLFACER_TEST_DATABASE_URL is set, the same gate the
// coordinator comment-store contract test uses.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("WALLFACER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("WALLFACER_TEST_DATABASE_URL unset")
	}
	return dsn
}

// dropAll resets the database to a clean pre-migration state: no domain tables
// and no schema_migrations marker, so the next New starts from version 0.
func dropAll(ctx context.Context, t *testing.T, dsn string) {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	for _, stmt := range []string{
		"DROP TABLE IF EXISTS spec_comments",
		"DROP TABLE IF EXISTS spec_comment_threads",
		"DROP TABLE IF EXISTS schema_migrations",
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func schemaVersion(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (version int64, dirty bool) {
	t.Helper()
	if err := pool.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	return version, dirty
}

func tableExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var present bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", name).Scan(&present); err != nil {
		t.Fatalf("to_regclass %s: %v", name, err)
	}
	return present
}

// TestNew_AppliesMigrations: New on an empty database creates the comment tables
// and stamps version 1.
func TestNew_AppliesMigrations(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	dropAll(ctx, t, dsn)

	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer st.Close()

	if !tableExists(ctx, t, st.Pool(), "spec_comment_threads") || !tableExists(ctx, t, st.Pool(), "spec_comments") {
		t.Fatal("comment tables not created")
	}
	if v, dirty := schemaVersion(ctx, t, st.Pool()); v != 1 || dirty {
		t.Fatalf("version = %d dirty = %v, want 1 clean", v, dirty)
	}
}

// TestNew_ExistingTablesUpgrade is the load-bearing case: a deployment that ran
// the original inline schema has the tables but no schema_migrations row. New
// must run 000001 against it as a no-op that stamps version 1, never erroring or
// going dirty. It fails if 000001 is ever rewritten to non-idempotent DDL.
func TestNew_ExistingTablesUpgrade(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	dropAll(ctx, t, dsn)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	if _, err := pool.Exec(ctx, preMigrationSchema); err != nil {
		t.Fatalf("seed pre-migration schema: %v", err)
	}
	pool.Close()

	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New against existing tables: %v", err)
	}
	defer st.Close()
	if v, dirty := schemaVersion(ctx, t, st.Pool()); v != 1 || dirty {
		t.Fatalf("version = %d dirty = %v, want 1 clean", v, dirty)
	}
}

// TestNew_Idempotent: a second New against an already-migrated database is a
// no-op (ErrNoChange tolerated) and keeps version 1.
func TestNew_Idempotent(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	dropAll(ctx, t, dsn)

	st1, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New 1: %v", err)
	}
	st1.Close()

	st2, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New 2 (should be a no-op): %v", err)
	}
	defer st2.Close()
	if v, dirty := schemaVersion(ctx, t, st2.Pool()); v != 1 || dirty {
		t.Fatalf("version = %d dirty = %v after re-run, want 1 clean", v, dirty)
	}
}

// TestDownMigration: the 000001 down migration drops both tables, child first,
// with no foreign-key error.
func TestDownMigration(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	dropAll(ctx, t, dsn)

	if err := runMigrations(dsn); err != nil {
		t.Fatalf("up: %v", err)
	}
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("iofs: %v", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, pgxScheme(dsn))
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Down(); err != nil {
		t.Fatalf("down: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	if tableExists(ctx, t, pool, "spec_comments") || tableExists(ctx, t, pool, "spec_comment_threads") {
		t.Fatal("down migration left tables behind")
	}
}
