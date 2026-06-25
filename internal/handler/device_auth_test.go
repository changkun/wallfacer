package handler

import (
	"bytes"
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
