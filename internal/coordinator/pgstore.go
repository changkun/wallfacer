package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"latere.ai/x/wallfacer/internal/speccomment"
)

// pgSchema is the durable schema for cloud-authoritative spec comments. It is
// applied idempotently on store init. Spec comments are the one relay-not-mirror
// exception, so their system of record is Postgres (durable), not the Valkey
// cache. The flat thread/comment shape mirrors internal/speccomment so the
// future git-export path is a serializer, not a transform.
const pgSchema = `
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
CREATE INDEX IF NOT EXISTS spec_comment_threads_repo
  ON spec_comment_threads (org_id, workspace_id);

CREATE TABLE IF NOT EXISTS spec_comments (
  id         text PRIMARY KEY,
  thread_id  text NOT NULL REFERENCES spec_comment_threads(id) ON DELETE CASCADE,
  parent_id  text NOT NULL DEFAULT '',
  author_sub text NOT NULL,
  body       text NOT NULL,
  created_at timestamptz NOT NULL,
  edited_at  timestamptz
);
CREATE INDEX IF NOT EXISTS spec_comments_thread ON spec_comments (thread_id);
`

// pgStore is the durable, Postgres-backed CommentStore. Every query is scoped by
// org (the tenant boundary), so a forged repo claim never reaches another org's
// threads. It shares the wallfacer database with the projection rollups.
type pgStore struct {
	pool *pgxpool.Pool
}

// NewPostgresCommentStore opens a pgx pool against dsn, applies the schema, and
// returns a durable comment store. dsn is WALLFACER_DATABASE_URL.
func NewPostgresCommentStore(ctx context.Context, dsn string) (CommentStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if _, err := pool.Exec(ctx, pgSchema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &pgStore{pool: pool}, nil
}

// Close releases the pool.
func (s *pgStore) Close() { s.pool.Close() }

func (s *pgStore) ThreadsForRepo(ctx context.Context, org, repo string) ([]speccomment.Thread, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, org_id, workspace_id, spec_path, anchor, author_sub, created_at,
		       resolved, resolved_by, resolved_at, status
		FROM spec_comment_threads
		WHERE org_id = $1 AND workspace_id = $2
		ORDER BY id`, org, repo)
	if err != nil {
		return nil, err
	}
	threads, byID, err := scanThreads(rows)
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 {
		return nil, nil
	}
	if err := s.loadComments(ctx, byID); err != nil {
		return nil, err
	}
	out := make([]speccomment.Thread, len(threads))
	for i, id := range threads {
		out[i] = *byID[id]
	}
	return out, nil
}

func (s *pgStore) GetThread(ctx context.Context, org, threadID string) (speccomment.Thread, bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, org_id, workspace_id, spec_path, anchor, author_sub, created_at,
		       resolved, resolved_by, resolved_at, status
		FROM spec_comment_threads
		WHERE org_id = $1 AND id = $2`, org, threadID)
	if err != nil {
		return speccomment.Thread{}, false, err
	}
	order, byID, err := scanThreads(rows)
	if err != nil {
		return speccomment.Thread{}, false, err
	}
	if len(order) == 0 {
		return speccomment.Thread{}, false, nil
	}
	if err := s.loadComments(ctx, byID); err != nil {
		return speccomment.Thread{}, false, err
	}
	return *byID[order[0]], true, nil
}

// PutThread upserts a thread and its comments in one transaction. v1 never
// deletes comments, so absent comments are not pruned.
func (s *pgStore) PutThread(ctx context.Context, t speccomment.Thread) error {
	anchorJSON, err := json.Marshal(t.Anchor)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO spec_comment_threads
			(id, org_id, workspace_id, spec_path, anchor, author_sub, created_at, resolved, resolved_by, resolved_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO UPDATE SET
			anchor = EXCLUDED.anchor,
			resolved = EXCLUDED.resolved,
			resolved_by = EXCLUDED.resolved_by,
			resolved_at = EXCLUDED.resolved_at,
			status = EXCLUDED.status`,
		t.ID, t.OrgID, t.WorkspaceID, t.SpecPath, anchorJSON, t.AuthorSub, t.CreatedAt,
		t.Resolved, t.ResolvedBy, nullableTime(t.ResolvedAt), t.Status); err != nil {
		return err
	}

	for _, c := range t.Comments {
		if _, err := tx.Exec(ctx, `
			INSERT INTO spec_comments (id, thread_id, parent_id, author_sub, body, created_at, edited_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (id) DO UPDATE SET
				body = EXCLUDED.body,
				edited_at = EXCLUDED.edited_at`,
			c.ID, t.ID, c.ParentID, c.AuthorSub, c.Body, c.CreatedAt, nullableTime(c.EditedAt)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// loadComments fills the Comments slice of each thread in byID with one query.
func (s *pgStore) loadComments(ctx context.Context, byID map[string]*speccomment.Thread) error {
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, thread_id, parent_id, author_sub, body, created_at, edited_at
		FROM spec_comments
		WHERE thread_id = ANY($1)
		ORDER BY id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c speccomment.Comment
		var editedAt *time.Time
		if err := rows.Scan(&c.ID, &c.ThreadID, &c.ParentID, &c.AuthorSub, &c.Body, &c.CreatedAt, &editedAt); err != nil {
			return err
		}
		if editedAt != nil {
			c.EditedAt = *editedAt
		}
		if t := byID[c.ThreadID]; t != nil {
			t.Comments = append(t.Comments, c)
		}
	}
	return rows.Err()
}

// scanThreads scans thread rows into a slice (preserving order) and an id index.
func scanThreads(rows pgx.Rows) (order []string, byID map[string]*speccomment.Thread, err error) {
	defer rows.Close()
	byID = make(map[string]*speccomment.Thread)
	for rows.Next() {
		var t speccomment.Thread
		var anchorJSON []byte
		var resolvedAt *time.Time
		if err := rows.Scan(&t.ID, &t.OrgID, &t.WorkspaceID, &t.SpecPath, &anchorJSON,
			&t.AuthorSub, &t.CreatedAt, &t.Resolved, &t.ResolvedBy, &resolvedAt, &t.Status); err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal(anchorJSON, &t.Anchor); err != nil {
			return nil, nil, err
		}
		if resolvedAt != nil {
			t.ResolvedAt = *resolvedAt
		}
		tc := t
		byID[t.ID] = &tc
		order = append(order, t.ID)
	}
	return order, byID, rows.Err()
}

// nullableTime maps a zero time.Time to SQL NULL (and back on read), so an
// unresolved thread or an unedited comment stores NULL rather than the zero ts.
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
