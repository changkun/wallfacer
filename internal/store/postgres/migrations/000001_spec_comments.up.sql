-- spec-comments.md: cloud-authoritative inline spec comments, the one
-- relay-not-mirror exception. The flat thread/comment shape mirrors
-- internal/speccomment so the future git-export path is a serializer, not a
-- transform. IF NOT EXISTS is load-bearing: deployments that ran the pre-migration
-- inline schema already have these tables but no schema_migrations row, so this
-- migration must be a no-op against them that just stamps version 1.
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
