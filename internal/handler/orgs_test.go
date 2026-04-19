package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/auth"
)

// fakeAuthClientWithSession extends the fakeAuthProvider used in other
// handler tests with a GetSession method so *Handler.AuthOrgs /
// AuthSwitchOrg can pull the access token. Narrow to what the tested
// code path needs.
type fakeAuthClientWithSession struct {
	fakeAuthProvider
	sess *auth.Session
	err  error
}

func (f *fakeAuthClientWithSession) GetSession(_ *http.Request) (*auth.Session, error) {
	return f.sess, f.err
}

// lastOrgsRequest captures the most recent request sent to the
// stubbed auth service so tests can assert on the Authorization
// header without wiring channels.
var lastOrgsRequest *http.Request

func stubOrgsHTTPCapture(t *testing.T, status int, body string) {
	t.Helper()
	lastOrgsRequest = nil
	original := httpGet
	httpGet = func(req *http.Request) (*http.Response, error) {
		lastOrgsRequest = req
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}
	t.Cleanup(func() { httpGet = original })
}

// TestAuthOrgs_SingleOrgReturns200 confirms a user with one org gets
// a 200 with a one-entry list (not 204). The frontend renders a
// static label in this case so the user sees which org their token
// is scoped to — visible confirmation that /api/auth/orgs is wired
// even for the common single-org case.
func TestAuthOrgs_SingleOrgReturns200(t *testing.T) {
	h := newTestHandler(t)
	h.SetAuth(&fakeAuthClientWithSession{sess: &auth.Session{AccessToken: "tok"}})

	stubOrgsHTTPCapture(t, http.StatusOK, `[{"id":"org-a","name":"Alice Inc","slug":"alice"}]`)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/orgs", nil)
	w := httptest.NewRecorder()
	h.AuthOrgs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if lastOrgsRequest == nil {
		t.Fatal("auth service was never called")
	}
	if got := lastOrgsRequest.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization header = %q, want Bearer tok", got)
	}
}

// TestAuthOrgs_NoMembershipsReturns204 covers the genuine "nothing
// to render" case: the user has zero orgs.
func TestAuthOrgs_NoMembershipsReturns204(t *testing.T) {
	h := newTestHandler(t)
	h.SetAuth(&fakeAuthClientWithSession{sess: &auth.Session{AccessToken: "tok"}})

	stubOrgsHTTPCapture(t, http.StatusOK, `[]`)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/orgs", nil)
	w := httptest.NewRecorder()
	h.AuthOrgs(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

// TestAuthOrgs_MultiOrgReturns200 covers the happy path: more than one
// org produces a 200 with the full list + current_id from claims.
func TestAuthOrgs_MultiOrgReturns200(t *testing.T) {
	h := newTestHandler(t)
	h.SetAuth(&fakeAuthClientWithSession{sess: &auth.Session{AccessToken: "tok"}})

	stubOrgsHTTPCapture(t, http.StatusOK, `[{"id":"org-a","name":"Alice Inc"},{"id":"org-b","name":"Bob Corp"}]`)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/orgs", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{Sub: "u1", OrgID: "org-b"}))
	w := httptest.NewRecorder()
	h.AuthOrgs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var got orgsResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Orgs) != 2 {
		t.Fatalf("orgs len = %d, want 2", len(got.Orgs))
	}
	if got.CurrentID != "org-b" {
		t.Errorf("current_id = %q, want org-b", got.CurrentID)
	}
}

// TestAuthOrgs_NoSessionReturns204 covers the unauthenticated branch:
// no session cookie means no access token to forward, so we match
// /api/auth/me's 204-for-anonymous behavior.
func TestAuthOrgs_NoSessionReturns204(t *testing.T) {
	h := newTestHandler(t)
	h.SetAuth(&fakeAuthClientWithSession{sess: nil, err: http.ErrNoCookie})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/orgs", nil)
	w := httptest.NewRecorder()
	h.AuthOrgs(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

// TestAuthOrgs_LocalModeReturns204 covers the local-mode path: no auth
// client configured at all.
func TestAuthOrgs_LocalModeReturns204(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/orgs", nil)
	w := httptest.NewRecorder()
	h.AuthOrgs(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

// TestAuthSwitchOrg_HappyPath covers the successful switch: valid
// target, member of that org, response contains the /login URL with
// org_id, session cookie is cleared.
func TestAuthSwitchOrg_HappyPath(t *testing.T) {
	h := newTestHandler(t)
	h.SetAuth(&fakeAuthClientWithSession{sess: &auth.Session{AccessToken: "tok"}})

	stubOrgsHTTPCapture(t, http.StatusOK, `[{"id":"org-a","name":"Alice Inc"},{"id":"org-b","name":"Bob Corp"}]`)

	body := bytes.NewBufferString(`{"org_id":"org-b"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/switch-org", body)
	w := httptest.NewRecorder()
	h.AuthSwitchOrg(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var got switchOrgResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.RedirectURL != "/login?org_id=org-b" {
		t.Errorf("redirect_url = %q, want /login?org_id=org-b", got.RedirectURL)
	}
	// The session cookie must have been cleared (Max-Age=0 or a past
	// expiry).
	cleared := false
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 || !c.Expires.IsZero() {
			cleared = true
			break
		}
	}
	if !cleared {
		t.Error("session cookie not cleared on switch")
	}
}

// TestAuthSwitchOrg_NonMemberReturns403 closes the attack/mistake
// case: switching to an org the user is not a member of returns 403
// instead of silently redirecting.
func TestAuthSwitchOrg_NonMemberReturns403(t *testing.T) {
	h := newTestHandler(t)
	h.SetAuth(&fakeAuthClientWithSession{sess: &auth.Session{AccessToken: "tok"}})

	stubOrgsHTTPCapture(t, http.StatusOK, `[{"id":"org-a","name":"Alice Inc"}]`)

	body := bytes.NewBufferString(`{"org_id":"org-evil"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/switch-org", body)
	w := httptest.NewRecorder()
	h.AuthSwitchOrg(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

// TestAuthSwitchOrg_Unauthenticated401 covers the no-session case.
func TestAuthSwitchOrg_Unauthenticated401(t *testing.T) {
	h := newTestHandler(t)
	h.SetAuth(&fakeAuthClientWithSession{sess: nil, err: http.ErrNoCookie})

	body := bytes.NewBufferString(`{"org_id":"org-a"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/switch-org", body)
	w := httptest.NewRecorder()
	h.AuthSwitchOrg(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// TestAuthSwitchOrg_EmptyOrgIDSwitchesToPersonal covers the
// switch-to-personal path: empty org_id is a valid request (not an
// error), clears the session cookie, and returns a redirect to
// /login?org_id= which the auth service reads as "clear
// active_org on this SSO session".
func TestAuthSwitchOrg_EmptyOrgIDSwitchesToPersonal(t *testing.T) {
	h := newTestHandler(t)
	h.SetAuth(&fakeAuthClientWithSession{sess: &auth.Session{AccessToken: "tok"}})

	body := bytes.NewBufferString(`{"org_id":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/switch-org", body)
	w := httptest.NewRecorder()
	h.AuthSwitchOrg(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got switchOrgResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.RedirectURL != "/login?org_id=" {
		t.Errorf("redirect_url = %q, want /login?org_id=", got.RedirectURL)
	}
	// Session cookie must be cleared.
	cleared := false
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 || !c.Expires.IsZero() {
			cleared = true
			break
		}
	}
	if !cleared {
		t.Error("session cookie not cleared on switch-to-personal")
	}
}
