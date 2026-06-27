package coordinator

import (
	"context"
	"errors"
	"sync"
	"testing"

	"latere.ai/x/wallfacer/internal/speccomment"
)

// failingPutStore wraps a CommentStore and fails PutThread once armed, to
// exercise the persist-before-fanout ordering.
type failingPutStore struct {
	CommentStore
	fail bool
}

func (f *failingPutStore) PutThread(ctx context.Context, t speccomment.Thread) error {
	if f.fail {
		return errors.New("store write failed")
	}
	return f.CommentStore.PutThread(ctx, t)
}

// captureSender records frames pushed to an instance, for asserting fan-out.
type captureSender struct {
	mu     sync.Mutex
	events []speccomment.Event
}

func (s *captureSender) Send(v any) error {
	ev, ok := v.(speccomment.Event)
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return nil
}

func (s *captureSender) all() []speccomment.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]speccomment.Event(nil), s.events...)
}

func (s *captureSender) opsFor(op string) []speccomment.Event {
	var out []speccomment.Event
	for _, e := range s.all() {
		if e.Op == op {
			out = append(out, e)
		}
	}
	return out
}

const testRepo = "github.com/acme/widgets"

// joinInst registers an instance serving testRepo with a capturing sender.
func joinInst(reg *Registry, id, sub, org string) *captureSender {
	sender := &captureSender{}
	reg.Join(Instance{
		Principal: Principal{Sub: sub, OrgID: org},
		Manifest:  NewManifest(id, "host", "dev", []WorkspaceRef{{Remote: testRepo}}, []string{"comments"}),
		Conn:      sender,
	})
	return sender
}

func createEvent(specPath, body string, forgedAuthor string) speccomment.Event {
	return speccomment.Event{
		Op:   speccomment.OpCreate,
		Repo: testRepo,
		Thread: &speccomment.Thread{
			SpecPath:  specPath,
			AuthorSub: forgedAuthor, // must be ignored; coordinator stamps from JWT
			Anchor:    speccomment.Anchor{LineHash: "h1", ExactText: "a line"},
			Comments:  []speccomment.Comment{{Body: body, AuthorSub: forgedAuthor}},
		},
	}
}

func TestCommentCreateStampsPrincipalAndFansOut(t *testing.T) {
	reg := NewRegistry()
	svc := NewCommentService(NewMemCommentStore(), reg)

	author := joinInst(reg, "inst_a", "u_alice", "org_1")
	peer := joinInst(reg, "inst_b", "u_bob", "org_1")
	other := joinInst(reg, "inst_c", "u_carol", "org_2") // different tenant, same repo

	p := Principal{Sub: "u_alice", OrgID: "org_1"}
	if err := svc.Apply(context.Background(), p, createEvent("specs/x.md", "first!", "u_attacker")); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Fan-out reaches the author and the same-org peer, never the other tenant.
	if got := len(author.opsFor(speccomment.OpCreate)); got != 1 {
		t.Fatalf("author received %d create events, want 1", got)
	}
	if got := len(peer.opsFor(speccomment.OpCreate)); got != 1 {
		t.Fatalf("same-org peer received %d create events, want 1", got)
	}
	if got := len(other.all()); got != 0 {
		t.Fatalf("cross-tenant instance received %d events, want 0 (org isolation)", got)
	}

	// Attribution and tenant come from the principal, never the wire.
	ev := author.opsFor(speccomment.OpCreate)[0]
	if ev.Thread.AuthorSub != "u_alice" {
		t.Fatalf("thread author = %q, want u_alice (JWT), not the forged value", ev.Thread.AuthorSub)
	}
	if ev.Thread.OrgID != "org_1" {
		t.Fatalf("thread org = %q, want org_1", ev.Thread.OrgID)
	}
	root, _ := ev.Thread.Root()
	if root.AuthorSub != "u_alice" {
		t.Fatalf("root comment author = %q, want u_alice", root.AuthorSub)
	}
	if ev.Thread.ID == "" || root.ID == "" {
		t.Fatal("coordinator must mint thread and comment ids")
	}
	if ev.Thread.Status != speccomment.StatusActive {
		t.Fatalf("new thread status = %q, want active", ev.Thread.Status)
	}
}

