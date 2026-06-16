package coordinator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"latere.ai/x/wallfacer/internal/auth"
)

// acceptHarness stands up the accept handler behind a context-injected
// principal (the same seam auth middleware uses, without a real JWKS server) and
// returns the ws:// dial URL plus the registry the handler feeds.
func acceptHarness(t *testing.T, p Principal) (string, *Registry) {
	t.Helper()
	reg := NewRegistry()
	coord := NewCoordinator(reg)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithIdentity(r.Context(), &auth.Identity{Sub: p.Sub, OrgID: p.OrgID})
		coord.HandleWS(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http"), reg
}

func closeConn(c *websocket.Conn) { _ = c.Close(websocket.StatusNormalClosure, "") }

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func writeFrame(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// waitFor polls cond up to 2 s; registration and teardown are async (the server
// reads a frame, then mutates the registry), so tests synchronize on the
// observable registry state rather than a sleep.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func TestAcceptRegistersManifest(t *testing.T) {
	url, reg := acceptHarness(t, Principal{Sub: "u_1", OrgID: "org_1"})
	conn := dial(t, url)
	defer closeConn(conn)

	m := NewManifest("inst_a", "laptop", "wallfacer/v1",
		[]WorkspaceRef{{Remote: "github.com/o/r", LocalKey: "k"}},
		[]string{"presence", "comments"})
	writeFrame(t, conn, m)

	waitFor(t, func() bool { return reg.Len() == 1 }, "instance to register")

	snap := reg.Snapshot("org_1")
	if len(snap) != 1 {
		t.Fatalf("Snapshot(org_1) = %d instances, want 1", len(snap))
	}
	got := snap[0]
	if got.Principal.Sub != "u_1" || got.Principal.OrgID != "org_1" {
		t.Errorf("principal = %+v, want {u_1 org_1}", got.Principal)
	}
	if got.ID() != "inst_a" {
		t.Errorf("instance id = %q, want inst_a", got.ID())
	}
	if remotes := got.Manifest.Remotes(); len(remotes) != 1 || remotes[0] != "github.com/o/r" {
		t.Errorf("remotes = %v, want [github.com/o/r]", remotes)
	}
}

func TestAcceptDerivesPrincipalFromContextNotManifest(t *testing.T) {
	// The handler's identity is u_real; the manifest carries no principal field,
	// but even a hand-rolled frame trying to smuggle one must not win.
	url, reg := acceptHarness(t, Principal{Sub: "u_real", OrgID: "org_real"})
	conn := dial(t, url)
	defer closeConn(conn)

	// A raw manifest frame with extra principal/org keys the struct ignores.
	writeFrame(t, conn, map[string]any{
		"type":        FrameManifest,
		"instance_id": "inst_b",
		"principal":   "u_forged",
		"org":         "org_forged",
	})

	waitFor(t, func() bool { return reg.Len() == 1 }, "instance to register")
	snap := reg.Snapshot("org_real")
	if len(snap) != 1 {
		t.Fatalf("forged org leaked: Snapshot(org_real) = %d, want 1", len(snap))
	}
	if got := reg.Snapshot("org_forged"); len(got) != 0 {
		t.Fatalf("forged org visible: Snapshot(org_forged) = %d, want 0", len(got))
	}
	if snap[0].Principal.Sub != "u_real" {
		t.Errorf("principal sub = %q, want u_real (from JWT, not manifest)", snap[0].Principal.Sub)
	}
}

func TestAcceptManifestUpdate(t *testing.T) {
	url, reg := acceptHarness(t, Principal{Sub: "u_1", OrgID: "org_1"})
	conn := dial(t, url)
	defer closeConn(conn)

	writeFrame(t, conn, NewManifest("inst_a", "laptop", "v1", nil, []string{"presence"}))
	waitFor(t, func() bool { return reg.Len() == 1 }, "initial register")

	// Workspace-set change: re-send the manifest with a new remote.
	writeFrame(t, conn, NewManifest("inst_a", "laptop", "v1",
		[]WorkspaceRef{{Remote: "github.com/o/r2"}}, []string{"presence"}))

	waitFor(t, func() bool {
		s := reg.Snapshot("org_1")
		return len(s) == 1 && len(s[0].Manifest.Remotes()) == 1 && s[0].Manifest.Remotes()[0] == "github.com/o/r2"
	}, "manifest to update with new remote")

	if reg.Len() != 1 {
		t.Fatalf("update added a second instance: Len = %d, want 1", reg.Len())
	}
}

func TestAcceptLeaveOnClose(t *testing.T) {
	url, reg := acceptHarness(t, Principal{Sub: "u_1", OrgID: "org_1"})
	conn := dial(t, url)
	writeFrame(t, conn, NewManifest("inst_a", "laptop", "v1", nil, nil))
	waitFor(t, func() bool { return reg.Len() == 1 }, "register")

	closeConn(conn)
	waitFor(t, func() bool { return reg.Len() == 0 }, "instance to leave on close")
}

func TestAcceptRejectsNonManifestFirstFrame(t *testing.T) {
	url, reg := acceptHarness(t, Principal{Sub: "u_1", OrgID: "org_1"})
	conn := dial(t, url)
	defer closeConn(conn)

	// First frame is a reserved capability type, not a manifest: the handshake
	// must reject it and register nothing.
	writeFrame(t, conn, map[string]any{"type": FramePresence})

	// Give the server a moment; it should close without registering.
	time.Sleep(200 * time.Millisecond)
	if reg.Len() != 0 {
		t.Fatalf("registered on a non-manifest first frame: Len = %d, want 0", reg.Len())
	}
}

func TestAcceptIgnoresUnknownFrameType(t *testing.T) {
	url, reg := acceptHarness(t, Principal{Sub: "u_1", OrgID: "org_1"})
	conn := dial(t, url)
	defer closeConn(conn)

	writeFrame(t, conn, NewManifest("inst_a", "laptop", "v1", nil, nil))
	waitFor(t, func() bool { return reg.Len() == 1 }, "register")

	// An unknown frame type must not drop the connection or the registration.
	writeFrame(t, conn, map[string]any{"type": "totally-unknown-future-type"})

	time.Sleep(200 * time.Millisecond)
	if reg.Len() != 1 {
		t.Fatalf("unknown frame dropped the instance: Len = %d, want 1", reg.Len())
	}
}
