package handler

import (
	"encoding/json"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/speccomment"
)

func frame(ev speccomment.Event) []byte {
	ev.Type = coordinator.FrameSpecComment
	b, _ := json.Marshal(ev)
	return b
}

func TestCommentRelaySyncAndUpsert(t *testing.T) {
	r := NewCommentRelay()
	const repo = "github.com/acme/widgets"

	// A sync frame seeds the repo's thread set.
	r.HandleInbound(frame(speccomment.Event{Op: speccomment.OpSync, Repo: repo, Threads: []speccomment.Thread{
		{ID: "01A", OrgID: "o1", WorkspaceID: repo, SpecPath: "x.md", Status: speccomment.StatusActive},
		{ID: "01B", OrgID: "o1", WorkspaceID: repo, SpecPath: "x.md", Status: speccomment.StatusActive},
	}}))
	if got := r.ThreadsForRepo(repo); len(got) != 2 || got[0].ID != "01A" || got[1].ID != "01B" {
		t.Fatalf("after sync: %+v, want two sorted threads", got)
	}

	// A create frame upserts a single thread.
	r.HandleInbound(frame(speccomment.Event{Op: speccomment.OpCreate, Repo: repo, Thread: &speccomment.Thread{
		ID: "01C", OrgID: "o1", WorkspaceID: repo, SpecPath: "x.md", Status: speccomment.StatusActive,
	}}))
	if got := r.ThreadsForRepo(repo); len(got) != 3 || got[2].ID != "01C" {
		t.Fatalf("after create: %+v, want three threads", got)
	}

	// A resolve frame replaces the existing thread by id, not appends.
	r.HandleInbound(frame(speccomment.Event{Op: speccomment.OpResolve, Repo: repo, Thread: &speccomment.Thread{
		ID: "01A", OrgID: "o1", WorkspaceID: repo, SpecPath: "x.md", Status: speccomment.StatusResolved, Resolved: true,
	}}))
	got := r.ThreadsForRepo(repo)
	if len(got) != 3 {
		t.Fatalf("resolve changed thread count: %d, want 3", len(got))
	}
	if !got[0].Resolved || got[0].Status != speccomment.StatusResolved {
		t.Fatalf("resolve did not update thread 01A: %+v", got[0])
	}
}

func TestCommentRelaySubmitForwards(t *testing.T) {
	r := NewCommentRelay()

	// Without a send-up wired, Submit reports the coordinator unavailable.
	if err := r.Submit(speccomment.Event{Op: speccomment.OpCreate, Repo: "r"}); err != ErrCoordinatorUnavailable {
		t.Fatalf("Submit without sendUp = %v, want ErrCoordinatorUnavailable", err)
	}

	var sent speccomment.Event
	r.SetSendUp(func(ev speccomment.Event) error { sent = ev; return nil })
	if err := r.Submit(speccomment.Event{Op: speccomment.OpReply, Repo: "r"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if sent.Op != speccomment.OpReply || sent.Type != coordinator.FrameSpecComment {
		t.Fatalf("forwarded event = %+v, want a reply tagged with the frame type", sent)
	}
}

func TestCommentRelayBroadcast(t *testing.T) {
	r := NewCommentRelay()
	id, ch := r.Subscribe()
	defer r.Unsubscribe(id)

	r.HandleInbound(frame(speccomment.Event{Op: speccomment.OpCreate, Repo: "r", Thread: &speccomment.Thread{ID: "z", WorkspaceID: "r"}}))

	select {
	case ev := <-ch:
		if ev.Op != speccomment.OpCreate || ev.Thread == nil || ev.Thread.ID != "z" {
			t.Fatalf("broadcast event = %+v, want the create", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive the broadcast")
	}
}