func TestCommentReplyAndResolve(t *testing.T) {
	reg := NewRegistry()
	store := NewMemCommentStore()
	svc := NewCommentService(store, reg)
	author := joinInst(reg, "inst_a", "u_alice", "org_1")
	p := Principal{Sub: "u_alice", OrgID: "org_1"}

	if err := svc.Apply(context.Background(), p, createEvent("specs/x.md", "root", "")); err != nil {
		t.Fatalf("create: %v", err)
	}
	threadID := author.opsFor(speccomment.OpCreate)[0].Thread.ID

	// Reply.
	reply := speccomment.Event{Op: speccomment.OpReply, Repo: testRepo,
		Comment: &speccomment.Comment{ThreadID: threadID, Body: "a reply"}}
	if err := svc.Apply(context.Background(), p, reply); err != nil {
		t.Fatalf("reply: %v", err)
	}
	replyEv := author.opsFor(speccomment.OpReply)
	if len(replyEv) != 1 || len(replyEv[0].Thread.Comments) != 2 {
		t.Fatalf("reply thread has %d comments, want 2", len(replyEv[0].Thread.Comments))
	}

	// Resolve then reopen.
	resolve := speccomment.Event{Op: speccomment.OpResolve, Repo: testRepo, Thread: &speccomment.Thread{ID: threadID}}
	if err := svc.Apply(context.Background(), p, resolve); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	rEv := author.opsFor(speccomment.OpResolve)
	if len(rEv) != 1 || !rEv[0].Thread.Resolved || rEv[0].Thread.Status != speccomment.StatusResolved {
		t.Fatalf("resolve did not mark thread resolved: %+v", rEv)
	}
	if rEv[0].Thread.ResolvedBy != "u_alice" {
		t.Fatalf("resolved_by = %q, want u_alice", rEv[0].Thread.ResolvedBy)
	}

	reopen := speccomment.Event{Op: speccomment.OpReopen, Repo: testRepo, Thread: &speccomment.Thread{ID: threadID}}
	if err := svc.Apply(context.Background(), p, reopen); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	reEv := author.opsFor(speccomment.OpReopen)
	if len(reEv) != 1 || reEv[0].Thread.Resolved || reEv[0].Thread.Status != speccomment.StatusActive {
		t.Fatalf("reopen did not reactivate thread: %+v", reEv)
	}
}

// TestCommentMarkOutdated verifies that OpOutdated files a thread away as the
// terminal "outdated" status and fans the authoritative thread out. Before
// OpOutdated was wired through Apply, this op returned "unsupported op".
func TestCommentMarkOutdated(t *testing.T) {
	reg := NewRegistry()
	store := NewMemCommentStore()
	svc := NewCommentService(store, reg)
	author := joinInst(reg, "inst_a", "u_alice", "org_1")
	p := Principal{Sub: "u_alice", OrgID: "org_1"}

	if err := svc.Apply(context.Background(), p, createEvent("specs/x.md", "root", "")); err != nil {
		t.Fatalf("create: %v", err)
	}
	threadID := author.opsFor(speccomment.OpCreate)[0].Thread.ID

	outdated := speccomment.Event{Op: speccomment.OpOutdated, Repo: testRepo, Thread: &speccomment.Thread{ID: threadID}}
	if err := svc.Apply(context.Background(), p, outdated); err != nil {
		t.Fatalf("outdated: %v", err)
	}
	oEv := author.opsFor(speccomment.OpOutdated)
	if len(oEv) != 1 || oEv[0].Thread.Status != speccomment.StatusOutdated {
		t.Fatalf("outdated did not file thread away: %+v", oEv)
	}

	// A member of another tenant must not be able to outdate the thread.
	mallory := Principal{Sub: "u_mallory", OrgID: "org_2"}
	if err := svc.Apply(context.Background(), mallory, outdated); err == nil {
		t.Fatal("expected cross-tenant outdate to be refused")
	}
}

// TestCommentResolveDoesNotFanOutOnStoreFailure verifies that setResolved
// persists before fanning out (like create/reply). If the store write fails,
// peers must NOT have been pushed the authoritative thread, otherwise
// originator, peers, and store would disagree.
func TestCommentResolveDoesNotFanOutOnStoreFailure(t *testing.T) {
	reg := NewRegistry()
	store := &failingPutStore{CommentStore: NewMemCommentStore()}
	svc := NewCommentService(store, reg)
	author := joinInst(reg, "inst_a", "u_alice", "org_1")
	p := Principal{Sub: "u_alice", OrgID: "org_1"}

	if err := svc.Apply(context.Background(), p, createEvent("specs/x.md", "root", "")); err != nil {
		t.Fatalf("create: %v", err)
	}
	threadID := author.opsFor(speccomment.OpCreate)[0].Thread.ID

	// Arm the store to fail the resolve's PutThread.
	store.fail = true
	resolve := speccomment.Event{Op: speccomment.OpResolve, Repo: testRepo, Thread: &speccomment.Thread{ID: threadID}}
	if err := svc.Apply(context.Background(), p, resolve); err == nil {
		t.Fatal("expected resolve to fail when the store write fails")
	}

	if got := author.opsFor(speccomment.OpResolve); len(got) != 0 {
		t.Fatalf("fanned out %d resolve events despite a failed store write; want 0", len(got))
	}
}

