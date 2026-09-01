package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"latere.ai/x/pkg/wait/waittest"

	"latere.ai/x/pkg/authkit"

	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/coordinator/client"
	"latere.ai/x/wallfacer/internal/speccomment"
)

// TestCommentEndToEndCrossInstance is the literal user flow: teammate A on one
// instance marks text in a spec and comments; teammate B on a different
// instance, on the same repo in the same org, sees the comment, without either
// touching the other directly. It wires the real WSS connection, the coordinator
// (authoritative, mints the id and stamps the author), and both instances'
// relays, end to end.
func TestCommentEndToEndCrossInstance(t *testing.T) {
	const org, repo = "org_e2e", "github.com/acme/widgets"

	// Coordinator: registry + comment capability over an in-memory store.
	reg := coordinator.NewRegistry()
	coord := coordinator.NewCoordinator(reg)
	coord.SetCommentService(coordinator.NewCommentService(coordinator.NewMemCommentStore(), reg))

	// The accept handler, behind a principal derived from the bearer token, so
	// the two connectors authenticate as two different teammates in one org.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		ctx := auth.WithIdentity(r.Context(), &authkit.Identity{Sub: sub, OrgID: org})
		coord.HandleWS(w, r.WithContext(ctx))
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// Two instances, each with its own relay (the instance-side cache + fan-out).
	relayA := NewCommentRelay()
	relayB := NewCommentRelay()

	connFor := func(sub string, relay *CommentRelay) *client.Connector {
		c := client.NewConnector(client.Config{
			URL:       wsURL,
			Token:     func() (string, bool) { return sub, true },
			OptedIn:   func() bool { return true },
			OnInbound: relay.HandleInbound,
			Manifest: func() coordinator.Manifest {
				return coordinator.NewManifest("inst_"+sub, sub, "dev",
					[]coordinator.WorkspaceRef{{Remote: repo}}, []string{"comments"})
			},
		})
		relay.SetSendUp(func(ev speccomment.Event) error { return c.Send(ev) })
		return c
	}

	ctx := t.Context()
	connA := connFor("u_alice", relayA)
	connB := connFor("u_bob", relayB)
	go connA.Run(ctx)
	go connB.Run(ctx)

	waittest.For(t, 5*time.Second, func() bool { return reg.Len() == 2 })

	// B subscribes to its browser SSE stream, exactly as a board would.
	subID, events := relayB.Subscribe()
	defer relayB.Unsubscribe(subID)

	// A marks a line and comments (the browser POST -> relay.Submit -> WSS).
	if err := relayA.Submit(speccomment.Event{
		Op:   speccomment.OpCreate,
		Repo: repo,
		Thread: &speccomment.Thread{
			SpecPath: "cloud/x.md",
			Anchor:   speccomment.Anchor{LineHash: "h", ExactText: "the anchored line"},
			Comments: []speccomment.Comment{{Body: "is this still accurate?"}},
		},
	}); err != nil {
		t.Fatalf("A submit: %v", err)
	}

	// B's browser receives the new thread over SSE within seconds, no reload.
	select {
	case ev := <-events:
		if ev.Op != speccomment.OpCreate || ev.Thread == nil {
			t.Fatalf("B received unexpected event: %+v", ev)
		}
		if ev.Thread.AuthorSub != "u_alice" {
			t.Fatalf("comment author = %q, want u_alice (stamped by the coordinator)", ev.Thread.AuthorSub)
		}
		root, _ := ev.Thread.Root()
		if root.Body != "is this still accurate?" {
			t.Fatalf("comment body = %q, want the text A typed", root.Body)
		}
		if ev.Thread.ID == "" {
			t.Fatal("coordinator must mint a thread id")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("B did not receive A's comment within 5s")
	}

	// And B's cache (what a fresh GET would return) now holds the thread.
	waittest.For(t, 5*time.Second, func() bool { return len(relayB.ThreadsForRepo(repo)) == 1 })
	if got := relayB.ThreadsForRepo(repo); got[0].WorkspaceID != repo {
		t.Fatalf("B thread repo = %q, want %q", got[0].WorkspaceID, repo)
	}
}
