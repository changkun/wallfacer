package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"latere.ai/x/pkg/httpjson"
	"latere.ai/x/pkg/metrics"

	"latere.ai/x/wallfacer/internal/apicontract"
	"latere.ai/x/wallfacer/internal/handler"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store/storetest"
	"latere.ai/x/wallfacer/internal/workspace"
)

// TestContractRoutes_AllRegisteredInMux verifies that every route declared in
// apicontract.Routes is actually registered in the HTTP multiplexer built by
// buildMux. This catches drift where a new route is added to the contract but
// no handler entry is wired up (which would panic at server startup), and also
// ensures routes cannot be accidentally removed from the handlers map without
// a corresponding contract removal.
func TestContractRoutes_AllRegisteredInMux(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()

	// BuildMux panics if any route in the contract lacks a handler entry, so
	// getting past this call already validates the handlers map is complete.
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), nil, false)

	// Substitute path parameters with concrete values so the mux can match the
	// pattern. We only need the matched pattern string — we do not execute handlers.
	dummyID := uuid.New().String()
	dummyFile := "turn-0001.json"

	for _, route := range apicontract.Routes {
		t.Run(fmt.Sprintf("%s %s", route.Method, route.Pattern), func(t *testing.T) {
			path := route.Pattern
			path = strings.ReplaceAll(path, "{id}", dummyID)
			path = strings.ReplaceAll(path, "{filename}", dummyFile)

			req := httptest.NewRequest(route.Method, path, nil)
			_, matchedPattern := mux.Handler(req)

			if matchedPattern == "" {
				t.Errorf("route %q (%s %s) is not registered in the mux",
					route.Name, route.Method, route.Pattern)
				return
			}
			wantPattern := route.FullPattern()
			if matchedPattern != wantPattern {
				t.Errorf("route %q: mux matched %q, want %q",
					route.Name, matchedPattern, wantPattern)
			}
		})
	}
}

// TestIdeateRoutesRemoved guards against reintroduction of the retired
// idea-agent / brainstorm HTTP surface. The /api/ideate status, trigger, and
// cancel endpoints were removed with the idea-agent subsystem; any of them
// returning anything other than 404/405 means a handler was wired back in.
//
// See specs/local/remove-idea-agent-subsystem.md for the rationale —
// brainstorming is now an ordinary scheduled routine.
func TestIdeateRoutesRemoved(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), nil, false)

	retiredRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/ideate"},
		{"POST", "/api/ideate"},
		{"DELETE", "/api/ideate"},
	}
	for _, rt := range retiredRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s returned %d, want 404 or 405 (route should be retired; Allow=%q)",
					rt.method, rt.path, w.Code, w.Header().Get("Allow"))
			}
		})
	}
}

// TestRefineRoutesRemoved is the guard against accidental reintroduction of
// the retired refinement subsystem's HTTP endpoints. Any of the five routes
// returning anything other than 404 means a handler has been wired back in.
//
// See specs/local/refinement-into-plan/retire-refine-subsystem.md for the
// rationale — task-mode planning (Send to Plan) replaces these routes.
func TestRefineRoutesRemoved(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), nil, false)

	dummyID := uuid.New().String()
	retiredRoutes := []struct {
		method string
		path   string
	}{
		{"POST", "/api/tasks/" + dummyID + "/refine"},
		{"DELETE", "/api/tasks/" + dummyID + "/refine"},
		{"GET", "/api/tasks/" + dummyID + "/refine/logs"},
		{"POST", "/api/tasks/" + dummyID + "/refine/apply"},
		{"POST", "/api/tasks/" + dummyID + "/refine/dismiss"},
	}
	for _, rt := range retiredRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			// Accept 404 (no handler) or 405 (method not allowed because another
			// method remains registered on the same path). Both prove the retired
			// method+path has no wired handler. Only a 2xx/3xx/4xx<405 would mean
			// a handler regressed.
			if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s returned %d, want 404 or 405 (route should be retired; Allow=%q)",
					rt.method, rt.path, w.Code, w.Header().Get("Allow"))
			}
		})
	}
}

