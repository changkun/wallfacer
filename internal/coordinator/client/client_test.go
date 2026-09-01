package client

import (
	"context"
	"latere.ai/x/pkg/wait/waittest"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/coordinator"
)

// acceptHarness stands up the real coordinator accept handler behind a
// context-injected principal (the seam auth middleware uses), so the connector
// is exercised against the genuine server side without a JWKS server.
func acceptHarness(t *testing.T, sub, org string) (wsURL string, reg *coordinator.Registry) {
	t.Helper()
	reg = coordinator.NewRegistry()
	coord := coordinator.NewCoordinator(reg)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithIdentity(r.Context(), &authkit.Identity{Sub: sub, OrgID: org})
		coord.HandleWS(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http"), reg
}

func manifestFor(id string) func() coordinator.Manifest {
	return func() coordinator.Manifest {
		return coordinator.NewManifest(id, "laptop", "wallfacer/test",
			[]coordinator.WorkspaceRef{{Remote: "github.com/o/r"}}, []string{"presence"})
	}
}

// runConnector starts a connector with a cancellable context registered for
// cleanup, so no goroutine outlives the test.
func runConnector(t *testing.T, cfg Config) {
	t.Helper()
	c := NewConnector(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.Run(ctx)
}

func TestConnectorRegistersWhenEnabled(t *testing.T) {
	url, reg := acceptHarness(t, "u_1", "org_1")
	runConnector(t, Config{
		URL:      url,
		Token:    func() (string, bool) { return "tok", true },
		OptedIn:  func() bool { return true },
		Manifest: manifestFor("inst_client"),
	})

	waittest.For(t, 3*time.Second, func() bool { return reg.Len() == 1 })
	snap := reg.Snapshot("org_1")
	if len(snap) != 1 || snap[0].ID() != "inst_client" {
		t.Fatalf("registry = %+v, want one inst_client", snap)
	}
}

func TestConnectorGateBlocksWhenOptedOut(t *testing.T) {
	url, reg := acceptHarness(t, "u_1", "org_1")
	runConnector(t, Config{
		URL:      url,
		Token:    func() (string, bool) { return "tok", true },
		OptedIn:  func() bool { return false }, // signed in but not opted in
		Manifest: manifestFor("inst_client"),
	})

	// The gate must keep the connector from dialing at all.
	time.Sleep(400 * time.Millisecond)
	if reg.Len() != 0 {
		t.Fatalf("opted-out connector dialed: registry Len = %d, want 0", reg.Len())
	}
}

func TestConnectorGateBlocksWhenSignedOut(t *testing.T) {
	url, reg := acceptHarness(t, "u_1", "org_1")
	runConnector(t, Config{
		URL:      url,
		Token:    func() (string, bool) { return "", false }, // opted in but not signed in
		OptedIn:  func() bool { return true },
		Manifest: manifestFor("inst_client"),
	})

	time.Sleep(400 * time.Millisecond)
	if reg.Len() != 0 {
		t.Fatalf("signed-out connector dialed: registry Len = %d, want 0", reg.Len())
	}
}

func TestConnectorTearsDownOnOptOut(t *testing.T) {
	url, reg := acceptHarness(t, "u_1", "org_1")
	var optedIn atomic.Bool
	optedIn.Store(true)
	runConnector(t, Config{
		URL:          url,
		Token:        func() (string, bool) { return "tok", true },
		OptedIn:      optedIn.Load,
		Manifest:     manifestFor("inst_client"),
		PingInterval: 100 * time.Millisecond,
	})

	waittest.For(t, 3*time.Second, func() bool { return reg.Len() == 1 })

	// Flip the opt-in off: the live connection must tear down promptly and the
	// instance leave the registry (clean close, not a 60 s liveness wait).
	optedIn.Store(false)
	waittest.For(t, 3*time.Second, func() bool { return reg.Len() == 0 })
}

// TestConnectorFlagsAuthRejection covers the silent-failure that hid the
// coordination "503 / invalid audience" bug: a coordinator that refuses the
// token with 401 must leave AuthRejected() set (and never Connected()), so the
// status surface can show a fixable auth error instead of an endless spinner.
func TestConnectorFlagsAuthRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid audience", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := NewConnector(Config{
		URL:         wsURL,
		Token:       func() (string, bool) { return "tok", true },
		OptedIn:     func() bool { return true },
		Manifest:    manifestFor("inst_client"),
		BaseBackoff: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.Run(ctx)

	waittest.For(t, 3*time.Second, c.AuthRejected)
	if c.Connected() {
		t.Fatal("Connected() = true after a 401 dial")
	}
}

func TestJitterFullRange(t *testing.T) {
	c := NewConnector(Config{Rand: func() float64 { return 0 }})
	if got := c.jitter(10 * time.Second); got != 0 {
		t.Errorf("jitter at rand=0 = %v, want 0", got)
	}
	c = NewConnector(Config{Rand: func() float64 { return 0.5 }})
	if got := c.jitter(10 * time.Second); got != 5*time.Second {
		t.Errorf("jitter at rand=0.5 = %v, want 5s", got)
	}
}
