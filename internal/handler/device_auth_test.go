package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/pkg/oidc"
)

func newDeviceServer(t *testing.T, deviceBody, tokenBody string, tokenStatus int) (*oidc.Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, deviceBody)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if tokenStatus != 0 {
			w.WriteHeader(tokenStatus)
		}
		_, _ = fmt.Fprint(w, tokenBody)
	})
	srv := httptest.NewServer(mux)
	c := oidc.New(oidc.Config{
		AuthURL:         srv.URL,
		ClientID:        "wallfacer-local",
		ClientSecret:    "x",
		RedirectURL:     srv.URL + "/cb",
		CookieKey:       "0011223344556677889900aabbccddeeff",
		InsecureCookies: true,
	})
	return c, srv
}

// TestDeviceAuth_Lifecycle exercises start -> poll-pending -> poll-done.
func TestDeviceAuth_Lifecycle(t *testing.T) {
	c, srv := newDeviceServer(t,
		`{"device_code":"dc","user_code":"UC-1234","verification_uri":"https://verify.example/","verification_uri_complete":"https://verify.example/?u=UC-1234","expires_in":300,"interval":1}`,
		`{"access_token":"at-x","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-x"}`, 0)
	defer srv.Close()

	tmpStore := filepath.Join(t.TempDir(), "token.json")
	store, _ := authkit.NewFileTokenStore(tmpStore)
	d := &DeviceAuth{OIDC: c, Store: store}

	mux := http.NewServeMux()
	d.Mount(mux)

	// Start.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/device/start", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d body=%s", rec.Code, rec.Body.String())
	}
	var sresp startResponse
	if err := json.NewDecoder(rec.Body).Decode(&sresp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sresp.UserCode != "UC-1234" || !strings.Contains(sresp.VerificationURIComplete, "UC-1234") {
		t.Fatalf("start response = %+v", sresp)
	}

	// Poll until done. The polling loop runs in a goroutine and the underlying
	// oauth2 device-token call has a per-iteration interval; allow generous
	// real time per poll so the test does not race the goroutine.
	done := false
	for i := 0; i < 60 && !done; i++ {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/device/poll", nil))
		var presp pollResponse
		if err := json.NewDecoder(rec.Body).Decode(&presp); err != nil {
			t.Fatalf("poll decode: %v", err)
		}
		switch presp.Status {
		case "done":
			done = true
		case "pending":
			time.Sleep(100 * time.Millisecond)
		default:
			t.Fatalf("unexpected poll status %q (err=%q)", presp.Status, presp.Error)
		}
	}
	if !done {
		t.Fatal("device flow never reached done within timeout")
	}

	// The poll that observes completion must mint the session cookie so
	// /api/me reflects the sign-in (the token also lands in the file store,
	// but /api/me reads the cookie session, not the store).
	var sessionCookie *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if strings.Contains(ck.Name, "latere-session") {
			sessionCookie = ck
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("done poll did not set a session cookie; cookies=%v", rec.Result().Cookies())
	}

	tok, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tok == nil || tok.AccessToken != "at-x" {
		t.Fatalf("token = %+v", tok)
	}

	// A subsequent poll on an empty flow reports idle.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/device/poll", nil))
	var presp pollResponse
	_ = json.NewDecoder(rec.Body).Decode(&presp)
	if presp.Status != "idle" {
		t.Fatalf("idle poll = %q", presp.Status)
	}
}

// TestDeviceAuth_NilMountsUnavailable verifies the nil-mount fallback so the
// SPA can rely on a 503 instead of a 404 for unconfigured deployments.
func TestDeviceAuth_NilMountsUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	var d *DeviceAuth
	d.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/device/start", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil mount = %d", rec.Code)
	}
}

// fakeJWT builds an unsigned but well-formed JWT (header.payload.sig). The
// oidc read path decodes claims without verifying the signature, so this is
// enough to drive BuildMe/AuthMe end to end in tests.
func fakeJWT(t *testing.T, sub, email string, exp time.Time) string {
	t.Helper()
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := fmt.Sprintf(`{"sub":%q,"email":%q,"exp":%d}`, sub, email, exp.Unix())
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return hdr + "." + body + ".sig"
}

