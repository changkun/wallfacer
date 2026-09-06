package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The web server serves the fleet's probe paths through pkg/health, with
// /healthz kept as an alias of /livez for one release while the manifest
// moves to the new paths.
func TestWebProbes(t *testing.T) {
	mux := http.NewServeMux()
	mountWebProbes(mux)
	for _, p := range []string{"/livez", "/readyz", "/version", "/healthz"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s -> %d, want 200", p, rec.Code)
		}
	}
}
