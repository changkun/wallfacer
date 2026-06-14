package cli

import (
	"os"
	"path/filepath"
	"testing"

	"latere.ai/x/wallfacer/internal/auth"
)

// TestResolveAuthConfig_PublicDefault verifies that with no AUTH_* env a plain
// `wallfacer run` gets a working public (secret-less) client: the "wallfacer"
// client id, the loopback callback, a generated+persisted cookie key, and
// insecure cookies for the http callback. The resulting config must build a
// non-nil oidc client.
func TestResolveAuthConfig_PublicDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := resolveAuthConfig(auth.Config{}, ":8080", dir)
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
	if cfg.CookieKey == "" {
		t.Error("expected a generated cookie key")
	}
	if !cfg.InsecureCookies {
		t.Error("expected InsecureCookies for an http loopback callback")
	}
	if _, err := os.Stat(filepath.Join(dir, "cookie-key")); err != nil {
		t.Errorf("cookie key not persisted: %v", err)
	}
	if auth.New(cfg) == nil {
		t.Error("auth.New returned nil for the default public config")
	}
}

// TestResolveAuthConfig_CookieKeyStable confirms the generated cookie key is
// persisted and reused, so sessions survive a restart.
func TestResolveAuthConfig_CookieKeyStable(t *testing.T) {
	dir := t.TempDir()
	a, err := resolveAuthConfig(auth.Config{}, ":8080", dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := resolveAuthConfig(auth.Config{}, ":8080", dir)
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
	in := auth.Config{
		AuthURL:      "https://auth.example.com",
		ClientID:     "custom",
		ClientSecret: "sec",
		RedirectURL:  "https://app.example.com/callback",
		CookieKey:    "deadbeefdeadbeefdeadbeefdeadbeef",
	}
	cfg, err := resolveAuthConfig(in, ":8080", dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "custom" || cfg.AuthURL != "https://auth.example.com" ||
		cfg.RedirectURL != "https://app.example.com/callback" {
		t.Errorf("env values not preserved: %+v", cfg)
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
