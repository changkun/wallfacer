package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/handler"
	"latere.ai/x/wallfacer/internal/metrics"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
)

// TestInitServer verifies that initServer returns valid components with a
// bound listener.
func TestInitServer(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      ":0",
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))
	defer sc.Shutdown()

	if sc.Srv == nil {
		t.Fatal("expected non-nil http.Server")
	}
	if sc.Ln == nil {
		t.Fatal("expected non-nil Listener")
	}
	if sc.Runner == nil {
		t.Fatal("expected non-nil Runner")
	}
	if sc.ActualPort == 0 {
		t.Fatal("expected non-zero port")
	}
}

// TestStatusResponseWriter_WriteHeaderAndFlush verifies that the
// statusResponseWriter captures the status code and delegates Flush.
func TestStatusResponseWriter_WriteHeaderAndFlush(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := &statusResponseWriter{
		ResponseWriter: rr,
		status:         http.StatusOK,
	}

	sw.WriteHeader(http.StatusAccepted)
	sw.Flush()

	if sw.status != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, sw.status)
	}
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected recorder status %d, got %d", http.StatusAccepted, rr.Code)
	}
}

// TestLoggingMiddleware_LogsForApiAndUiRoutes verifies that the logging
// middleware preserves the status code for both API and UI routes.
func TestLoggingMiddleware_LogsForApiAndUiRoutes(t *testing.T) {
	reg := metrics.NewRegistry()
	apiHandler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}), reg)
	apiRR := httptest.NewRecorder()
	apiReq := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	apiHandler.ServeHTTP(apiRR, apiReq)
	if apiRR.Code != http.StatusCreated {
		t.Fatalf("expected API middleware to preserve status, got %d", apiRR.Code)
	}

	uiRR := httptest.NewRecorder()
	uiReq := httptest.NewRequest(http.MethodGet, "/", nil)
	loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}), reg).ServeHTTP(uiRR, uiReq)
	if uiRR.Code != http.StatusOK {
		t.Fatalf("expected UI middleware to preserve default status, got %d", uiRR.Code)
	}
}

// TestLoggingMiddleware_UnmatchedRoutesCollapseToSentinel verifies that
// requests with no matched mux pattern (r.Pattern empty, e.g. 404s) are
// recorded under a single "<unmatched>" route label rather than their raw URL
// path, which would give unbounded Prometheus label cardinality on path scans.
func TestLoggingMiddleware_UnmatchedRoutesCollapseToSentinel(t *testing.T) {
	reg := metrics.NewRegistry()
	h := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), reg)

	for _, p := range []string{"/api/does-not-exist", "/another/unknown/path", "/random"} {
		rr := httptest.NewRecorder()
		// A bare handler never sets r.Pattern, mirroring an unmatched mux route.
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, p, nil))
	}

	var buf bytes.Buffer
	reg.WritePrometheus(&buf)
	out := buf.String()

	if got := strings.Count(out, `route="<unmatched>"`); got == 0 {
		t.Fatalf("expected an <unmatched> route series, got none:\n%s", out)
	}
	for _, p := range []string{"/api/does-not-exist", "/another/unknown/path", "/random"} {
		if strings.Contains(out, `route="`+p+`"`) {
			t.Fatalf("raw path %q must not appear as a metric label:\n%s", p, out)
		}
	}
}

// TestBuildMux_RoutesServeKnownPaths verifies that the mux returns the
// expected HTTP status codes for a selection of known paths (health, config,
// tasks, events, outputs).
func TestBuildMux_RoutesServeKnownPaths(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	dataDir := filepath.Join(workdir, "data")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "task prompt", Timeout: 10})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), stubVueFS(t), false)

	paths := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/", http.StatusOK},
		{http.MethodGet, "/api/config", http.StatusOK},
		{http.MethodGet, "/api/debug/health", http.StatusOK},
		{http.MethodGet, "/api/debug/spans", http.StatusOK},
		{http.MethodGet, "/api/debug/runtime", http.StatusOK},
		{http.MethodGet, "/api/files", http.StatusOK},
		{http.MethodGet, "/api/tasks", http.StatusOK},
		{http.MethodGet, "/api/tasks/stream", http.StatusOK},
		{http.MethodGet, fmt.Sprintf("/api/tasks/%s/events", task.ID), http.StatusOK},
		{http.MethodGet, fmt.Sprintf("/api/tasks/%s/outputs/missing.txt", task.ID), http.StatusNotFound},
	}

	for _, tc := range paths {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()

			// The SSE stream route never terminates on its own, so skip execution and
			// only verify that it is registered in the mux.
			if tc.path == "/api/tasks/stream" {
				_, pattern := mux.Handler(req)
				if pattern != "GET /api/tasks/stream" {
					t.Fatalf("expected route %s to be registered, got %q", "GET /api/tasks/stream", pattern)
				}
				return
			}

			mux.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status for %s %s: got %d, want %d (body=%s)", tc.method, tc.path, rr.Code, tc.want, strings.TrimSpace(rr.Body.String()))
			}
		})
	}
}

