package cli

import (
	"bufio"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/apicontract"
	"changkun.de/x/wallfacer/internal/handler"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// serverRoutesRepoRoot returns the repository root directory from this file's location.
func serverRoutesRepoRoot(t *testing.T) string {
	return repoRoot(t)
}

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
	s, err := store.NewStore(filepath.Join(workdir, "data"))
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
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), testFS(t))

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

// TestNoRawAPILiterals_InUISourceFiles guards against UI call sites regressing
// to raw "/api/..." string literals. After the routes.js migration, every API
// path must go through the generated Routes.* helpers or task(id).* builders.
//
// Each non-comment line in the monitored files is checked. A line is flagged
// if it contains a quoted /api/ prefix (e.g. "/api/tasks" or '/api/env').
// The generated routes.js file is intentionally excluded.
func TestNoRawAPILiterals_InUISourceFiles(t *testing.T) {
	root := serverRoutesRepoRoot(t)

	sources := []string{
		filepath.Join(root, "ui", "js", "api.js"),
		filepath.Join(root, "ui", "js", "tasks.js"),
		filepath.Join(root, "ui", "js", "envconfig.js"),
		filepath.Join(root, "ui", "js", "git.js"),
		filepath.Join(root, "ui", "js", "refine.js"),
	}

	// Matches a single- or double-quoted string starting with /api/.
	rawAPILiteral := regexp.MustCompile(`["']/api/`)

	for _, src := range sources {
		src := src
		t.Run(filepath.Base(src), func(t *testing.T) {
			f, err := os.Open(src)
			if err != nil {
				t.Fatalf("open %s: %v", src, err)
			}
			defer func() {
				_ = f.Close()
			}()

			scanner := bufio.NewScanner(f)
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				line := scanner.Text()
				// Skip single-line JavaScript comments.
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				if rawAPILiteral.MatchString(line) {
					t.Errorf("%s:%d: raw /api/ literal found (use Routes.* helpers instead):\n  %s",
						filepath.Base(src), lineNum, strings.TrimSpace(line))
				}
			}
			if err := scanner.Err(); err != nil {
				t.Fatalf("scan %s: %v", src, err)
			}
		})
	}
}
