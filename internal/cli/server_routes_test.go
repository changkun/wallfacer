package cli

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"latere.ai/x/pkg/metrics"

	"latere.ai/x/wallfacer/internal/apicontract"
	"latere.ai/x/wallfacer/internal/handler"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store/storetest"
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
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()

	// BuildMux panics if any route in the contract lacks a handler entry, so
	// getting past this call already validates the handlers map is complete.
	mux := BuildMux(h, reg, IndexViewData{}, nil)

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
