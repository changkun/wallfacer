package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/auth"
	"latere.ai/x/pkg/oidc"
)

// fakeSessionSource implements the unexported sessionSource interface
// that CookiePrincipal depends on. GetSession returns whatever the
// test set up; the middleware doesn't care what type wraps it as long
// as it has the GetSession method.
type fakeSessionSource struct {
	sess *oidc.Session
	err  error
}

func (f *fakeSessionSource) GetSession(_ *http.Request) (*oidc.Session, error) {
	return f.sess, f.err
}

// TestCookiePrincipal_JWTAlreadyInCtxIsNoOp covers the short-circuit
// when OptionalAuth already populated claims: the cookie bridge must
// not overwrite them or call GetSession.
func TestCookiePrincipal_JWTAlreadyInCtxIsNoOp(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	var getSessionCalled bool
	src := sessionSpy(&fakeSessionSource{}, &getSessionCalled)

	var seenSub string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, ok := auth.PrincipalFromContext(r.Context()); ok {
			seenSub = c.Sub
		}
		w.WriteHeader(http.StatusOK)
	})

	h := auth.CookiePrincipal(src, v, inner)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(auth.WithClaims(r.Context(), &auth.Claims{Sub: "preset-user"}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if seenSub != "preset-user" {
		t.Errorf("handler saw sub=%q, want preset-user (no-op branch)", seenSub)
	}
	if getSessionCalled {
		t.Error("GetSession should not be called when claims already present")
	}
}

// TestCookiePrincipal_ValidSessionPopulatesClaims is the main happy
// path: cookie present, access token is a valid JWT, claims land in
// context for downstream handlers.
func TestCookiePrincipal_ValidSessionPopulatesClaims(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	tok := signToken(t, key, defaultHeader(key), defaultPayload(time.Now().Add(time.Hour)))
	src := &fakeSessionSource{sess: &oidc.Session{AccessToken: tok}}

	capt := &claimsCapture{}
	h := auth.CookiePrincipal(src, v, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !capt.ok || capt.seen.Sub != "user-abc" {
		t.Fatalf("claims not injected; ok=%v seen=%+v", capt.ok, capt.seen)
	}
}

// TestCookiePrincipal_NoSessionPassesThrough covers the common case
// of an unauthenticated browser: GetSession returns an error or nil,
// request proceeds anonymously.
func TestCookiePrincipal_NoSessionPassesThrough(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	src := &fakeSessionSource{err: http.ErrNoCookie}

	capt := &claimsCapture{}
	h := auth.CookiePrincipal(src, v, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if capt.ok {
		t.Fatalf("expected no claims when session missing, got %+v", capt.seen)
	}
}

// TestCookiePrincipal_ExpiredSessionClearsCookie covers the safety
// behavior: a session whose stored access token no longer validates
// triggers a Set-Cookie that expires the wallfacer session cookie,
// so the next request doesn't re-try a dead token.
func TestCookiePrincipal_ExpiredSessionClearsCookie(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	expiredTok := signToken(t, key, defaultHeader(key), defaultPayload(time.Now().Add(-time.Hour)))
	src := &fakeSessionSource{sess: &oidc.Session{AccessToken: expiredTok}}

	capt := &claimsCapture{}
	h := auth.CookiePrincipal(src, v, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if capt.ok {
		t.Fatalf("expected no claims for expired token, got %+v", capt.seen)
	}
	// The session cookie must be cleared via a Set-Cookie with a past
	// expiration / Max-Age=0; the platform's ClearSession does both.
	foundClear := false
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 || !c.Expires.IsZero() {
			foundClear = true
			break
		}
	}
	if !foundClear {
		t.Error("expected a Set-Cookie clearing the session after expired token")
	}
}

// TestCookiePrincipal_NilInputsIdentity confirms the graceful
// degradation on local mode (nil client) and on misconfigured cloud
// deployments (nil validator): the middleware collapses to identity.
func TestCookiePrincipal_NilInputsIdentity(t *testing.T) {
	capt := &claimsCapture{}
	h := auth.CookiePrincipal(nil, nil, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || capt.ok {
		t.Fatalf("nil inputs should be identity; code=%d ok=%v", w.Code, capt.ok)
	}
}

// sessionSpy wraps a fakeSessionSource to record whether GetSession was
// called. Used by the no-op test to confirm we don't touch the cookie
// when JWT claims already present.
func sessionSpy(f *fakeSessionSource, called *bool) *sessionSpyImpl {
	return &sessionSpyImpl{inner: f, called: called}
}

type sessionSpyImpl struct {
	inner  *fakeSessionSource
	called *bool
}

func (s *sessionSpyImpl) GetSession(r *http.Request) (*oidc.Session, error) {
	*s.called = true
	return s.inner.GetSession(r)
}
