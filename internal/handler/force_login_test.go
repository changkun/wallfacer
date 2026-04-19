package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/auth"
)

// neverReached is a terminal handler that fails the test if invoked;
// used to confirm the redirect short-circuits the chain.
func neverReached(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("inner handler should not have been reached (expected redirect)")
		w.WriteHeader(http.StatusOK)
	})
}

// ok204 is a terminal handler that returns 204 when reached.
func ok204() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

// newAuthHandler builds a Handler with auth enabled via the same
// fakeAuthProvider already used in the superadmin test; a regular
// newTestHandler yields h.HasAuth() == false which would make
// ForceLogin collapse to identity.
func newAuthHandler(t *testing.T) *Handler {
	t.Helper()
	h := newTestHandler(t)
	h.SetAuth(fakeAuthProvider{})
	return h
}

// fakeAuthProvider locally satisfies AuthProvider. Duplicated from
// internal/cli so this test doesn't cross the cli package boundary.
type fakeAuthProvider struct{}

func (fakeAuthProvider) HandleLogin(http.ResponseWriter, *http.Request)    {}
func (fakeAuthProvider) HandleCallback(http.ResponseWriter, *http.Request) {}
func (fakeAuthProvider) HandleLogout(http.ResponseWriter, *http.Request)   {}
func (fakeAuthProvider) UserFromRequest(http.ResponseWriter, *http.Request) *auth.User {
	return nil
}
func (fakeAuthProvider) AuthURL() string { return "https://auth.latere.ai" }

// TestForceLogin_AnonymousBrowserRedirectsToLogin covers the core
// contract: cloud mode + anonymous HTML GET → 302 to /login with
// the original path preserved in ?next=.
func TestForceLogin_AnonymousBrowserRedirectsToLogin(t *testing.T) {
	h := newAuthHandler(t)
	mw := h.ForceLogin(neverReached(t))

	req := httptest.NewRequest(http.MethodGet, "/mode/board", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/login") {
		t.Fatalf("Location = %q, want /login prefix", loc)
	}
	if !strings.Contains(loc, "next=%2Fmode%2Fboard") {
		t.Errorf("Location = %q, want next=%%2Fmode%%2Fboard", loc)
	}
}

// TestForceLogin_AnonymousAPIGetsThrough covers the API exemption:
// a /api/tasks GET with Accept: application/json gets through, so
// the JWT middleware downstream can emit 401 cleanly.
func TestForceLogin_AnonymousAPIGetsThrough(t *testing.T) {
	h := newAuthHandler(t)
	mw := h.ForceLogin(ok204())

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (pass through)", w.Code)
	}
}

// TestForceLogin_AllowlistPaths confirms every unprotected path and
// prefix reaches the inner handler even when anonymous + HTML.
func TestForceLogin_AllowlistPaths(t *testing.T) {
	h := newAuthHandler(t)
	mw := h.ForceLogin(ok204())

	cases := []string{"/login", "/callback", "/logout", "/logout/notify",
		"/api/config", "/api/auth/me", "/favicon.ico", "/css/app.css",
		"/js/main.js", "/assets/logo.svg", "/static/foo.png"}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "text/html")
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, req)
			if w.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204 for allowlisted path", w.Code)
			}
		})
	}
}

// TestForceLogin_AuthenticatedPassesThrough covers the happy path:
// a claim set in context, even on a protected HTML GET, still
// reaches the inner handler.
func TestForceLogin_AuthenticatedPassesThrough(t *testing.T) {
	h := newAuthHandler(t)
	mw := h.ForceLogin(ok204())

	req := httptest.NewRequest(http.MethodGet, "/mode/board", nil)
	req.Header.Set("Accept", "text/html")
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{Sub: "alice"}))
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (authenticated passes)", w.Code)
	}
}

// TestForceLogin_LocalModeIsIdentity confirms the middleware is a
// no-op when h.HasAuth() is false, matching today's anonymous local
// deployment behavior.
func TestForceLogin_LocalModeIsIdentity(t *testing.T) {
	h := newTestHandler(t) // no SetAuth
	mw := h.ForceLogin(ok204())

	req := httptest.NewRequest(http.MethodGet, "/mode/board", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (local mode pass)", w.Code)
	}
}

// TestForceLogin_RejectsAbsoluteNext confirms the open-redirect
// guard: a crafted request path that looks like an absolute URL
// must not survive into the ?next= parameter.
func TestForceLogin_RejectsAbsoluteNext(t *testing.T) {
	h := newAuthHandler(t)
	mw := h.ForceLogin(neverReached(t))

	// Protocol-relative URL that could otherwise redirect off-site.
	req := httptest.NewRequest(http.MethodGet, "//evil.example/oops", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/login" {
		t.Errorf("Location = %q, want bare /login (next dropped)", loc)
	}
}

// TestForceLogin_NonGETPassesThrough ensures POSTs and similar
// state-changing methods are never redirected, even if anonymous
// and HTML-shaped. They land at the upstream 401 path.
func TestForceLogin_NonGETPassesThrough(t *testing.T) {
	h := newAuthHandler(t)
	mw := h.ForceLogin(ok204())

	req := httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (POST passes)", w.Code)
	}
}
