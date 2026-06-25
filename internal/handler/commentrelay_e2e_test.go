package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/coordinator"
	coordclient "latere.ai/x/wallfacer/internal/coordinator/client"
	"latere.ai/x/wallfacer/internal/speccomment"
)

// TestCommentRoundTripThroughCoordinator exercises the full instance<->coordinator
// path with the REAL connector and a live in-process coordinator: a browser-side
// create op goes up the WebSocket, the coordinator mints+persists+fans out, and
// the authoritative thread echoes back down and lands in the relay cache. This is
// the round trip that "I posted a comment but it never appears" hinges on, and the
// existing relay tests skip it by calling HandleInbound/Submit directly.
func TestCommentRoundTripThroughCoordinator(t *testing.T) {
	const (
		repo = "github.com/changkun/wallfacer"
		org  = "8ef527d8"
		sub  = "u_changkun"
	)

	// Live coordinator with the comment capability attached, behind a
	// context-injected principal (the seam the auth middleware fills).
	reg := coordinator.NewRegistry()
	coord := coordinator.NewCoordinator(reg)
	coord.SetCommentService(coordinator.NewCommentService(coordinator.NewMemCommentStore(), reg))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithIdentity(r.Context(), &auth.Identity{Sub: sub, OrgID: org})
		coord.HandleWS(w, r.WithContext(ctx))
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// Instance side: the relay wired to the real connector, exactly as the host
	// wires them in production (SetSendUp -> Send, OnInbound -> HandleInbound).
	relay := NewCommentRelay()
	conn := coordclient.NewConnector(coordclient.Config{
		URL:     wsURL,
		Token:   func() (string, bool) { return "tok", true },
		OptedIn: func() bool { return true },
		Manifest: func() coordinator.Manifest {
			return coordinator.NewManifest("inst_test", "laptop", "v1",
				[]coordinator.WorkspaceRef{{Remote: repo, LocalKey: "k"}},
				[]string{"comments"})
		},
		OnInbound:    relay.HandleInbound,
		PingInterval: time.Second,
	})
	relay.SetSendUp(func(ev speccomment.Event) error { return conn.Send(ev) })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go conn.Run(ctx)

	// Wait until the instance has registered (the connector dialed and sent its
	// manifest) before submitting; Send needs a live connection.
	waitForCond(t, 3*time.Second, "instance to register", func() bool { return reg.Len() == 1 })

	// Browser create op for a spec in the served repo.
	if err := relay.Submit(speccomment.Event{
		Op:   speccomment.OpCreate,
		Repo: repo,
		Thread: &speccomment.Thread{
			SpecPath: "specs/00-overview.md",
			Comments: []speccomment.Comment{{Body: "does this round-trip?"}},
		},
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// The authoritative thread must echo back down and land in the cache.
	waitForCond(t, 3*time.Second, "create to echo back into the relay cache", func() bool { return len(relay.ThreadsForRepo(repo)) == 1 })

	got := relay.ThreadsForRepo(repo)
	if len(got) != 1 {
		t.Fatalf("ThreadsForRepo(%q) = %d threads, want 1", repo, len(got))
	}
	th := got[0]
	if th.OrgID != org {
		t.Errorf("thread OrgID = %q, want %q (coordinator stamps the principal org)", th.OrgID, org)
	}
	if th.WorkspaceID != repo {
		t.Errorf("thread WorkspaceID = %q, want %q", th.WorkspaceID, repo)
	}
	if len(th.Comments) != 1 || th.Comments[0].Body != "does this round-trip?" {
		t.Errorf("thread comments = %+v, want the submitted body", th.Comments)
	}
}
