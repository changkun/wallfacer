package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"latere.ai/x/pkg/authkit/oidc"
	"latere.ai/x/pkg/httpjson"
)

// TestWebMe_SignedOutAnswersErrorEnvelope verifies that GET /api/me on the
// coordinator site answers a request with no session 401 in the error
// envelope: the not_signed_in code and its fixed sentence, the status the
// session store reads as signed out.
func TestWebMe_SignedOutAnswersErrorEnvelope(t *testing.T) {
	// No RedirectURL: a client for the session helpers only, which needs no
	// cookie key and no network to construct.
	client := oidc.New(oidc.Config{ClientID: "wallfacer-web", AuthURL: "http://127.0.0.1:0"})
	if client == nil {
		t.Fatal("oidc.New returned nil")
	}

	rec := httptest.NewRecorder()
	webMeHandler(client)(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var env httpjson.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (%q)", err, rec.Body.String())
	}
	if env.Error.Code != codeWebNotSignedIn || env.Error.Message != messageWebNotSignedIn {
		t.Fatalf("envelope = %+v, want code %q with its message", env.Error, codeWebNotSignedIn)
	}
}
