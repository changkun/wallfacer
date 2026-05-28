package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAssetCacheControl verifies path-scoped cache policy for the Vue
// SPA static handler: hashed /assets/* are immutable, preloaded /fonts/
// get stale-while-revalidate, and other paths get no override.
func TestAssetCacheControl(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/assets/app-abc123.js", immutableAssetCache},
		{"/fonts/inter-400.woff2", staticAssetCache},
		{"/static/og.png", ""},
		{"/", ""},
	}
	for _, tc := range cases {
		if got := assetCacheControl(tc.path); got != tc.want {
			t.Errorf("assetCacheControl(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// TestWithAssetCache verifies the wrapper stamps the right Cache-Control
// header before delegating to the underlying file server.
func TestWithAssetCache(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := withAssetCache(next)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fonts/inter-400.woff2", nil))
	if got := rec.Header().Get("Cache-Control"); got != staticAssetCache {
		t.Errorf("font Cache-Control = %q, want %q", got, staticAssetCache)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-abc.js", nil))
	if got := rec.Header().Get("Cache-Control"); got != immutableAssetCache {
		t.Errorf("asset Cache-Control = %q, want %q", got, immutableAssetCache)
	}
}