// TestFleetRoutesRemoved verifies that the API mux has no route for
// user-authored agents or fleets: every method on /api/agents and /api/flows,
// with and without a slug, matches no pattern and answers 404, and the
// contract declares none of them.
func TestFleetRoutesRemoved(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), nil, false)

	var removed []struct{ method, path string }
	for _, base := range []string{"/api/agents", "/api/flows"} {
		removed = append(removed,
			struct{ method, path string }{http.MethodGet, base},
			struct{ method, path string }{http.MethodPost, base},
			struct{ method, path string }{http.MethodGet, base + "/implement"},
			struct{ method, path string }{http.MethodPut, base + "/implement"},
			struct{ method, path string }{http.MethodDelete, base + "/implement"},
		)
	}
	for _, rt := range removed {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			if _, pattern := mux.Handler(req); pattern != "" {
				t.Fatalf("%s %s matched pattern %q, want no match", rt.method, rt.path, pattern)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s returned %d, want 404", rt.method, rt.path, w.Code)
			}
		})
	}

	for _, route := range apicontract.Routes {
		if strings.HasPrefix(route.Pattern, "/api/agents") || strings.HasPrefix(route.Pattern, "/api/flows") {
			t.Errorf("contract still declares %s %s (%s)", route.Method, route.Pattern, route.Name)
		}
	}
}

// TestGitHubRoutesRemoved verifies that the API mux has no GitHub route:
// neither the connection surface under /api/github nor the task pull-request
// surface under /api/tasks/{id}/pr. Wallfacer makes no call to GitHub, so each
// of these paths must match no pattern and answer 404.
func TestGitHubRoutesRemoved(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), nil, false)

	const githubBase = "/api/github"
	taskBase := "/api/tasks/" + uuid.New().String()
	removed := []struct {
		method string
		path   string
	}{
		{http.MethodGet, githubBase + "/auth/status"},
		{http.MethodPost, githubBase + "/auth/connect"},
		{http.MethodPost, githubBase + "/auth/disconnect"},
		{http.MethodPost, githubBase + "/pulls"},
		{http.MethodPost, githubBase + "/comments"},
		{http.MethodGet, taskBase + "/pr"},
		{http.MethodPost, taskBase + "/pr"},
		{http.MethodPost, taskBase + "/pr/comment"},
	}
	for _, rt := range removed {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			if _, pattern := mux.Handler(req); pattern != "" {
				t.Fatalf("%s %s matched pattern %q, want no match", rt.method, rt.path, pattern)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s returned %d, want 404", rt.method, rt.path, w.Code)
			}
		})
	}

	for _, route := range apicontract.Routes {
		if strings.HasPrefix(route.Pattern, githubBase) ||
			strings.HasSuffix(route.Pattern, "/pr") || strings.Contains(route.Pattern, "/pr/") {
			t.Errorf("contract still declares %s %s (%s)", route.Method, route.Pattern, route.Name)
		}
	}
}

// fileState is what treeState records for one entry: enough to tell a file
// or directory that was rewritten, replaced, or re-permissioned from one left
// alone.
type fileState struct {
	mode    fs.FileMode
	size    int64
	modTime int64
	body    string
}

