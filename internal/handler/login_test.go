package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/auth"
)

// fakeAuth implements AuthProvider so tests can exercise the handler branches
// without constructing a real OIDC client (which would require a live auth
// service for UserFromRequest to succeed).
type fakeAuth struct {
	user         *auth.User
	url          string
	loginCalls   int
	callbackCalls int
	logoutCalls  int
}

func (f *fakeAuth) HandleLogin(w http.ResponseWriter, _ *http.Request) {
	f.loginCalls++
	w.WriteHeader(http.StatusFound)
}

func (f *fakeAuth) HandleCallback(w http.ResponseWriter, _ *http.Request) {
	f.callbackCalls++
	w.WriteHeader(http.StatusFound)
}

func (f *fakeAuth) HandleLogout(w http.ResponseWriter, _ *http.Request) {
	f.logoutCalls++
	w.WriteHeader(http.StatusFound)
}

func (f *fakeAuth) UserFromRequest(_ http.ResponseWriter, _ *http.Request) *auth.User {
	return f.user
}

func (f *fakeAuth) AuthURL() string {
	if f.url == "" {
		return "https://auth.latere.ai"
	}
	return f.url
}

// TestAuthMe_NilClient_Returns204 confirms local mode silently reports no
// session rather than surfacing a 5xx that the UI would have to special-case.
func TestAuthMe_NilClient_Returns204(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	h.AuthMe(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d; want 204", w.Code)
	}
}

// TestAuthMe_NoSession_Returns204 covers cloud mode with no cookie: the
// auth provider exists but the user hasn't signed in.
func TestAuthMe_NoSession_Returns204(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	h.SetAuth(&fakeAuth{user: nil})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	h.AuthMe(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d; want 204", w.Code)
	}
}

// TestAuthMe_WithSession_Returns200 verifies the JSON shape the frontend
// consumes. Required keys: sub, email, name, picture.
func TestAuthMe_WithSession_Returns200(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	h.SetAuth(&fakeAuth{user: &auth.User{
		Sub:     "u-123",
		Email:   "a@b.com",
		Name:    "Alice",
		Picture: "https://cdn/a.png",
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	h.AuthMe(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d; want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; want application/json", ct)
	}
	var got struct {
		Sub, Email, Name, Picture string
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Sub != "u-123" || got.Email != "a@b.com" || got.Name != "Alice" || got.Picture != "https://cdn/a.png" {
		t.Errorf("got %+v; want sub=u-123 email=a@b.com name=Alice picture=https://cdn/a.png", got)
	}
}

// TestLogin_NilClient_Returns503 ensures a misconfigured deployment surfaces
// a clear 503 rather than an opaque panic or empty response.
func TestLogin_NilClient_Returns503(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()
	h.Login(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d; want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "auth not configured") {
		t.Errorf("body = %q; want auth-not-configured message", w.Body.String())
	}
}

// TestLogin_WithClient_Delegates verifies a configured provider's
// HandleLogin is invoked (indirectly confirms the nil-guard short-circuits
// only when intended).
func TestLogin_WithClient_Delegates(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	f := &fakeAuth{}
	h.SetAuth(f)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()
	h.Login(w, req)
	if f.loginCalls != 1 {
		t.Errorf("loginCalls = %d; want 1", f.loginCalls)
	}
}

// TestLogoutNotify_ClearsCookie covers the front-channel logout path: the
// auth service broadcasts by loading /logout/notify in a hidden iframe, and
// we must clear the cookie even when the AuthProvider is nil (the cookie
// name and key derivation don't depend on provider state).
func TestLogoutNotify_ClearsCookie(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/logout/notify", nil)
	w := httptest.NewRecorder()
	h.LogoutNotify(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d; want 200", w.Code)
	}
	// auth.ClearSession writes a Set-Cookie with MaxAge <= 0 to expire it.
	setCookie := w.Header().Get("Set-Cookie")
	if setCookie == "" {
		t.Fatal("expected Set-Cookie header clearing the session; got none")
	}
}

// TestLogout_NilClient_RedirectsHome keeps the endpoint safe to link to
// from the UI even before auth is configured.
func TestLogout_NilClient_RedirectsHome(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	w := httptest.NewRecorder()
	h.Logout(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("code = %d; want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q; want /", loc)
	}
}

// TestGetConfig_CloudFlagFalse — no auth provider: response contains
// cloud=false and omits auth_url entirely (omitempty keeps the wire small
// for the common local-mode case).
func TestGetConfig_CloudFlagFalse(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := resp["cloud"].(bool); got {
		t.Errorf("cloud = true; want false")
	}
	if _, present := resp["auth_url"]; present {
		t.Errorf("auth_url present in local-mode response; want omitted")
	}
}

// TestGetConfig_CloudFlagTrue — auth provider installed: cloud=true and
// auth_url echoes the provider's AuthURL().
func TestGetConfig_CloudFlagTrue(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	h.SetAuth(&fakeAuth{url: "https://auth.latere.ai"})

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := resp["cloud"].(bool); !got {
		t.Errorf("cloud = false; want true")
	}
	if got, _ := resp["auth_url"].(string); got != "https://auth.latere.ai" {
		t.Errorf("auth_url = %q; want https://auth.latere.ai", got)
	}
}