func TestCommentCrossTenantCannotResolve(t *testing.T) {
	reg := NewRegistry()
	store := NewMemCommentStore()
	svc := NewCommentService(store, reg)
	joinInst(reg, "inst_a", "u_alice", "org_1")
	alice := Principal{Sub: "u_alice", OrgID: "org_1"}
	if err := svc.Apply(context.Background(), alice, createEvent("specs/x.md", "root", "")); err != nil {
		t.Fatalf("create: %v", err)
	}
	threads, _ := store.ThreadsForRepo(context.Background(), "org_1", testRepo)
	threadID := threads[0].ID

	// A principal in another org cannot even find the thread (scoped lookup), so
	// resolve fails as "unknown thread" — cross-tenant access is impossible.
	mallory := Principal{Sub: "u_mallory", OrgID: "org_evil"}
	resolve := speccomment.Event{Op: speccomment.OpResolve, Repo: testRepo, Thread: &speccomment.Thread{ID: threadID}}
	if err := svc.Apply(context.Background(), mallory, resolve); err == nil {
		t.Fatal("cross-tenant resolve must be rejected")
	}
	// The thread is untouched.
	got, _, _ := store.GetThread(context.Background(), "org_1", threadID)
	if got.Resolved {
		t.Fatal("cross-tenant principal resolved another org's thread")
	}
}

// TestCommentPersonalScope is the regression for the 202-then-empty bug: a
// signed-in user with no org (personal scope) must be able to create and see
// their own comments, and a different personal user must be isolated.
func TestCommentPersonalScope(t *testing.T) {
	reg := NewRegistry()
	store := NewMemCommentStore()
	svc := NewCommentService(store, reg)

	// Alice, personal (no org), on her own instance serving the repo.
	alice := joinInst(reg, "inst_alice", "u_alice", "") // empty org = personal
	p := Principal{Sub: "u_alice", OrgID: ""}
	if err := svc.Apply(context.Background(), p, createEvent("specs/x.md", "personal note", "")); err != nil {
		t.Fatalf("personal create rejected: %v", err)
	}
	// It fans back to Alice's own instance (this is what GET reads from).
	if got := len(alice.opsFor(speccomment.OpCreate)); got != 1 {
		t.Fatalf("personal author received %d create events, want 1 (was the 202-then-empty bug)", got)
	}
	ev := alice.opsFor(speccomment.OpCreate)[0]
	if ev.Thread.OrgID != "u:u_alice" {
		t.Fatalf("personal thread tenant = %q, want u:u_alice", ev.Thread.OrgID)
	}

	// A different personal user (Bob) serving the same repo is isolated: he does
	// not receive Alice's personal comment, and a sync gives him nothing.
	bob := &captureSender{}
	svc.SyncTo(context.Background(), Instance{
		Principal: Principal{Sub: "u_bob", OrgID: ""},
		Manifest:  NewManifest("inst_bob", "host", "dev", []WorkspaceRef{{Remote: testRepo}}, nil),
		Conn:      bob,
	})
	sync := bob.opsFor(speccomment.OpSync)
	if len(sync) != 1 || len(sync[0].Threads) != 0 {
		t.Fatalf("a different personal user saw %v, want an empty sync (isolation)", sync)
	}
}

func TestCommentSyncOnJoin(t *testing.T) {
	reg := NewRegistry()
	store := NewMemCommentStore()
	svc := NewCommentService(store, reg)

	// Seed a thread via an existing author instance.
	joinInst(reg, "inst_a", "u_alice", "org_1")
	alice := Principal{Sub: "u_alice", OrgID: "org_1"}
	if err := svc.Apply(context.Background(), alice, createEvent("specs/x.md", "root", "")); err != nil {
		t.Fatalf("create: %v", err)
	}

	// A new instance for the same org+repo connects: sync pushes the thread set.
	newcomer := &captureSender{}
	inst := Instance{
		Principal: Principal{Sub: "u_alice", OrgID: "org_1"},
		Manifest:  NewManifest("inst_new", "host", "dev", []WorkspaceRef{{Remote: testRepo}}, []string{"comments"}),
		Conn:      newcomer,
	}
	svc.SyncTo(context.Background(), inst)

	sync := newcomer.opsFor(speccomment.OpSync)
	if len(sync) != 1 || len(sync[0].Threads) != 1 {
		t.Fatalf("sync pushed %v, want one sync frame with one thread", sync)
	}
	if sync[0].Repo != testRepo {
		t.Fatalf("sync repo = %q, want %q", sync[0].Repo, testRepo)
	}

	// A different-org instance serving the same repo syncs to an empty set.
	otherOrg := &captureSender{}
	svc.SyncTo(context.Background(), Instance{
		Principal: Principal{Sub: "u_carol", OrgID: "org_2"},
		Manifest:  NewManifest("inst_c", "host", "dev", []WorkspaceRef{{Remote: testRepo}}, nil),
		Conn:      otherOrg,
	})
	s2 := otherOrg.opsFor(speccomment.OpSync)
	if len(s2) != 1 || len(s2[0].Threads) != 0 {
		t.Fatalf("cross-org sync = %v, want one empty sync frame (tenant isolation)", s2)
	}
}