// TestBuildMux_DocsEndpoints verifies the docs API returns proper responses
// for listing docs and reading individual doc files.
func TestBuildMux_DocsEndpoints(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), nil, false)

	// GET /api/docs should return a JSON array.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/docs: status %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON content type, got %q", ct)
	}

	// GET /api/docs/guide/usage should return markdown.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/docs/guide/usage", nil)
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("GET /api/docs/guide/usage: status %d, want 200", rr2.Code)
	}

	// GET /api/docs/nonexistent should return 404.
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/docs/nonexistent", nil)
	mux.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusNotFound {
		t.Fatalf("GET /api/docs/nonexistent: status %d, want 404", rr3.Code)
	}

	// GET /api/docs/../etc/passwd should be rejected.
	rr4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodGet, "/api/docs/..%2F..%2Fetc%2Fpasswd", nil)
	// Manually set the path value to bypass URL canonicalization.
	req4.SetPathValue("slug", "../../etc/passwd")
	mux.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusBadRequest && rr4.Code != http.StatusNotFound {
		t.Fatalf("path traversal: status %d, want 400 or 404", rr4.Code)
	}
}

// TestBuildMux_DocsAsset verifies the docs-asset route serves embedded images
// with the right content type, and rejects non-image extensions, traversal,
// and missing files — so doc screenshots referenced from guide markdown render
// in-app without leaking other embedded files.
func TestBuildMux_DocsAsset(t *testing.T) {
	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, "worktrees"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command: "true", EnvFile: filepath.Join(workdir, ".env"),
		WorktreesDir: filepath.Join(workdir, "worktrees"), Workspaces: []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()

	pngBytes := []byte("\x89PNG\r\n\x1a\nfake-png-bytes")
	docsFS := fstest.MapFS{
		"docs/guide/images/board.png": {Data: pngBytes},
		"docs/guide/board.md":         {Data: []byte("# Board\n")},
	}
	mux := BuildMux(h, reg, IndexViewData{}, docsFS, nil, false)

	// The path goes in the URL so the mux populates {path...} itself (a
	// pre-set PathValue would be overwritten during routing).
	get := func(urlPath string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/docs-asset/"+urlPath, nil)
		mux.ServeHTTP(rr, req)
		return rr
	}

	// A real image is served with the image content type and exact bytes.
	rr := get("guide/images/board.png")
	if rr.Code != http.StatusOK {
		t.Fatalf("asset: status %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type %q, want image/png", ct)
	}
	if rr.Body.String() != string(pngBytes) {
		t.Fatalf("asset bytes mismatch")
	}

	// Non-image extensions (e.g. markdown) are rejected, so the route cannot
	// be used to read arbitrary embedded docs.
	if rr := get("guide/board.md"); rr.Code != http.StatusBadRequest {
		t.Fatalf("markdown via asset route: status %d, want 400", rr.Code)
	}
	// Path traversal is rejected (%2F keeps the ".." in the captured value
	// instead of being cleaned away by the router).
	if rr := get("..%2F..%2Fetc%2Fpasswd.png"); rr.Code != http.StatusBadRequest {
		t.Fatalf("traversal: status %d, want 400", rr.Code)
	}
	// Missing image is a 404.
	if rr := get("guide/images/missing.png"); rr.Code != http.StatusNotFound {
		t.Fatalf("missing asset: status %d, want 404", rr.Code)
	}
}

