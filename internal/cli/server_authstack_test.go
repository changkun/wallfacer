package cli

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
	"changkun.de/x/wallfacer/internal/handler"
)

// The three tests below cover the middleware-composition contract the
// server relies on in cloud mode: JWT OptionalAuth populates claims on
// the way in, and the downstream static-key BearerAuthMiddleware honors
// those claims as an identity source. The chain is replicated here
// (rather than standing up initServer) because the real boot path needs
// Podman, the store, runner goroutines, and env files; a direct
// composition test is both faster and easier to reason about.

// stackWithValidator returns the same handler chain initServer builds
// when WALLFACER_CLOUD is on: BearerAuth wraps next, OptionalAuth wraps
// BearerAuth. apiKey="" disables the static-key check (local cloud
// deployment without a key).
func stackWithValidator(t *testing.T, v *auth.Validator, apiKey string, captured **auth.Claims) http.Handler {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := auth.PrincipalFromContext(r.Context())
		*captured = c
		w.WriteHeader(http.StatusOK)
	})
	inner := handler.BearerAuthMiddleware(apiKey)(next)
	return auth.OptionalAuth(v, inner)
}

// mintRSAKeyAndJWKS returns an RSA key and a JWKS server that exposes
// its public half. The server lifetime is bound to the test.
func mintRSAKeyAndJWKS(t *testing.T) (*rsa.PrivateKey, *httptest.Server) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kidHash := sha256.Sum256(key.N.Bytes())
	kid := base64.RawURLEncoding.EncodeToString(kidHash[:])[:8]
	jwks, _ := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": kid,
			"alg": "RS256",
			"use": "sig",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(srv.Close)
	return key, srv
}

// signJWT produces a valid JWT using the RSA key. exp controls the
// expiration time so tests can make "valid" or "expired" tokens.
func signJWT(t *testing.T, key *rsa.PrivateKey, sub string, exp time.Time) string {
	t.Helper()
	kidHash := sha256.Sum256(key.N.Bytes())
	kid := base64.RawURLEncoding.EncodeToString(kidHash[:])[:8]
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid})
	payload, _ := json.Marshal(map[string]any{
		"sub":            sub,
		"iss":            "https://auth.latere.ai",
		"aud":            "my-client",
		"exp":            float64(exp.Unix()),
		"iat":            float64(time.Now().Unix()),
		"principal_type": "user",
	})
	in := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(in))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return in + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// TestAPIRoutes_ClaimsContext_InCloudMode mirrors the spec's
// cloud-mode happy path: a JWT in the Authorization header surfaces as
// claims in the downstream handler's context.
func TestAPIRoutes_ClaimsContext_InCloudMode(t *testing.T) {
	key, jwks := mintRSAKeyAndJWKS(t)
	v := auth.BuildValidator(
		auth.Config{AuthURL: jwks.URL, ClientID: "my-client"},
		jwks.URL,
		"https://auth.latere.ai",
	)

	var captured *auth.Claims
	stack := stackWithValidator(t, v, "", &captured)

	tok := signJWT(t, key, "user-abc", time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if captured == nil || captured.Sub != "user-abc" {
		t.Fatalf("handler saw claims = %+v, want sub=user-abc", captured)
	}
}

// TestAPIRoutes_JWTWithStaticKeySet_BypassesKeyCheck confirms a JWT
// authenticates the request even when WALLFACER_SERVER_API_KEY is set,
// thanks to BearerAuth's claims bypass.
func TestAPIRoutes_JWTWithStaticKeySet_BypassesKeyCheck(t *testing.T) {
	key, jwks := mintRSAKeyAndJWKS(t)
	v := auth.BuildValidator(
		auth.Config{AuthURL: jwks.URL, ClientID: "my-client"},
		jwks.URL,
		"https://auth.latere.ai",
	)

	var captured *auth.Claims
	stack := stackWithValidator(t, v, "server-api-key-value", &captured)

	tok := signJWT(t, key, "user-abc", time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	// JWT only; no static key. BearerAuth must let this through.
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (JWT bypass)", rec.Code)
	}
	if captured == nil {
		t.Fatal("JWT claims should have reached handler")
	}
}

// TestAPIRoutes_NoValidator_InLocalMode mirrors the spec's local-mode
// behavior: a nil validator collapses OptionalAuth to the identity,
// and requests behave exactly as today (no claims, BearerAuth gate
// unchanged).
func TestAPIRoutes_NoValidator_InLocalMode(t *testing.T) {
	var captured *auth.Claims
	stack := stackWithValidator(t, nil, "", &captured)

	// Stray Bearer header. No validator -> no validation attempted.
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer junk")
	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (local mode no-op)", rec.Code)
	}
	if captured != nil {
		t.Fatalf("local mode should not populate claims, got %+v", captured)
	}
}
