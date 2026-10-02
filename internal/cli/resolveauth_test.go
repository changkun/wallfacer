package cli

import (
	"os"
	"path/filepath"
	"testing"

	"latere.ai/x/pkg/authkit/oidc"
)

// TestResolveAuthConfig_PublicDefault verifies that with no AUTH_* env a plain
// `wallfacer run` gets a working public (secret-less) client: the "wallfacer"
// client id, the loopback callback, a generated+persisted cookie key, and
// insecure cookies for the http callback. The resulting config must build a
// non-nil oidc client.
func TestResolveAuthConfig_PublicDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := resolveAuthConfig(oidc.Config{}, ":8080", dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "wallfacer" {
		t.Errorf("ClientID = %q, want wallfacer", cfg.ClientID)
	}
	if cfg.AuthURL != "https://auth.latere.ai" {
		t.Errorf("AuthURL = %q, want https://auth.latere.ai", cfg.AuthURL)
	}
	if cfg.RedirectURL != "http://localhost:8080/callback" {
		t.Errorf("RedirectURL = %q, want http://localhost:8080/callback", cfg.RedirectURL)
	}
	// No audience is requested at login: the session token is the issuer's,
	// and the API takes an actor token minted for this service instead.
	if cfg.Audience != "" {
		t.Errorf("Audience = %q, want none requested", cfg.Audience)
	}
	if cfg.CookieKey == "" {
		t.Error("expected a generated cookie key")
	}
	if !cfg.InsecureCookies {
		t.Error("expected InsecureCookies for an http loopback callback")
	}
	if _, err := os.Stat(filepath.Join(dir, "cookie-key")); err != nil {
		t.Errorf("cookie key not persisted: %v", err)
	}
	if oidc.New(cfg) == nil {
		t.Error("oidc.New returned nil for the default public config")
	}
}

// TestResolveAuthConfig_CookieKeyStable confirms the generated cookie key is
// persisted and reused, so sessions survive a restart.
func TestResolveAuthConfig_CookieKeyStable(t *testing.T) {
	dir := t.TempDir()
	a, err := resolveAuthConfig(oidc.Config{}, ":8080", dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := resolveAuthConfig(oidc.Config{}, ":8080", dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.CookieKey == "" || a.CookieKey != b.CookieKey {
		t.Errorf("cookie key not stable: %q vs %q", a.CookieKey, b.CookieKey)
	}
}

// TestResolveAuthConfig_EnvOverride confirms explicit AUTH_* values win over the
// defaults and a confidential client (secret set) neither generates nor persists
// a cookie key and keeps Secure cookies.
func TestResolveAuthConfig_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	in := oidc.Config{
		AuthURL:      "https://auth.example.com",
		ClientID:     "custom",
		ClientSecret: "sec",
		RedirectURL:  "https://app.example.com/callback",
		CookieKey:    "deadbeefdeadbeefdeadbeefdeadbeef",
		Audience:     "custom-aud",
	}
	cfg, err := resolveAuthConfig(in, ":8080", dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "custom" || cfg.AuthURL != "https://auth.example.com" ||
		cfg.RedirectURL != "https://app.example.com/callback" {
		t.Errorf("env values not preserved: %+v", cfg)
	}
	if cfg.Audience != "" {
		t.Errorf("Audience = %q, want none requested whatever the input says", cfg.Audience)
	}
	if cfg.CookieKey != "deadbeefdeadbeefdeadbeefdeadbeef" {
		t.Errorf("cookie key overwritten: %q", cfg.CookieKey)
	}
	if cfg.InsecureCookies {
		t.Error("https redirect must not enable InsecureCookies")
	}
	if _, err := os.Stat(filepath.Join(dir, "cookie-key")); err == nil {
		t.Error("must not persist a cookie key when one is provided")
	}
}

func TestDefaultRedirectURL(t *testing.T) {
	cases := map[string]string{
		":8080":            "http://localhost:8080/callback",
		"127.0.0.1:9000":   "http://localhost:9000/callback",
		"0.0.0.0:8080":     "http://localhost:8080/callback",
		"localhost:3000":   "http://localhost:3000/callback",
		"wf.latere.ai:443": "https://wf.latere.ai:443/callback",
	}
	for addr, want := range cases {
		if got := defaultRedirectURL(addr); got != want {
			t.Errorf("defaultRedirectURL(%q) = %q, want %q", addr, got, want)
		}
	}
}

// TestRedirectPortMismatch pins the decision behind the redirect sign-in gate:
// a redirect URL can receive the callback only when it names the port the
// listener is bound to.
func TestRedirectPortMismatch(t *testing.T) {
	cases := []struct {
		name        string
		redirectURL string
		boundPort   int
		mismatch    bool
	}{
		{"requested port bound", "http://localhost:8080/callback", 8080, false},
		{"tls host on its port", "https://wf.latere.ai:443/callback", 443, false},
		{"fallback port", "http://localhost:8080/callback", 53211, true},
		{"port zero requested", "http://localhost:0/callback", 53211, true},
		{"no port in the URL", "http://localhost/callback", 8080, true},
		{"unparsable URL", "http://local host:8080/callback", 8080, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := redirectPortMismatch(tc.redirectURL, tc.boundPort)
			if got := reason != ""; got != tc.mismatch {
				t.Errorf("redirectPortMismatch(%q, %d) = %q, want a mismatch: %v", tc.redirectURL, tc.boundPort, reason, tc.mismatch)
			}
		})
	}
}