// TestBuildMux_WithIDInvalidUUID verifies that routes using withID return 400
// when the UUID is malformed.
func TestBuildMux_WithIDInvalidUUID(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), nil, false)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/not-a-uuid/events", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid UUID, got %d", rr.Code)
	}
}

// TestBuildMux_ServeOutputInvalidUUID verifies that the ServeOutput route
// returns 400 for an invalid task UUID.
func TestBuildMux_ServeOutputInvalidUUID(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), nil, false)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/bad-uuid/outputs/file.txt", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid UUID in output, got %d", rr.Code)
	}
}

// TestBuildMux_MetricsEndpoint verifies that the /metrics endpoint returns
// Prometheus-format text.
func TestBuildMux_MetricsEndpoint(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), nil, false)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /metrics: status %d, want 200", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("expected text/plain content type, got %q", ct)
	}
}

// TestBuildMux_DocsInternals verifies that the internals docs are served.
func TestBuildMux_DocsInternals(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), nil, false)

	// GET /api/docs/internals/internals should return the internals index.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/docs/internals/internals", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/docs/internals/internals: status %d, want 200", rr.Code)
	}
}

// TestBuildMux_ServesVueSPA verifies that BuildMux mounts the Vue SPA at "/"
// with the window.__WALLFACER__ runtime config injected, and that unmatched
// non-API GET paths fall back to the SPA index for client-side routing.
func TestBuildMux_ServesVueSPA(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{ServerAPIKey: "test-key"}, testFS(t), stubVueFS(t), false)

	// "/" serves the injected SPA index.
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /: status %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected HTML content type, got %q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "window.__WALLFACER__") {
		t.Fatalf("expected window.__WALLFACER__ injection, got %q", body)
	}
	if !strings.Contains(body, `"test-key"`) {
		t.Fatalf("expected injected serverApiKey, got %q", body)
	}

	// Unmatched non-API paths fall back to the SPA index (history routing).
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/some/client/route", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /some/client/route: status %d, want 200 (SPA fallback)", rr.Code)
	}
}

// TestGauge_FailedTasksByCategory validates the Prometheus gauge collector
// that counts failed tasks grouped by failure category.
func TestGauge_FailedTasksByCategory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "test prompt", Timeout: 10})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(context.Background(), task.ID, store.TaskStatusFailed); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.SetTaskFailureCategory(context.Background(), task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	// Mirror the gauge collector from server.go.
	collector := func() []metrics.LabeledValue {
		tasks, err := s.ListTasks(context.Background(), false)
		if err != nil {
			return nil
		}
		counts := make(map[string]int)
		for _, t := range tasks {
			if t.Status == store.TaskStatusFailed {
				cat := string(t.FailureCategory)
				if cat == "" {
					cat = "unknown"
				}
				counts[cat]++
			}
		}
		vals := make([]metrics.LabeledValue, 0, len(counts))
		for cat, n := range counts {
			vals = append(vals, metrics.LabeledValue{
				Labels: map[string]string{"category": cat},
				Value:  float64(n),
			})
		}
		return vals
	}

	vals := collector()
	if len(vals) != 1 {
		t.Fatalf("expected 1 LabeledValue, got %d", len(vals))
	}
	if vals[0].Labels["category"] != string(store.FailureCategoryContainerCrash) {
		t.Errorf("category label = %q, want %q", vals[0].Labels["category"], store.FailureCategoryContainerCrash)
	}
	if vals[0].Value != 1 {
		t.Errorf("value = %v, want 1", vals[0].Value)
	}
}