// treeState records every entry under root, keyed by its slash path relative
// to root. A directory's modification time changes when an entry is created
// in it or removed from it, so the map also catches additions and deletions.
func treeState(t *testing.T, root string) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		st := fileState{mode: info.Mode(), size: info.Size(), modTime: info.ModTime().UnixNano()}
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			st.body = string(b)
		}
		out[filepath.ToSlash(rel)] = st
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// TestArtifactRoutesRemoved starts the server over a workspace whose
// artifacts/ directory holds pages and checks that nothing serves or touches
// them. GET /api/artifacts matches no route and answers 404 in the API error
// envelope. GET /artifact/<path> and GET /artifacts are paths outside /api
// that no route serves, so they get the SPA shell like any other, and the
// console renders its not-found page there; no response carries a file's
// bytes. After shutdown every entry under artifacts/ is unchanged. Requests
// go through the full handler chain with the server key, so each answer is
// the mux's and not the bearer middleware's.
func TestArtifactRoutesRemoved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("AppData", home)
	t.Setenv("AUTH_REDIRECT_URL", "")
	t.Setenv("WALLFACER_CLOUD", "")

	// The workspace path is resolved so it compares equal to the path the
	// workspace manager reports (t.TempDir sits behind a symlink on macOS).
	ws, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve workspace dir: %v", err)
	}
	const marker = "artifact-file-body"
	pages := map[string]string{
		"deck.html":  "<title>Deck</title><h1>" + marker + " deck</h1>",
		"sub/r.html": "<p>" + marker + " nested</p>",
		"notes.md":   marker + " notes",
	}
	artifactsDir := filepath.Join(ws, "artifacts")
	for rel, body := range pages {
		p := filepath.Join(artifactsDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	before := treeState(t, artifactsDir)

	// Startup restores the most recent saved workspace from workspaces.json.
	configDir := t.TempDir()
	if err := workspace.SaveGroups(configDir, []workspace.Workspace{{Folders: []string{ws}}}); err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      ":0",
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, stubVueFS(t), testFS(t))
	shutdown := sync.OnceFunc(sc.Shutdown)
	t.Cleanup(shutdown)
	key := readServerAPIKey(configDir)
	if key == "" {
		t.Fatal("startup persisted no server API key")
	}

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rr := httptest.NewRecorder()
		sc.Srv.Handler.ServeHTTP(rr, req)
		return rr
	}

	// The start restored the workspace, so any route that reads a workspace
	// directory would see the files under its artifacts/ directory.
	cfg := get("/api/config")
	if cfg.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d, want 200; body %q", cfg.Code, cfg.Body.String())
	}
	var config struct {
		Workspaces []string `json:"workspaces"`
	}
	if err := json.Unmarshal(cfg.Body.Bytes(), &config); err != nil {
		t.Fatalf("decode /api/config: %v", err)
	}
	if !slices.Contains(config.Workspaces, ws) {
		t.Fatalf("active workspaces = %v, want them to include %s", config.Workspaces, ws)
	}

	t.Run("GET /api/artifacts", func(t *testing.T) {
		rr := get("/api/artifacts")
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body %q", rr.Code, rr.Body.String())
		}
		if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
		var env httpjson.ErrorEnvelope
		if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v (%q)", err, rr.Body.String())
		}
		if env.Error.Code != codeAPINotFound {
			t.Fatalf("error code = %q, want %q", env.Error.Code, codeAPINotFound)
		}
	})

	for _, path := range []string{"/artifact/deck.html", "/artifact/sub/r.html", "/artifact/notes.md", "/artifacts"} {
		t.Run("GET "+path, func(t *testing.T) {
			rr := get(path)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (SPA shell); body %q", rr.Code, rr.Body.String())
			}
			if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("Content-Type = %q, want text/html", ct)
			}
			body := rr.Body.String()
			if !strings.Contains(body, "window.__WALLFACER__") {
				t.Fatalf("body is not the SPA shell: %q", body)
			}
			if strings.Contains(body, marker) {
				t.Fatalf("response carries a workspace file's bytes: %q", body)
			}
		})
	}

	shutdown()
	if after := treeState(t, artifactsDir); !maps.Equal(after, before) {
		t.Errorf("artifacts/ changed across a start:\nbefore %v\nafter  %v", before, after)
	}

	for _, route := range apicontract.Routes {
		if strings.Contains(route.Pattern, "artifact") {
			t.Errorf("contract still declares %s %s (%s)", route.Method, route.Pattern, route.Name)
		}
	}
}
