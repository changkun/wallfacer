package coordinator

import (
	"context"
	"os"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/speccomment"
)

// storeFactory builds a fresh CommentStore for one subtest run.
type storeFactory struct {
	name string
	make func(t *testing.T) CommentStore
}

// commentStores returns the stores under contract test: memStore always, and
// the Postgres store when WALLFACER_TEST_DATABASE_URL points at a throwaway
// database (skipped otherwise, the same shape latere's other pg stores use).
func commentStores(_ *testing.T) []storeFactory {
	out := []storeFactory{{
		name: "mem",
		make: func(*testing.T) CommentStore { return NewMemCommentStore() },
	}}
	if dsn := os.Getenv("WALLFACER_TEST_DATABASE_URL"); dsn != "" {
		out = append(out, storeFactory{
			name: "postgres",
			make: func(t *testing.T) CommentStore {
				st, err := NewPostgresCommentStore(context.Background(), dsn)
				if err != nil {
					t.Fatalf("open pg store: %v", err)
				}
				// Isolate each run by clearing the tables.
				if pg, ok := st.(*pgStore); ok {
					if _, err := pg.pool.Exec(context.Background(),
						"TRUNCATE spec_comments, spec_comment_threads"); err != nil {
						t.Fatalf("truncate: %v", err)
					}
				}
				return st
			},
		})
	}
	return out
}

func sampleThread(id, org, repo string) speccomment.Thread {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return speccomment.Thread{
		ID: id, OrgID: org, WorkspaceID: repo, SpecPath: "x.md",
		Anchor:    speccomment.Anchor{LineHash: "h", ExactText: "a line", SectionPath: []string{"S"}},
		AuthorSub: "u1", CreatedAt: now, Status: speccomment.StatusActive,
		Comments: []speccomment.Comment{{ID: id + "c1", ThreadID: id, AuthorSub: "u1", Body: "hi", CreatedAt: now}},
	}
}

func TestCommentStoreContract(t *testing.T) {
	const org, repo = "org_1", "github.com/acme/widgets"
	ctx := context.Background()

	for _, sf := range commentStores(t) {
		t.Run(sf.name, func(t *testing.T) {
			st := sf.make(t)

			// Put + Get round trips with comments intact.
			if err := st.PutThread(ctx, sampleThread("01A", org, repo)); err != nil {
				t.Fatalf("put: %v", err)
			}
			got, ok, err := st.GetThread(ctx, org, "01A")
			if err != nil || !ok {
				t.Fatalf("get: ok=%v err=%v", ok, err)
			}
			if len(got.Comments) != 1 || got.Comments[0].Body != "hi" {
				t.Fatalf("comments not round-tripped: %+v", got.Comments)
			}
			if got.Status != speccomment.StatusActive {
				t.Fatalf("status = %q, want active", got.Status)
			}

			// Update (resolve) replaces, not duplicates.
			upd := got
			upd.Resolved = true
			upd.ResolvedBy = "u2"
			upd.ResolvedAt = time.Now().UTC().Truncate(time.Millisecond)
			upd.Status = speccomment.StatusResolved
			if err := st.PutThread(ctx, upd); err != nil {
				t.Fatalf("update: %v", err)
			}
			got2, _, _ := st.GetThread(ctx, org, "01A")
			if !got2.Resolved || got2.ResolvedBy != "u2" || got2.Status != speccomment.StatusResolved {
				t.Fatalf("update not persisted: %+v", got2)
			}

			// ThreadsForRepo lists by org+repo; another org never appears.
			if err := st.PutThread(ctx, sampleThread("01B", org, repo)); err != nil {
				t.Fatalf("put 2: %v", err)
			}
			if err := st.PutThread(ctx, sampleThread("01C", "org_other", repo)); err != nil {
				t.Fatalf("put cross-org: %v", err)
			}
			list, err := st.ThreadsForRepo(ctx, org, repo)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(list) != 2 || list[0].ID != "01A" || list[1].ID != "01B" {
				t.Fatalf("list = %v, want [01A 01B] sorted, org-scoped", ids(list))
			}

			// Cross-tenant get is a miss even with the right id.
			if _, ok, _ := st.GetThread(ctx, "org_other", "01A"); ok {
				t.Fatal("cross-tenant GetThread returned a thread")
			}
		})
	}
}

func ids(ts []speccomment.Thread) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}
