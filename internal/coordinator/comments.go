package coordinator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"latere.ai/x/wallfacer/internal/speccomment"
)

// CommentService is the coordinator's authoritative spec-comment capability. It
// applies create/reply/resolve/reopen ops a local instance relays up the
// connection: it mints the ULID, stamps AuthorSub and OrgID from the validated
// connection principal (never the wire), persists to the durable store, then
// fans the authoritative thread out to other instances serving the same repo in
// the same org. It is the one place that writes comment state.
type CommentService struct {
	store CommentStore
	reg   *Registry
	log   *slog.Logger
	now   func() time.Time
}

// NewCommentService returns a comment capability backed by store and reg.
func NewCommentService(store CommentStore, reg *Registry) *CommentService {
	return &CommentService{store: store, reg: reg, log: slog.Default(), now: time.Now}
}

// errInvalid is returned for malformed ops (missing repo, body, or target).
var errInvalid = errors.New("invalid comment op")

// Apply validates, persists, and fans out one op from a connection. The
// principal is the validated JWT identity; org and author are taken from it, so
// a client cannot forge attribution or reach another tenant. Apply returns an
// error for a malformed op or a permission denial; the caller relays it back to
// the originating browser.
func (s *CommentService) Apply(ctx context.Context, p Principal, ev speccomment.Event) error {
	if ev.Repo == "" || p.Sub == "" {
		return fmt.Errorf("%w: missing repo or principal", errInvalid)
	}
	switch ev.Op {
	case speccomment.OpCreate:
		return s.create(ctx, p, ev)
	case speccomment.OpReply:
		return s.reply(ctx, p, ev)
	case speccomment.OpResolve:
		return s.setResolved(ctx, p, ev, true)
	case speccomment.OpReopen:
		return s.setResolved(ctx, p, ev, false)
	case speccomment.OpOutdated:
		return s.setOutdated(ctx, p, ev)
	default:
		return fmt.Errorf("%w: unsupported op %q", errInvalid, ev.Op)
	}
}

func (s *CommentService) create(ctx context.Context, p Principal, ev speccomment.Event) error {
	if ev.Thread == nil || ev.Thread.SpecPath == "" {
		return fmt.Errorf("%w: create needs a thread with a spec_path", errInvalid)
	}
	root, ok := ev.Thread.Root()
	if !ok || root.Body == "" {
		return fmt.Errorf("%w: create needs a root comment body", errInvalid)
	}
	now := s.now()
	t := speccomment.Thread{
		ID:          speccomment.NewID(),
		OrgID:       tenantKey(p),
		WorkspaceID: ev.Repo,
		SpecPath:    ev.Thread.SpecPath,
		Anchor:      ev.Thread.Anchor,
		AuthorSub:   p.Sub,
		CreatedAt:   now,
		Status:      speccomment.StatusActive,
	}
	t.Comments = []speccomment.Comment{{
		ID:        speccomment.NewID(),
		ThreadID:  t.ID,
		AuthorSub: p.Sub,
		Body:      root.Body,
		CreatedAt: now,
	}}
	if err := s.store.PutThread(ctx, t); err != nil {
		return err
	}
	s.fanout(tenantKey(p), ev.Repo, speccomment.OpCreate, t)
	return nil
}

func (s *CommentService) reply(ctx context.Context, p Principal, ev speccomment.Event) error {
	if ev.Comment == nil || ev.Comment.ThreadID == "" || ev.Comment.Body == "" {
		return fmt.Errorf("%w: reply needs a thread_id and body", errInvalid)
	}
	t, ok, err := s.store.GetThread(ctx, tenantKey(p), ev.Comment.ThreadID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: unknown thread", errInvalid)
	}
	now := s.now()
	t.Comments = append(t.Comments, speccomment.Comment{
		ID:        speccomment.NewID(),
		ThreadID:  t.ID,
		ParentID:  ev.Comment.ParentID,
		AuthorSub: p.Sub,
		Body:      ev.Comment.Body,
		CreatedAt: now,
	})
	if err := s.store.PutThread(ctx, t); err != nil {
		return err
	}
	s.fanout(tenantKey(p), t.WorkspaceID, speccomment.OpReply, t)
	return nil
}

