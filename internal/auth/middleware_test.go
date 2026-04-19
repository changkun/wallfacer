package auth_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/auth"
)

// --- Test helpers ----------------------------------------------------------
//
// The jwtauth package's test helpers are package-internal. The bits we need
// (keypair, JWKS server, token signing) are small; inlining them keeps the
// test binary self-contained.

func genKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func rsaKid(pub *rsa.PublicKey) string {
	h := sha256.Sum256(pub.N.Bytes())
	return base64.RawURLEncoding.EncodeToString(h[:])[:8]
}

func b64(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

func signToken(t *testing.T, key *rsa.PrivateKey, header, payload map[string]any) string {
	t.Helper()
	h := b64(header)
	p := b64(payload)
	input := h + "." + p
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func serveJWKS(t *testing.T, keys ...*rsa.PrivateKey) *httptest.Server {
	t.Helper()
	type jwk struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Alg string `json:"alg"`
		Use string `json:"use"`
		N   string `json:"n"`
		E   string `json:"e"`
	}
	var out struct {
		Keys []jwk `json:"keys"`
	}
	for _, k := range keys {
		out.Keys = append(out.Keys, jwk{
			Kty: "RSA",
			Kid: rsaKid(&k.PublicKey),
			Alg: "RS256",
			Use: "sig",
			N:   base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
		})
	}
	data, _ := json.Marshal(out)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func defaultHeader(key *rsa.PrivateKey) map[string]any {
	return map[string]any{"alg": "RS256", "typ": "JWT", "kid": rsaKid(&key.PublicKey)}
}

func defaultPayload(expAt time.Time) map[string]any {
	return map[string]any{
		"sub":            "user-abc",
		"iss":            "https://auth.latere.ai",
		"aud":            "my-client",
		"exp":            float64(expAt.Unix()),
		"iat":            float64(time.Now().Unix()),
		"principal_type": "user",
		"email":          "alice@example.com",
		"org_id":         "org-1",
	}
}

// claimsCapture is a tiny HandlerFunc that records whether claims were in
// the context and exposes them for assertions.
type claimsCapture struct {
	seen *auth.Claims
	ok   bool
}

func (c *claimsCapture) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.seen, c.ok = auth.PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

// --- BuildValidator --------------------------------------------------------

func TestBuildValidator_NilWhenAuthURLEmpty(t *testing.T) {
	if v := auth.BuildValidator(auth.Config{}, "", ""); v != nil {
		t.Fatalf("BuildValidator(empty) = %v, want nil", v)
	}
}

func TestBuildValidator_UsesAuthURLDefaults(t *testing.T) {
	v := auth.BuildValidator(auth.Config{AuthURL: "https://auth.example.com"}, "", "")
	if v == nil {
		t.Fatal("BuildValidator returned nil with valid AuthURL")
	}
}

// --- OptionalAuth ----------------------------------------------------------

func TestOptionalAuth_ValidTokenInjectsClaims(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	tok := signToken(t, key, defaultHeader(key), defaultPayload(time.Now().Add(time.Hour)))

	capt := &claimsCapture{}
	h := auth.OptionalAuth(v, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !capt.ok {
		t.Fatal("PrincipalFromContext returned !ok")
	}
	if capt.seen.Sub != "user-abc" {
		t.Errorf("claims.Sub = %q, want user-abc", capt.seen.Sub)
	}
	if capt.seen.OrgID != "org-1" {
		t.Errorf("claims.OrgID = %q, want org-1", capt.seen.OrgID)
	}
}

func TestOptionalAuth_ExpiredTokenPassesThroughAnonymous(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	tok := signToken(t, key, defaultHeader(key), defaultPayload(time.Now().Add(-time.Hour)))

	capt := &claimsCapture{}
	h := auth.OptionalAuth(v, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if capt.ok {
		t.Fatalf("expected no claims on expired token, got %+v", capt.seen)
	}
}

func TestOptionalAuth_MalformedHeaderPassesThrough(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	capt := &claimsCapture{}
	h := auth.OptionalAuth(v, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	r.Header.Set("Authorization", "Bearer junk-not-a-jwt")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (pass through)", w.Code)
	}
	if capt.ok {
		t.Fatal("expected no claims on malformed token")
	}
}

func TestOptionalAuth_NoHeaderPassesThrough(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	capt := &claimsCapture{}
	h := auth.OptionalAuth(v, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if capt.ok {
		t.Fatal("expected no claims when no Authorization header")
	}
}

func TestOptionalAuth_NilValidatorIsIdentity(t *testing.T) {
	capt := &claimsCapture{}
	h := auth.OptionalAuth(nil, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	r.Header.Set("Authorization", "Bearer anything")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || capt.ok {
		t.Fatalf("nil validator should be identity; code=%d ok=%v", w.Code, capt.ok)
	}
}

// --- Auth (strict) ---------------------------------------------------------

func TestAuth_ValidToken200(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	tok := signToken(t, key, defaultHeader(key), defaultPayload(time.Now().Add(time.Hour)))

	capt := &claimsCapture{}
	h := auth.Auth(v, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/api/admin/rebuild-index", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !capt.ok || capt.seen.Sub != "user-abc" {
		t.Fatalf("expected claims.Sub = user-abc, got ok=%v seen=%+v", capt.ok, capt.seen)
	}
}

func TestAuth_MissingTokenReturns401(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	h := auth.Auth(v, (&claimsCapture{}).handler())
	r := httptest.NewRequest(http.MethodGet, "/api/admin/rebuild-index", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAuth_ExpiredTokenReturns401(t *testing.T) {
	key := genKey(t)
	srv := serveJWKS(t, key)
	v := auth.BuildValidator(auth.Config{AuthURL: srv.URL, ClientID: "my-client"}, srv.URL, "https://auth.latere.ai")

	tok := signToken(t, key, defaultHeader(key), defaultPayload(time.Now().Add(-time.Hour)))

	h := auth.Auth(v, (&claimsCapture{}).handler())
	r := httptest.NewRequest(http.MethodGet, "/api/admin/rebuild-index", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAuth_NilValidatorIsIdentity(t *testing.T) {
	capt := &claimsCapture{}
	h := auth.Auth(nil, capt.handler())
	r := httptest.NewRequest(http.MethodGet, "/api/admin/rebuild-index", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || capt.ok {
		t.Fatalf("nil validator should be identity; code=%d ok=%v", w.Code, capt.ok)
	}
}

// --- PrincipalFromContext --------------------------------------------------

func TestPrincipalFromContext_EmptyContextReturnsFalse(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := auth.PrincipalFromContext(r.Context()); ok {
		t.Fatal("PrincipalFromContext on empty ctx should return (nil, false)")
	}
}