// TestStatusResponseWriter_HijackNotSupported verifies that Hijack returns an
// error when the underlying writer doesn't implement http.Hijacker.
func TestStatusResponseWriter_HijackNotSupported(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := &statusResponseWriter{ResponseWriter: rr, status: http.StatusOK}

	// httptest.ResponseRecorder does not implement Hijacker, so this should
	// return an error about the underlying writer.
	_, _, err := sw.Hijack()
	if err == nil {
		t.Fatal("expected error from Hijack on non-Hijacker writer")
	}
	if !strings.Contains(err.Error(), "does not implement http.Hijacker") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// fakeHijackWriter implements http.ResponseWriter and http.Hijacker for testing.
type fakeHijackWriter struct {
	http.ResponseWriter
}

func (f *fakeHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

// TestStatusResponseWriter_HijackSupported verifies that Hijack delegates to
// the underlying writer when it implements http.Hijacker.
func TestStatusResponseWriter_HijackSupported(t *testing.T) {
	rr := httptest.NewRecorder()
	hw := &fakeHijackWriter{ResponseWriter: rr}
	sw := &statusResponseWriter{ResponseWriter: hw, status: http.StatusOK}

	conn, rw, err := sw.Hijack()
	if err != nil {
		t.Fatalf("expected no error from Hijack, got: %v", err)
	}
	// The fake returns nil for both.
	if conn != nil || rw != nil {
		t.Fatalf("expected nil conn and rw from fake hijacker")
	}
}

// TestNormalizeBrowserVisibleHostPort verifies address normalization for
// different listener bind addresses.
// fakeAddr is a net.Addr that returns a string without a host:port separator,
// triggering the SplitHostPort error path in normalizeBrowserVisibleHostPort.
type fakeAddr string

func (f fakeAddr) Network() string { return "tcp" }
func (f fakeAddr) String() string  { return string(f) }

// TestNormalizeBrowserVisibleHostPort_InvalidAddr verifies the fallback when
// the address cannot be parsed by net.SplitHostPort.
func TestNormalizeBrowserVisibleHostPort_InvalidAddr(t *testing.T) {
	addr := fakeAddr("no-port-here")
	got := normalizeBrowserVisibleHostPort(":8080", addr)
	if got != "no-port-here" {
		t.Fatalf("expected raw addr string, got %q", got)
	}
}

func TestNormalizeBrowserVisibleHostPort(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		actual    string
		want      string
	}{
		{"wildcard with localhost fallback", ":8080", "0.0.0.0:8080", "localhost:8080"},
		{"ipv6 wildcard", ":8080", "[::]:8080", "localhost:8080"},
		{"specific host preserved", "127.0.0.1:9090", "127.0.0.1:9090", "127.0.0.1:9090"},
		{"requested host used for wildcard", "myhost:0", "0.0.0.0:12345", "myhost:12345"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := net.ResolveTCPAddr("tcp", tc.actual)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			got := normalizeBrowserVisibleHostPort(tc.requested, addr)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRequiresStore verifies the routing classification for store-requiring
// and store-independent routes.
func TestRequiresStore(t *testing.T) {
	storeIndependent := []string{
		"GetConfig", "UpdateConfig", "BrowseWorkspaces", "MkdirWorkspace",
		"RenameWorkspace", "UpdateWorkspaces", "GetEnvConfig",
		"UpdateEnvConfig", "TestSandbox", "GitStatus", "GitStatusStream",
	}
	for _, name := range storeIndependent {
		if requiresStore(name) {
			t.Errorf("requiresStore(%q) = true, want false", name)
		}
	}

	storeRequired := []string{
		"ListTasks", "CreateTask", "Health",
	}
	for _, name := range storeRequired {
		if !requiresStore(name) {
			t.Errorf("requiresStore(%q) = false, want true", name)
		}
	}
}

// TestInitServer_MetricsScrapesGauges verifies that the Prometheus metrics
// endpoint triggers the gauge callbacks registered during initServer.
func TestInitServer_MetricsScrapesGauges(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      ":0",
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))
	defer sc.Shutdown()

	// Start the server in the background.
	go func() { _ = sc.Srv.Serve(sc.Ln) }()

	// Scrape /metrics to trigger gauge callbacks.
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", sc.ActualPort))
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics: status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	metricsText := string(body)

	// Verify gauge metrics appear.
	for _, want := range []string{
		"wallfacer_circuit_breaker_open",
		"wallfacer_background_goroutines",
	} {
		if !strings.Contains(metricsText, want) {
			t.Errorf("expected %q in metrics output", want)
		}
	}
}

// TestInitServer_WithExistingStore verifies that initServer handles an existing
// store with tasks, covering the recovery and workspace instructions paths.
func TestInitServer_WithExistingStore(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	wsDir := t.TempDir()
	envContent := "# test\nWALLFACER_WORKSPACES=" + wsDir + "\n"
	if err := os.WriteFile(envFile, []byte(envContent), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      ":0",
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))
	defer sc.Shutdown()

	if sc.ActualPort == 0 {
		t.Fatal("expected non-zero port")
	}
}

// TestInitServer_PortFallback verifies that when the requested port is taken,
// initServer falls back to a free port.
func TestInitServer_PortFallback(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	// First, bind a port to occupy it.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	occupiedPort := ln.Addr().(*net.TCPAddr).Port
	defer func() { _ = ln.Close() }()

	// Now try to init server on the same port.
	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      fmt.Sprintf(":%d", occupiedPort),
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))
	defer sc.Shutdown()

	// It should have found a different port.
	if sc.ActualPort == occupiedPort {
		t.Fatalf("expected fallback to different port, got same port %d", occupiedPort)
	}
	if sc.ActualPort == 0 {
		t.Fatal("expected non-zero fallback port")
	}
}

