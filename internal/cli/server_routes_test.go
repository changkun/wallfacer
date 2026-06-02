package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/apicontract"
	"changkun.de/x/wallfacer/internal/handler"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
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
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
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
		route := route // capture loop variable
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
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
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
		rt := rt
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
