package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrCreateServerAPIKey_GeneratesAndReuses: first call mints a
// 32-byte hex key with owner-only permissions, later calls return the same
// key so scripts and restarts keep working.
func TestLoadOrCreateServerAPIKey_GeneratesAndReuses(t *testing.T) {
	dir := t.TempDir()
	a, err := loadOrCreateServerAPIKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 {
		t.Fatalf("key length = %d, want 64 hex chars", len(a))
	}
	info, err := os.Stat(filepath.Join(dir, serverAPIKeyFile))
	if err != nil {
		t.Fatalf("key not persisted: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file mode = %o, want 600", perm)
	}
	b, err := loadOrCreateServerAPIKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("key not stable across calls: %q vs %q", a, b)
	}
}

// TestReadServerAPIKey: the environment wins, the generated file is the
// fallback, and neither yields "".
func TestReadServerAPIKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WALLFACER_SERVER_API_KEY", "")
	if got := readServerAPIKey(dir); got != "" {
		t.Errorf("no env, no file: got %q, want empty", got)
	}
	if err := os.WriteFile(filepath.Join(dir, serverAPIKeyFile), []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readServerAPIKey(dir); got != "from-file" {
		t.Errorf("file fallback: got %q, want from-file", got)
	}
	t.Setenv("WALLFACER_SERVER_API_KEY", "from-env")
	if got := readServerAPIKey(dir); got != "from-env" {
		t.Errorf("env precedence: got %q, want from-env", got)
	}
}

// TestIndexKeyAllowed pins who receives the server API key in index.html:
// loopback peers, and callers that already present the key.
func TestIndexKeyAllowed(t *testing.T) {
	const key = "s3cret"
	cases := []struct {
		name   string
		remote string
		bearer string
		query  string
		key    string
		want   bool
	}{
		{"loopback v4", "127.0.0.1:5000", "", "", key, true},
		{"loopback v6", "[::1]:5000", "", "", key, true},
		{"lan peer anonymous", "192.168.1.5:5000", "", "", key, false},
		{"public peer anonymous", "203.0.113.9:5000", "", "", key, false},
		{"lan peer with bearer", "192.168.1.5:5000", key, "", key, true},
		{"lan peer wrong bearer", "192.168.1.5:5000", "nope", "", key, false},
		{"lan peer bearer prefix", "192.168.1.5:5000", "s3cre", "", key, false},
		{"lan peer with token query", "192.168.1.5:5000", "", key, key, true},
		{"lan peer wrong token query", "192.168.1.5:5000", "", "nope", key, false},
		{"no key configured", "127.0.0.1:5000", "", "", "", false},
		{"unparseable remote", "garbage", "", "", key, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "/"
			if tc.query != "" {
				target = "/?token=" + tc.query
			}
			r := httptest.NewRequest(http.MethodGet, target, nil)
			r.RemoteAddr = tc.remote
			if tc.bearer != "" {
				r.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			if got := indexKeyAllowed(r, tc.key); got != tc.want {
				t.Errorf("indexKeyAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}
