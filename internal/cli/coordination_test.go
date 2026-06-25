package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"latere.ai/x/pkg/oidc"

	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/workspace"
)

// fakeTokenStore is an in-memory authkit.TokenStore for exercising the token
// callback without touching disk.
type fakeTokenStore struct {
	tok    *oauth2.Token
	loaded int
}

func (s *fakeTokenStore) Load() (*oauth2.Token, error) { s.loaded++; return s.tok, nil }
func (s *fakeTokenStore) Save(t *oauth2.Token) error   { s.tok = t; return nil }
func (s *fakeTokenStore) Clear() error                 { s.tok = nil; return nil }

func TestCoordinationTokenFunc(t *testing.T) {
	ctx := context.Background()

	t.Run("signed out yields not-ok", func(t *testing.T) {
		tf := coordinationTokenFunc(ctx, &fakeTokenStore{}, nil)
		if _, ok := tf(); ok {
			t.Fatal("expected not signed in with no stored token")
		}
	})

	t.Run("valid token returns access token", func(t *testing.T) {
		store := &fakeTokenStore{tok: &oauth2.Token{
			AccessToken: "live-jwt",
			Expiry:      time.Now().Add(time.Hour),
		}}
		tf := coordinationTokenFunc(ctx, store, nil)
		got, ok := tf()
		if !ok || got != "live-jwt" {
			t.Fatalf("token = %q, ok = %v; want live-jwt, true", got, ok)
		}
	})

	t.Run("expired token without refresh yields not-ok", func(t *testing.T) {
		store := &fakeTokenStore{tok: &oauth2.Token{
			AccessToken: "stale",
			Expiry:      time.Now().Add(-time.Hour),
		}}
		// nil oidc client => no refresh path; expired token must not be sent.
		tf := coordinationTokenFunc(ctx, store, nil)
		if _, ok := tf(); ok {
			t.Fatal("expired token without refresh must report signed out")
		}
	})
}

func TestSessionTokenBridgeSync(t *testing.T) {
	store := &fakeTokenStore{}
	b := newSessionTokenBridge(nil, store) // client nil is fine; sync is exercised directly
	exp := time.Now().Add(time.Hour)

	// First session token is written through to the store.
	b.sync("at1", "rt1", exp)
	if store.tok == nil || store.tok.AccessToken != "at1" || store.tok.RefreshToken != "rt1" {
		t.Fatalf("bridge did not persist the session token: %+v", store.tok)
	}

	// An unchanged access token is a no-op (no redundant save).
	store.tok = nil
	b.sync("at1", "rt1", exp)
	if store.tok != nil {
		t.Fatal("bridge re-saved an unchanged token")
	}

	// A new access token (refresh) is written through.
	b.sync("at2", "rt2", exp)
	if store.tok == nil || store.tok.AccessToken != "at2" {
		t.Fatalf("bridge did not persist the rotated token: %+v", store.tok)
	}

	// An empty access token (anonymous / no session) writes nothing.
	store.tok = nil
	b.sync("", "", exp)
	if store.tok != nil {
		t.Fatal("bridge persisted an empty token")
	}
}

// TestSessionTokenBridgeCapturesCookie proves the full path: a real encrypted
// OIDC session cookie (what the UI login sets) is decrypted by the bridge and
// its token persisted to the connector's store, so a UI sign-in enables the
// outbound connection without a separate `wallfacer auth login`.
func TestSessionTokenBridgeCapturesCookie(t *testing.T) {
	client := oidc.New(oidc.Config{
		AuthURL:    "https://auth.latere.ai",
		ClientID:   "wallfacer",
		CookieKey:  "0123456789abcdef0123456789abcdef",
		CookieName: "test-session", // avoid the __Host- Secure/HTTPS requirement
	})
	if client == nil {
		t.Skip("oidc client unavailable")
	}
	store := &fakeTokenStore{}
	bridge := newSessionTokenBridge(client, store)

	// Build and encrypt a session cookie as the UI login would.
	sess := oidc.SessionFromToken(&oauth2.Token{
		AccessToken:  "live-at",
		RefreshToken: "live-rt",
		Expiry:       time.Now().Add(time.Hour),
	}, 0)
	rec := httptest.NewRecorder()
	if err := client.SetSession(rec, sess); err != nil {
		t.Fatalf("set session: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie was set")
	}

	// A request carrying that cookie, routed through the bridge, persists the token.
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	bridge.wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(httptest.NewRecorder(), req)

	if store.tok == nil || store.tok.AccessToken != "live-at" || store.tok.RefreshToken != "live-rt" {
		t.Fatalf("bridge did not capture the UI cookie session token: %+v", store.tok)
	}
}

