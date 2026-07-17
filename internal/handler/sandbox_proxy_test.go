package handler

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
	"strings"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/auth"
)

// proxyKeyAndJWKS returns an RSA key and a JWKS server exposing its
// public half, for building a real auth.Validator in tests.
func proxyKeyAndJWKS(t *testing.T) (*rsa.PrivateKey, *httptest.Server) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks, _ := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": proxyKid(key),
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

func proxyKid(key *rsa.PrivateKey) string {
	h := sha256.Sum256(key.N.Bytes())
	return base64.RawURLEncoding.EncodeToString(h[:])[:8]
}

// signProxyJWT mints a valid sandbox-sidecar JWT with the given aud
// and scopes.
func signProxyJWT(t *testing.T, key *rsa.PrivateKey, sub, aud string, scopes []string) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": proxyKid(key)})
	payload, _ := json.Marshal(map[string]any{
		"sub":            sub,
		"iss":            "https://auth.latere.ai",
		"aud":            aud,
		"exp":            float64(time.Now().Add(time.Hour).Unix()),
		"iat":            float64(time.Now().Unix()),
		"principal_type": "user",
		"scp":            scopes,
	})
	in := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(in))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return in + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func proxyValidator(t *testing.T, jwksURL string) *auth.Validator {
	t.Helper()
	return auth.BuildValidator(auth.Config{AuthURL: jwksURL}, jwksURL, "https://auth.latere.ai")
}

// proxyMux wires the three trust-plane routes exactly as
// internal/cli/server.go does, so tests exercise the same routing.
func proxyMux(p *SandboxProxy) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/sandbox-proxy/llm/anthropic/", p.LLMAnthropic)
	mux.HandleFunc("POST /internal/sandbox-proxy/llm/openai/", p.LLMOpenAI)
	mux.HandleFunc("GET /internal/sandbox-proxy/github-token", p.GitHubToken)
	return mux
}

func proxyRequest(t *testing.T, mux *http.ServeMux, method, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// A disabled proxy 503s on every route before any JWT logic runs.
func TestSandboxProxyDisabled503(t *testing.T) {
	mux := proxyMux(NewSandboxProxy(SandboxProxyConfig{}, nil))
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/internal/sandbox-proxy/llm/anthropic/v1/messages"},
		{http.MethodPost, "/internal/sandbox-proxy/llm/openai/v1/chat/completions"},
		{http.MethodGet, "/internal/sandbox-proxy/github-token?repo=o/r"},
	} {
		rec := proxyRequest(t, mux, tc.method, tc.path, "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", tc.method, tc.path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "sandbox proxy disabled") {
			t.Errorf("%s %s: body = %q, want disabled message", tc.method, tc.path, rec.Body.String())
		}
	}
}

// Fail-closed regression: an ENABLED proxy with a nil validator must
// reject every route instead of treating requests as
// anonymous-but-authorized.
func TestSandboxProxyEnabledNilValidatorRejects(t *testing.T) {
	cfg := SandboxProxyConfig{
		Enabled:                  true,
		AuthInstallationTokenURL: "https://auth.example/internal/github/installation-token",
		AuthServiceToken:         "svc-token",
		AnthropicKey:             "sk-ant",
		OpenAIKey:                "sk-oai",
	}
	mux := proxyMux(NewSandboxProxy(cfg, nil))
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/internal/sandbox-proxy/llm/anthropic/v1/messages"},
		{http.MethodPost, "/internal/sandbox-proxy/llm/openai/v1/chat/completions"},
		{http.MethodGet, "/internal/sandbox-proxy/github-token?repo=o/r"},
	} {
		rec := proxyRequest(t, mux, tc.method, tc.path, "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503 (fail closed)", tc.method, tc.path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "validator not configured") {
			t.Errorf("%s %s: body = %q, want validator-not-configured message", tc.method, tc.path, rec.Body.String())
		}
	}
}