func (s *CommentService) setResolved(ctx context.Context, p Principal, ev speccomment.Event, resolved bool) error {
	if ev.Thread == nil || ev.Thread.ID == "" {
		return fmt.Errorf("%w: resolve/reopen needs a thread id", errInvalid)
	}
	t, ok, err := s.store.GetThread(ctx, tenantKey(p), ev.Thread.ID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: unknown thread", errInvalid)
	}
	if !canResolve(p, t) {
		return fmt.Errorf("not permitted to resolve thread")
	}
	now := s.now()
	op := speccomment.OpResolve
	if resolved {
		t.Resolved = true
		t.ResolvedBy = p.Sub
		t.ResolvedAt = now
		t.Status = speccomment.StatusResolved
	} else {
		t.Resolved = false
		t.ResolvedBy = ""
		t.ResolvedAt = time.Time{}
		t.Status = speccomment.StatusActive
		op = speccomment.OpReopen
	}
	// Persist before fanning out, matching create/reply. Fanning out first
	// would push the authoritative thread to peers even when the store write
	// then fails, leaving originator, peers, and store disagreeing.
	if err := s.store.PutThread(ctx, t); err != nil {
		return err
	}
	s.fanout(tenantKey(p), t.WorkspaceID, op, t)
	return nil
}

// setOutdated files a thread away as no-longer-relevant: a terminal verdict,
// distinct from resolve (which is reversible by reopen). The thread leaves the
// triage list and every spec highlight — repositionThread short-circuits a
// StatusOutdated thread to a non-orphaned, non-inline result — but it is
// retained, not hard-deleted. Reachable from the triage list. Gated by the same
// tenant boundary as resolve.
func (s *CommentService) setOutdated(ctx context.Context, p Principal, ev speccomment.Event) error {
	if ev.Thread == nil || ev.Thread.ID == "" {
		return fmt.Errorf("%w: outdated needs a thread id", errInvalid)
	}
	t, ok, err := s.store.GetThread(ctx, tenantKey(p), ev.Thread.ID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: unknown thread", errInvalid)
	}
	if !canResolve(p, t) {
		return fmt.Errorf("not permitted to outdate thread")
	}
	t.Status = speccomment.StatusOutdated
	// Persist before fanning out, matching setResolved: a failed store write
	// must not leave peers ahead of the store.
	if err := s.store.PutThread(ctx, t); err != nil {
		return err
	}
	s.fanout(tenantKey(p), t.WorkspaceID, speccomment.OpOutdated, t)
	return nil
}

// tenantKey is the isolation key for a principal: the org when present, else the
// user (a personal-scope tenant). It keys the store and the fan-out so personal
// comments stay private to one user's own instances while org comments are shared
// across the org. Cross-tenant access is structurally impossible either way.
func tenantKey(p Principal) string {
	if p.OrgID != "" {
		return p.OrgID
	}
	return "u:" + p.Sub
}

// canResolve gates resolve/reopen. v1: any member of the owning tenant may
// resolve (collaboration default), enforced structurally by the tenant boundary.
// Per-role gating (viewers read-only) re-homes from multi-user-collaboration's
// RBAC and is a follow-up; the seam is here so it slots in without touching callers.
func canResolve(p Principal, t speccomment.Thread) bool {
	return tenantKey(p) == t.OrgID
}

// fanout pushes the authoritative thread to every instance serving repo in the
// SAME tenant, including the originator (which adopts the coordinator's ids). The
// tenant filter is the boundary: InstancesForRemote spans tenants, so a thread
// must never reach an instance outside its tenant. `tenant` is tenantKey(p).
func (s *CommentService) fanout(tenant, repo, op string, t speccomment.Thread) {
	thread := t
	ev := speccomment.Event{Type: FrameSpecComment, Op: op, Repo: repo, Thread: &thread}
	for _, inst := range s.reg.InstancesForRemote(repo) {
		if tenantKey(inst.Principal) != tenant || inst.Conn == nil {
			continue
		}
		if err := inst.Conn.Send(ev); err != nil {
			s.log.Debug("coordinator: comment fan-out send failed", "instance", inst.ID(), "err", err)
		}
	}
}

// SyncTo pushes the full thread set for each repo an instance serves, scoped to
// the instance's org, right after it registers. An instance gets its repos'
// comments on connect without a separate fetch (the relay-not-mirror load path).
func (s *CommentService) SyncTo(ctx context.Context, inst Instance) {
	if inst.Conn == nil {
		return
	}
	for _, repo := range inst.Manifest.Remotes() {
		threads, err := s.store.ThreadsForRepo(ctx, tenantKey(inst.Principal), repo)
		if err != nil {
			s.log.Warn("coordinator: comment sync query failed", "repo", repo, "err", err)
			continue
		}
		ev := speccomment.Event{Type: FrameSpecComment, Op: speccomment.OpSync, Repo: repo, Threads: threads}
		if err := inst.Conn.Send(ev); err != nil {
			s.log.Debug("coordinator: comment sync send failed", "instance", inst.ID(), "err", err)
		}
	}
}