// wrap with a nil client or store must pass the handler through unchanged
// (auth not configured / local-anonymous stays byte-identical).
func TestSessionTokenBridgeWrapNil(t *testing.T) {
	var b *sessionTokenBridge
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if got := b.wrap(h); got == nil {
		t.Fatal("nil bridge wrap returned nil handler")
	}
	b2 := newSessionTokenBridge(nil, nil)
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	b2.wrap(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("wrap with nil store did not pass through to next")
	}
}

func TestCoordinationGate(t *testing.T) {
	g := &coordinationGate{}
	if g.OptedIn() {
		t.Fatal("gate must default to closed (data boundary off by default)")
	}
	g.SetOptedIn(true)
	if !g.OptedIn() {
		t.Fatal("gate did not open after SetOptedIn(true)")
	}
	g.SetOptedIn(false)
	if g.OptedIn() {
		t.Fatal("gate did not close after SetOptedIn(false)")
	}
}

func TestCoordinationGatePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coordination-opt-in")

	// Default for a signed-in instance (no file, no env) is on.
	t.Setenv("WALLFACER_COORDINATION", "")
	if !loadOptIn(path) {
		t.Fatal("loadOptIn should default on for a signed-in instance with no file/env")
	}
	// An explicit env opt-out wins over the default.
	t.Setenv("WALLFACER_COORDINATION", "0")
	if loadOptIn(path) {
		t.Fatal("WALLFACER_COORDINATION=0 should force off")
	}
	t.Setenv("WALLFACER_COORDINATION", "")

	// Flipping on persists, and a fresh load reads it back (survives restart).
	g := &coordinationGate{path: path}
	g.SetOptedIn(true)
	if !loadOptIn(path) {
		t.Fatal("opt-in did not persist across a reload")
	}
	g2 := &coordinationGate{path: path}
	g2.optedIn.Store(loadOptIn(path))
	if !g2.OptedIn() {
		t.Fatal("a restarted gate did not pick up the persisted opt-in")
	}

	// Flipping off persists too.
	g.SetOptedIn(false)
	if loadOptIn(path) {
		t.Fatal("opt-out did not persist")
	}
}

// TestManifestLocalKeyIsHashedNotPath is the data-boundary regression: the
// manifest's local_key is derived from workspace.GroupKey, which joins the raw
// local folder paths. It must be hashed before it crosses the wire so no local
// filesystem path ever reaches the coordinator.
func TestManifestLocalKeyIsHashedNotPath(t *testing.T) {
	paths := []string{"/Users/alice/dev/secret-project", "/Users/alice/work/widgets"}
	groupKey := workspace.GroupKey(paths) // joins the raw paths
	localKey := hashLocalKey(groupKey)

	if len(localKey) != 64 {
		t.Fatalf("hashLocalKey = %q (len %d), want a 64-char hex digest", localKey, len(localKey))
	}
	for _, p := range paths {
		if strings.Contains(localKey, p) {
			t.Fatalf("local_key leaks a local path: %q contains %q", localKey, p)
		}
	}
	if strings.Contains(localKey, "/") {
		t.Fatalf("local_key contains a path separator: %q", localKey)
	}

	// The full manifest, as it would be marshaled onto the wire, carries no path.
	m := coordinator.NewManifest("inst_1", "host", "dev",
		[]coordinator.WorkspaceRef{{Remote: "github.com/acme/widgets", LocalKey: localKey}},
		[]string{"comments"})
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	for _, p := range paths {
		if strings.Contains(string(b), p) {
			t.Fatalf("manifest wire bytes leak a local path: %s", b)
		}
	}
	if strings.Contains(string(b), "/Users/") {
		t.Fatalf("manifest wire bytes contain a local path: %s", b)
	}
}

func TestCoordinationDefault(t *testing.T) {
	// Explicit opt-out values force off.
	for _, v := range []string{"0", "false", "FALSE", "no", "off"} {
		t.Setenv("WALLFACER_COORDINATION", v)
		if coordinationDefault() {
			t.Fatalf("WALLFACER_COORDINATION=%q should force off", v)
		}
	}
	// Anything else (including unset) defaults on for a signed-in instance.
	for _, v := range []string{"", "1", "true", "on", "garbage"} {
		t.Setenv("WALLFACER_COORDINATION", v)
		if !coordinationDefault() {
			t.Fatalf("WALLFACER_COORDINATION=%q should default on", v)
		}
	}
}
