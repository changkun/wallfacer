package coordinator

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"latere.ai/x/wallfacer/internal/speccomment"
	"latere.ai/x/wallfacer/internal/store/postgres"
)

// TestPutThreadUnderExecMode pins how the anchor encoding reaches
// spec_comment_threads.anchor.
//
// The column is jsonb and NOT NULL, so the encoding binds as a string. A byte
// slice is sent as bytea and reaches the server as a hex literal, which jsonb
// refuses with SQLSTATE 22P02 once a parameter is encoded without the server
// describing the statement first.
//
// The pool opens in exec mode, the strictest of the query exec modes: it sends
// a statement without asking the server to describe it, so every parameter is
// encoded from its Go type alone. A mode that describes the statement first
// accepts a byte slice, so a test on one proves nothing about the binding.
// Exec mode is pinned here because a binding that holds under it is correct
// under every mode, whichever one a deployment's DSN carries. Migrations stay
// on the plain DSN, which is the split the production wiring uses.
//
// The test needs a real server, so it reads WALLFACER_TEST_DATABASE_URL and
// skips when it is unset, the same gate the store's own migration tests use.
func TestPutThreadUnderExecMode(t *testing.T) {
	dsn := os.Getenv("WALLFACER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("WALLFACER_TEST_DATABASE_URL unset")
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}

	st, err := postgres.New(t.Context(), dsn+sep+"default_query_exec_mode=exec", dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	pool := st.Pool()
	if got := pool.Config().ConnConfig.DefaultQueryExecMode; got != pgx.QueryExecModeExec {
		t.Fatalf("pool exec mode = %v, want %v; the DSN did not reach pgx", got, pgx.QueryExecModeExec)
	}

	const id = "thr_exec_mode"
	// context.Background, because the test's own context is already cancelled
	// by the time a cleanup runs.
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DELETE FROM spec_comments WHERE thread_id = $1`,
			`DELETE FROM spec_comment_threads WHERE id = $1`,
		} {
			if _, err := pool.Exec(context.Background(), stmt, id); err != nil {
				t.Errorf("clean up the thread: %v", err)
			}
		}
	})

	now := time.Now().UTC().Truncate(time.Microsecond)
	thread := speccomment.Thread{
		ID: id, OrgID: "org-exec", WorkspaceID: "github.com/example/repo",
		SpecPath: "specs/001-exec-mode.md", AuthorSub: "issuer|exec",
		CreatedAt: now, Status: "open",
		Anchor: speccomment.Anchor{
			SectionPath: []string{"Design", "Storage"},
			LineHash:    "abc123", Prefix: "the ", Suffix: " column",
			ExactText: "anchor", LineHint: 12,
		},
		Comments: []speccomment.Comment{{
			ID: "cmt_exec_mode", ThreadID: id, AuthorSub: "issuer|exec",
			Body: "written through a pool that describes nothing", CreatedAt: now,
		}},
	}

	store := NewPostgresCommentStore(pool)
	// Twice: the second pass takes the ON CONFLICT path, which binds the same
	// anchor parameter again.
	for _, pass := range []string{"insert", "update"} {
		if err := store.PutThread(t.Context(), thread); err != nil {
			t.Fatalf("PutThread (%s pass) through the exec-mode pool: %v", pass, err)
		}
	}

	var anchor string
	if err := pool.QueryRow(t.Context(), `SELECT anchor::text FROM spec_comment_threads WHERE id = $1`, id).Scan(&anchor); err != nil {
		t.Fatalf("read back the anchor: %v", err)
	}
	if !strings.Contains(anchor, "Storage") {
		t.Errorf("anchor = %s, want the section path the thread carried", anchor)
	}

	got, ok, err := store.GetThread(t.Context(), thread.OrgID, id)
	if err != nil || !ok {
		t.Fatalf("GetThread = %v, %v; want the thread PutThread wrote", ok, err)
	}
	if got.Anchor.LineHash != thread.Anchor.LineHash {
		t.Errorf("anchor line hash = %q, want %q", got.Anchor.LineHash, thread.Anchor.LineHash)
	}
}