// Happy path end to end: a valid JWT with aud=wallfacer-sandbox-proxy
// and scp=github:token reaches auth's installation-token endpoint and
// the JSON body is passed through.
func TestSandboxProxyGitHubTokenValidJWT(t *testing.T) {
	key, jwks := proxyKeyAndJWKS(t)

	var gotAuth, gotPrincipal, gotRepo string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPrincipal = r.URL.Query().Get("principal")
		gotRepo = r.URL.Query().Get("repo")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"ghs_test","expires_at":"2026-01-01T00:00:00Z"}`))
	}))
	t.Cleanup(upstream.Close)

	cfg := SandboxProxyConfig{
		Enabled:                  true,
		AuthInstallationTokenURL: upstream.URL,
		AuthServiceToken:         "svc-token",
		AnthropicKey:             "sk-ant",
	}
	mux := proxyMux(NewSandboxProxy(cfg, proxyValidator(t, jwks.URL)))

	tok := signProxyJWT(t, key, "user-42", "wallfacer-sandbox-proxy", []string{"github:token"})
	rec := proxyRequest(t, mux, http.MethodGet, "/internal/sandbox-proxy/github-token?repo=owner/name", tok)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"token":"ghs_test"`) {
		t.Errorf("body = %q, want auth JSON passthrough", rec.Body.String())
	}
	if gotAuth != "Bearer svc-token" {
		t.Errorf("auth call Authorization = %q, want service token bearer", gotAuth)
	}
	if gotPrincipal != "user-42" {
		t.Errorf("auth call principal = %q, want user-42 (non-delegated sub)", gotPrincipal)
	}
	if gotRepo != "owner/name" {
		t.Errorf("auth call repo = %q, want owner/name", gotRepo)
	}
}

// A valid JWT clears the auth gate on the LLM route: with no provider
// key configured the request fails AFTER requireClaims with the
// distinct provider-key 503, proving the JWT was accepted.
func TestSandboxProxyLLMValidJWTPassesAuthGate(t *testing.T) {
	key, jwks := proxyKeyAndJWKS(t)
	cfg := SandboxProxyConfig{
		Enabled:                  true,
		AuthInstallationTokenURL: "https://auth.example/internal/github/installation-token",
		AuthServiceToken:         "svc-token",
	}
	mux := proxyMux(NewSandboxProxy(cfg, proxyValidator(t, jwks.URL)))

	tok := signProxyJWT(t, key, "user-42", "wallfacer-sandbox-proxy", []string{"llm:proxy"})
	rec := proxyRequest(t, mux, http.MethodPost, "/internal/sandbox-proxy/llm/anthropic/v1/messages", tok)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 provider-key error (body %q)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "provider key not configured") {
		t.Errorf("body = %q, want provider-key message (JWT gate must have passed)", rec.Body.String())
	}
}

// Rejection matrix through the real validator: missing bearer,
// garbage token, wrong audience, wrong scope.
func TestSandboxProxyRejections(t *testing.T) {
	key, jwks := proxyKeyAndJWKS(t)
	cfg := SandboxProxyConfig{
		Enabled:                  true,
		AuthInstallationTokenURL: "https://auth.example/internal/github/installation-token",
		AuthServiceToken:         "svc-token",
		AnthropicKey:             "sk-ant",
	}
	mux := proxyMux(NewSandboxProxy(cfg, proxyValidator(t, jwks.URL)))

	cases := []struct {
		name     string
		bearer   string
		wantCode int
		wantBody string
	}{
		{"missing bearer", "", http.StatusUnauthorized, "missing bearer"},
		{"garbage token", "not-a-jwt", http.StatusUnauthorized, ""},
		{
			"wrong audience",
			signProxyJWT(t, key, "user-42", "some-other-service", []string{"github:token"}),
			http.StatusForbidden, "aud mismatch",
		},
		{
			"wrong scope",
			signProxyJWT(t, key, "user-42", "wallfacer-sandbox-proxy", []string{"llm:proxy"}),
			http.StatusForbidden, "missing scope github:token",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := proxyRequest(t, mux, http.MethodGet, "/internal/sandbox-proxy/github-token?repo=o/r", tc.bearer)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body = %q, want %q", rec.Body.String(), tc.wantBody)
			}
		})
	}
}
