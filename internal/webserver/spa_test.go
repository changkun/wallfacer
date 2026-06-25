package webserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMountSPAUsesFrontendDistFromEmbeddedRoot(t *testing.T) {
	frontend := fstest.MapFS{
		"frontend/dist/index.html":    {Data: []byte("<!doctype html><div id=\"app\"></div>")},
		"frontend/dist/assets/app.js": {Data: []byte("console.log('ok')")},
	}
	mux := http.NewServeMux()

	if !MountSPA(mux, frontend) {
		t.Fatal("MountSPA returned false for frontend/dist fixture")
	}
	SPAFallback(mux, frontend)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/docs/usage", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("fallback status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), `id="app"`) {
		t.Fatalf("fallback body = %q, want embedded index.html", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != "console.log('ok')" {
		t.Fatalf("asset body = %q, want app.js", got)
	}
}