// TestDeviceAuth_SignsInViaAuthMe is the end-to-end guard the feature exists
// for: a completed device flow must make /api/me return a principal, not the
// 204 it returned before the cookie bridge. It shares one OIDC client between
// the auth provider (which /api/me reads) and the device driver (which mints
// the cookie), so the cookie the done poll sets is the cookie AuthMe decodes.
func TestDeviceAuth_SignsInViaAuthMe(t *testing.T) {
	jwt := fakeJWT(t, "u-dev", "dev@latere.ai", time.Now().Add(time.Hour))
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"device_code":"dc","user_code":"UC-ME","verification_uri":"https://verify.example/","expires_in":300,"interval":1}`)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600,"refresh_token":"rt"}`, jwt)
	})
	// /userinfo + /me/orgs are best-effort in BuildMe; empty 200s keep the
	// principal resolution clean (sub still comes from the decoded token).
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/me/orgs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[]`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := oidc.New(oidc.Config{
		AuthURL:         srv.URL,
		ClientID:        "wallfacer-local",
		ClientSecret:    "x",
		RedirectURL:     srv.URL + "/cb",
		CookieKey:       "0011223344556677889900aabbccddeeff",
		InsecureCookies: true,
	})
	store, _ := authkit.NewFileTokenStore(filepath.Join(t.TempDir(), "token.json"))

	h, _ := newTestHandlerWithWorkspaces(t)
	// The same client backs both surfaces so the minted cookie is readable.
	h.SetAuth(c)
	h.SetDeviceAuth(&DeviceAuth{OIDC: c, Store: store})

	// Before sign-in, /api/me is 204 (the regression this feature closes).
	pre := httptest.NewRecorder()
	h.AuthMe(pre, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	if pre.Code != http.StatusNoContent {
		t.Fatalf("pre-login /api/me = %d; want 204", pre.Code)
	}

	// Start + poll to completion, capturing the done response's cookies.
	startRec := httptest.NewRecorder()
	h.AuthDeviceStart(startRec, httptest.NewRequest(http.MethodPost, "/api/auth/device/start", bytes.NewReader([]byte(`{}`))))
	if startRec.Code != http.StatusOK {
		t.Fatalf("start = %d", startRec.Code)
	}
	var doneCookies []*http.Cookie
	for i := 0; i < 60 && doneCookies == nil; i++ {
		rec := httptest.NewRecorder()
		h.AuthDevicePoll(rec, httptest.NewRequest(http.MethodGet, "/api/auth/device/poll", nil))
		var presp pollResponse
		_ = json.NewDecoder(rec.Body).Decode(&presp)
		switch presp.Status {
		case "done":
			doneCookies = rec.Result().Cookies()
		case "pending":
			time.Sleep(100 * time.Millisecond)
		default:
			t.Fatalf("unexpected poll status %q (err=%q)", presp.Status, presp.Error)
		}
	}
	if len(doneCookies) == 0 {
		t.Fatal("done poll set no cookies")
	}

	// A /api/me carrying the session cookie now returns the principal.
	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	for _, ck := range doneCookies {
		meReq.AddCookie(ck)
	}
	meRec := httptest.NewRecorder()
	h.AuthMe(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("post-login /api/me = %d body=%s; want 200", meRec.Code, meRec.Body.String())
	}
	var me map[string]any
	if err := json.Unmarshal(meRec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode /api/me: %v", err)
	}
	if me["principal_id"] != "u-dev" {
		t.Fatalf("principal_id = %v; want u-dev (body=%s)", me["principal_id"], meRec.Body.String())
	}
}

// TestHandler_SetDeviceAuth_Wires confirms the handler-level gate: with no
// driver installed the device endpoints answer 503 (so the SPA can detect
// "device sign-in unavailable"), and after SetDeviceAuth they go live.
func TestHandler_SetDeviceAuth_Wires(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	// Nil driver (default): 503.
	rec := httptest.NewRecorder()
	h.AuthDeviceStart(rec, httptest.NewRequest(http.MethodPost, "/api/auth/device/start", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired start = %d; want 503", rec.Code)
	}

	// Wired driver: the start endpoint drives the device flow and returns 200.
	c, srv := newDeviceServer(t,
		`{"device_code":"dc","user_code":"UC-9","verification_uri":"https://verify.example/","expires_in":300,"interval":1}`,
		`{"error":"authorization_pending"}`, http.StatusBadRequest)
	defer srv.Close()
	store, _ := authkit.NewFileTokenStore(filepath.Join(t.TempDir(), "token.json"))
	h.SetDeviceAuth(&DeviceAuth{OIDC: c, Store: store})

	rec = httptest.NewRecorder()
	h.AuthDeviceStart(rec, httptest.NewRequest(http.MethodPost, "/api/auth/device/start", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("wired start = %d body=%s; want 200", rec.Code, rec.Body.String())
	}
	// Cancel the in-flight flow so its polling goroutine doesn't outlive the test.
	h.AuthDeviceCancel(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/auth/device/cancel", nil))
}

// TestDeviceAuth_Cancel clears an in-flight flow so the next start does not
// race the previous goroutine.
func TestDeviceAuth_Cancel(t *testing.T) {
	c, srv := newDeviceServer(t,
		`{"device_code":"dc","user_code":"UC","verification_uri":"https://verify.example/","expires_in":300,"interval":1}`,
		`{"error":"authorization_pending"}`, http.StatusBadRequest)
	defer srv.Close()

	tmpStore := filepath.Join(t.TempDir(), "token.json")
	store, _ := authkit.NewFileTokenStore(tmpStore)
	d := &DeviceAuth{OIDC: c, Store: store}

	mux := http.NewServeMux()
	d.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/device/start", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/device/cancel", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cancel: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/device/poll", nil))
	var presp pollResponse
	_ = json.NewDecoder(rec.Body).Decode(&presp)
	if presp.Status != "idle" {
		t.Fatalf("post-cancel poll = %q", presp.Status)
	}
}
