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
	if ev.Repo == "" || p.OrgID == "" {
		return fmt.Errorf("%w: missing repo or org", errInvalid)
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
		OrgID:       p.OrgID,
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
	s.fanout(p.OrgID, ev.Repo, speccomment.OpCreate, t)
	return nil
}

func (s *CommentService) reply(ctx context.Context, p Principal, ev speccomment.Event) error {
	if ev.Comment == nil || ev.Comment.ThreadID == "" || ev.Comment.Body == "" {
		return fmt.Errorf("%w: reply needs a thread_id and body", errInvalid)
	}
	t, ok, err := s.store.GetThread(ctx, p.OrgID, ev.Comment.ThreadID)
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
	s.fanout(p.OrgID, t.WorkspaceID, speccomment.OpReply, t)
	return nil
}

func (s *CommentService) setResolved(ctx context.Context, p Principal, ev speccomment.Event, resolved bool) error {
	if ev.Thread == nil || ev.Thread.ID == "" {
		return fmt.Errorf("%w: resolve/reopen needs a thread id", errInvalid)
	}
	t, ok, err := s.store.GetThread(ctx, p.OrgID, ev.Thread.ID)
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
	if resolved {
		t.Resolved = true
		t.ResolvedBy = p.Sub
		t.ResolvedAt = now
		t.Status = speccomment.StatusResolved
		s.fanout(p.OrgID, t.WorkspaceID, speccomment.OpResolve, t)
	} else {
		t.Resolved = false
		t.ResolvedBy = ""
		t.ResolvedAt = time.Time{}
		t.Status = speccomment.StatusActive
		s.fanout(p.OrgID, t.WorkspaceID, speccomment.OpReopen, t)
	}
	return s.store.PutThread(ctx, t)
}

// canResolve gates resolve/reopen. v1: any member of the owning org may resolve
// (collaboration default), enforced structurally by the org boundary. Per-role
// gating (viewers read-only) re-homes from multi-user-collaboration's RBAC and
// is a follow-up; the seam is here so it slots in without touching callers.
func canResolve(p Principal, t speccomment.Thread) bool {
	return p.OrgID == t.OrgID
}

// fanout pushes the authoritative thread to every instance serving repo in the
// SAME org, including the originator (which adopts the coordinator's ids). The
// org filter is the tenant boundary: InstancesForRemote spans orgs, so a thread
// must never reach an instance outside its org.
func (s *CommentService) fanout(org, repo, op string, t speccomment.Thread) {
	thread := t
	ev := speccomment.Event{Type: FrameSpecComment, Op: op, Repo: repo, Thread: &thread}
	for _, inst := range s.reg.InstancesForRemote(repo) {
		if inst.Principal.OrgID != org || inst.Conn == nil {
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
		threads, err := s.store.ThreadsForRepo(ctx, inst.Principal.OrgID, repo)
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
