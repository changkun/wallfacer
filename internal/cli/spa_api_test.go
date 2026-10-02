package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"latere.ai/x/pkg/httpjson"
	"latere.ai/x/pkg/metrics"

	"latere.ai/x/wallfacer/internal/handler"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store/storetest"
)

// spaMux builds the full route table with the SPA mounted from an in-memory
// dist holding the shell and one hashed asset.
func spaMux(t *testing.T) *http.ServeMux {
	t.Helper()
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(s.Close)
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	dist := fstest.MapFS{
		"frontend/dist/index.html":         {Data: []byte(`<!doctype html><head></head><body><div id="app"></div></body>`)},
		"frontend/dist/assets/app-1a2b.js": {Data: []byte(`console.log("app")`)},
	}
	return BuildMux(h, metrics.NewRegistry(), IndexViewData{}, testFS(t), dist, false)
}

// TestBuildMux_UnknownAPIPathAnswers404JSON asserts an API path no route
// matches answers 404 with the error envelope for every method, instead of the
// SPA shell (200 for GET) or 405 (other methods).
func TestBuildMux_UnknownAPIPathAnswers404JSON(t *testing.T) {
	mux := spaMux(t)
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/does-not-exist"},
		{http.MethodHead, "/api/does-not-exist"},
		{http.MethodPost, "/api/does-not-exist"},
		{http.MethodPatch, "/api/does-not-exist"},
		{http.MethodDelete, "/api/does-not-exist"},
		{http.MethodGet, "/api/tasks/not/a/route/at/all"},
		{http.MethodGet, "/api/"},
		{http.MethodGet, "/api"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body %q", rr.Code, rr.Body.String())
			}
			if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}
			if tc.method == http.MethodHead {
				return
			}
			var env httpjson.ErrorEnvelope
			if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v (%q)", err, rr.Body.String())
			}
			if env.Error.Code != "not_found" || env.Error.Message == "" {
				t.Fatalf("envelope = %+v, want code not_found with a message", env.Error)
			}
		})
	}
}

// TestBuildMux_KnownAPIPathWrongMethodAnswers405 asserts a path a route
// serves under other methods still answers 405, with an Allow header naming
// those methods, and the error envelope.
func TestBuildMux_KnownAPIPathWrongMethodAnswers405(t *testing.T) {
	mux := spaMux(t)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/config", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body %q", rr.Code, rr.Body.String())
	}
	allow := rr.Header().Get("Allow")
	for _, m := range []string{http.MethodGet, http.MethodPut} {
		if !strings.Contains(allow, m) {
			t.Errorf("Allow = %q, want it to name %s", allow, m)
		}
	}
	if strings.Contains(allow, http.MethodDelete) {
		t.Errorf("Allow = %q names the refused method", allow)
	}
	var env httpjson.ErrorEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (%q)", err, rr.Body.String())
	}
	if env.Error.Code != "method_not_allowed" || env.Error.Message == "" {
		t.Fatalf("envelope = %+v, want code method_not_allowed with a message", env.Error)
	}
}

// TestBuildMux_SPADeepLinksAndAssets asserts a hard load of any client route
// gets the shell, the hashed assets are served as files, and a non-GET request
// to a client route still answers 405.
func TestBuildMux_SPADeepLinksAndAssets(t *testing.T) {
	mux := spaMux(t)
	for _, path := range []string{"/", "/plan", "/plan?spec=specs/local/foo.md", "/board", "/chat", "/docs/guide/usage", "/some/client/route"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
			if rr.Code != http.StatusOK {
				t.Errorf("%s %s: status %d, want 200 (SPA shell)", method, path, rr.Code)
				continue
			}
			if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
				t.Errorf("%s %s: Content-Type %q, want text/html", method, path, ct)
			}
			if method == http.MethodGet && !strings.Contains(rr.Body.String(), "window.__WALLFACER__") {
				t.Errorf("GET %s: body is not the SPA shell: %q", path, rr.Body.String())
			}
		}
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/assets/app-1a2b.js", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != `console.log("app")` {
		t.Fatalf("GET asset: status %d body %q, want the file", rr.Code, rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); cc != immutableAssetCache {
		t.Errorf("GET asset: Cache-Control %q, want %q", cc, immutableAssetCache)
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/plan", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /plan: status %d, want 405", rr.Code)
	}
	if allow := rr.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Errorf("POST /plan: Allow %q, want it to name GET", allow)
	}
}