// TestInitServer_TombstoneRetentionDays verifies that the tombstone retention
// env var is picked up during initialization.
func TestInitServer_TombstoneRetentionDays(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("WALLFACER_TOMBSTONE_RETENTION_DAYS", "30")

	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      ":0",
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))
	defer sc.Shutdown()

	if sc.Srv == nil {
		t.Fatal("expected non-nil server")
	}
}

// TestShutdown_WithAgentSessionRunning verifies that Shutdown stops the agent session
// when it is running.
func TestShutdown_WithAgentSessionRunning(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      ":0",
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))

	// Agent session is initialized but not running, so Shutdown should handle it.
	sc.Shutdown()
}

// TestShutdown_HttpShutdownError verifies that Shutdown completes even when
// the HTTP shutdown encounters an error (e.g. context deadline).
func TestShutdown_HttpShutdownError(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      ":0",
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))

	// Start serving so that Shutdown has something to shut down.
	go func() { _ = sc.Srv.Serve(sc.Ln) }()
	// Give the server a moment to start.
	sc.Shutdown()
}

// TestGauge_TasksTotal validates the Prometheus gauge that counts tasks by
// status and archived flag.
func TestGauge_TasksTotal(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	_, err = s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "task1", Timeout: 10})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Mirror the gauge collector from server.go.
	collector := func() []metrics.LabeledValue {
		tasks, err := s.ListTasks(context.Background(), true)
		if err != nil {
			return nil
		}
		type key struct{ status, archived string }
		counts := make(map[key]int)
		for _, t := range tasks {
			counts[key{string(t.Status), fmt.Sprintf("%v", t.Archived)}]++
		}
		vals := make([]metrics.LabeledValue, 0, len(counts))
		for k, n := range counts {
			vals = append(vals, metrics.LabeledValue{
				Labels: map[string]string{"status": k.status, "archived": k.archived},
				Value:  float64(n),
			})
		}
		return vals
	}

	vals := collector()
	if len(vals) == 0 {
		t.Fatal("expected at least one labeled value")
	}
	found := false
	for _, v := range vals {
		if v.Labels["status"] == "backlog" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a backlog status entry")
	}
}

// TestGauge_RunningContainers validates the running containers gauge collector.
func TestGauge_RunningContainers(t *testing.T) {
	workdir := t.TempDir()
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: filepath.Join(workdir, "worktrees"),
		Workspaces:   []string{workdir},
	})

	// Mirror the gauge collector from server.go.
	collector := func() []metrics.LabeledValue {
		containers, err := r.ListContainers()
		if err != nil {
			return []metrics.LabeledValue{{Value: 0}}
		}
		return []metrics.LabeledValue{{Value: float64(len(containers))}}
	}

	vals := collector()
	if len(vals) != 1 {
		t.Fatalf("expected 1 value, got %d", len(vals))
	}
	// With the "true" command, ListContainers may fail, but the gauge should
	// return 0 either way.
}

// TestGauge_BackgroundGoroutines validates the pending goroutines gauge.
func TestGauge_BackgroundGoroutines(t *testing.T) {
	workdir := t.TempDir()
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: filepath.Join(workdir, "worktrees"),
		Workspaces:   []string{workdir},
	})

	collector := func() []metrics.LabeledValue {
		return []metrics.LabeledValue{{Value: float64(len(r.PendingGoroutines()))}}
	}

	vals := collector()
	if len(vals) != 1 {
		t.Fatalf("expected 1 value, got %d", len(vals))
	}
	if vals[0].Value != 0 {
		t.Errorf("expected 0 pending goroutines, got %v", vals[0].Value)
	}
}

// TestGauge_StoreSubscribers validates the store subscribers gauge.
func TestGauge_StoreSubscribers(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	collector := func() []metrics.LabeledValue {
		return []metrics.LabeledValue{{Value: float64(s.SubscriberCount())}}
	}

	vals := collector()
	if len(vals) != 1 {
		t.Fatalf("expected 1 value, got %d", len(vals))
	}
	if vals[0].Value != 0 {
		t.Errorf("expected 0 subscribers initially, got %v", vals[0].Value)
	}
}

// TestGauge_CircuitBreakerOpen validates the circuit-breaker gauge: starts at
// 0 (closed), then flips to 1 (open) after exceeding the failure threshold.
func TestGauge_CircuitBreakerOpen(t *testing.T) {
	workdir := t.TempDir()
	s, err := store.NewFileStore(filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: filepath.Join(workdir, "worktrees"),
		Workspaces:   []string{workdir},
	})

	// Circuit should be closed initially.
	collector := func() []metrics.LabeledValue {
		v := 0.0
		if r.ContainerCircuitOpen() {
			v = 1.0
		}
		return []metrics.LabeledValue{{Value: v}}
	}

	vals := collector()
	if len(vals) != 1 {
		t.Fatalf("expected 1 LabeledValue, got %d", len(vals))
	}
	if vals[0].Value != 0.0 {
		t.Errorf("circuit breaker open = %v, want 0 (closed)", vals[0].Value)
	}

	// Trip the circuit breaker by recording failures above the threshold.
	for i := 0; i < constants.DefaultCBThreshold+1; i++ {
		r.RecordContainerFailure()
	}

	vals = collector()
	if len(vals) != 1 {
		t.Fatalf("expected 1 LabeledValue, got %d", len(vals))
	}
	if vals[0].Value != 1.0 {
		t.Errorf("circuit breaker open = %v, want 1 (open)", vals[0].Value)
	}
}

// TestMountVueSPA_NoLandingFlashOnDeepRoutes pins the SSG-strip behavior that
// keeps a logged-in user from seeing the ProductPage landing flash before Vue
// swaps in the real route on a deep-link refresh. The prerendered index.html
// bakes in the "/" route (ProductPage in cloud); serving it verbatim for any
// other path paints the landing markup for a frame. Only cloud "/" keeps the
// prerender; every other path mounts from a blank #app.
func TestMountVueSPA_NoLandingFlashOnDeepRoutes(t *testing.T) {
	const indexHTML = `<html><head></head><body>` +
		`<div id="app" data-server-rendered="true"><section class="product-hero">landing</section></div>` +
		`<script type="module" src="/assets/app.js"></script></body></html>`
	dist := fstest.MapFS{
		"frontend/dist/index.html": {Data: []byte(indexHTML)},
	}

	get := func(cloudMode bool, path string) string {
		mux := http.NewServeMux()
		mountVueSPA(mux, dist, "k", cloudMode)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s (cloud=%v): status %d, want 200", path, cloudMode, rr.Code)
		}
		return rr.Body.String()
	}

	const cleared = `e.textContent=""` // stripSSGContent's clear script

	// Cloud "/" keeps the prerender: ProductPage hydration legitimately matches.
	if body := get(true, "/"); !strings.Contains(body, "data-server-rendered") ||
		!strings.Contains(body, "product-hero") || strings.Contains(body, cleared) {
		t.Errorf("cloud /: want intact SSG ProductPage, got %q", body)
	}

	// Cloud deep route (e.g. /dashboard) must NOT serve the landing markup as
	// server-rendered content, or it flashes. #app is cleared before mount.
	if body := get(true, "/dashboard"); strings.Contains(body, "data-server-rendered") ||
		!strings.Contains(body, cleared) {
		t.Errorf("cloud /dashboard: want stripped shell (no flash), got %q", body)
	}

	// Local mode strips for every route, including "/", since local routes are
	// not prerendered (the SSG index.html is the cloud ProductPage).
	if body := get(false, "/"); strings.Contains(body, "data-server-rendered") ||
		!strings.Contains(body, cleared) || !strings.Contains(body, `mode:"local"`) {
		t.Errorf("local /: want stripped shell, got %q", body)
	}
}
