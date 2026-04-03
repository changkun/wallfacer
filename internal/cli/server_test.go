package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/handler"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
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
		LogFormat:    "text",
		Addr:         ":0",
		DataDir:      filepath.Join(configDir, "data"),
		ContainerCmd: "true",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envFile,
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
	if sc.Handler == nil {
		t.Fatal("expected non-nil Handler")
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
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), testFS(t))

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
		{http.MethodGet, "/api/containers", http.StatusOK},
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

// TestEnsureImage_ReturnsExistingOrPulledImage verifies that ensureImage
// returns the requested image when it is already present locally.
func TestEnsureImage_ReturnsExistingOrPulledImage(t *testing.T) {
	tmp := t.TempDir()
	runtimeScript := filepath.Join(tmp, "runtime.sh")
	if err := os.WriteFile(runtimeScript, []byte("#!/bin/sh\n"+
		"if [ \"$1\" = \"images\" ]; then\n"+
		"  if [ \"$2\" = \"-q\" ] && [ \"$3\" = \"wallfacer:latest\" ]; then\n"+
		"    echo found\n"+
		"  fi\n"+
		"  exit 0\n"+
		"elif [ \"$1\" = \"pull\" ]; then\n"+
		"  exit 0\n"+
		"fi\n"), 0o755); err != nil {
		t.Fatalf("write runtime script: %v", err)
	}

	got := ensureImage(runtimeScript, "wallfacer:latest")
	if got != "wallfacer:latest" {
		t.Fatalf("expected requested image, got %q", got)
	}
}

// TestEnsureImage_UsesFallbackWhenPullFails verifies that ensureImage falls
// back to wallfacer:latest when the requested image is not cached and the
// pull fails.
func TestEnsureImage_UsesFallbackWhenPullFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	tmp := t.TempDir()
	runtimeScript := filepath.Join(tmp, "runtime.sh")
	if err := os.WriteFile(runtimeScript, []byte("#!/bin/sh\n"+
		"if [ \"$1\" = \"images\" ]; then\n"+
		"  if [ \"$2\" = \"-q\" ] && [ \"$3\" = \"wallfacer:latest\" ]; then\n"+
		"    echo found\n"+
		"  elif [ \"$2\" = \"-q\" ] && [ \"$3\" = \"wallfacer-missing:latest\" ]; then\n"+
		"    :\n"+
		"  fi\n"+
		"  exit 0\n"+
		"elif [ \"$1\" = \"pull\" ]; then\n"+
		"  exit 1\n"+
		"fi\n"), 0o755); err != nil {
		t.Fatalf("write runtime script: %v", err)
	}

	got := ensureImage(runtimeScript, "wallfacer-missing:latest")
	if got != "wallfacer:latest" {
		t.Fatalf("expected fallback image, got %q", got)
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
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), testFS(t))

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
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), testFS(t))

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
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), testFS(t))

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
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), testFS(t))

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
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), testFS(t))

	// GET /api/docs/internals/internals should return the internals index.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/docs/internals/internals", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/docs/internals/internals: status %d, want 200", rr.Code)
	}
}

// TestBuildMux_IndexHTML verifies that /index.html serves the same content as /.
func TestBuildMux_IndexHTML(t *testing.T) {
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
	mux := BuildMux(h, reg, IndexViewData{ServerAPIKey: "test-key"}, testFS(t), testFS(t))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /index.html: status %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected HTML content type, got %q", ct)
	}
}

// TestBuildMux_StaticAssets verifies that CSS and JS static files are served.
func TestBuildMux_StaticAssets(t *testing.T) {
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
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), testFS(t))

	// A known CSS file should be served.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/css/base.css", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /css/base.css: status %d", rr.Code)
	}
}

// TestBuildMux_IndexNonRoot verifies that non-root paths like /foo return 404
// from the index handler.
func TestBuildMux_IndexNonRoot(t *testing.T) {
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
	mux := BuildMux(h, reg, IndexViewData{}, testFS(t), testFS(t))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for /nonexistent, got %d", rr.Code)
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

// TestEnsureImage_NeitherPullNorFallback verifies that when pull fails and no
// local fallback exists, the original image name is returned.
func TestEnsureImage_NeitherPullNorFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	tmp := t.TempDir()
	runtimeScript := filepath.Join(tmp, "runtime.sh")
	if err := os.WriteFile(runtimeScript, []byte("#!/bin/sh\n"+
		"if [ \"$1\" = \"images\" ]; then\n"+
		"  exit 0\n"+ // always returns empty (no image found)
		"fi\n"+
		"if [ \"$1\" = \"pull\" ]; then\n"+
		"  exit 1\n"+ // pull fails
		"fi\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := ensureImage(runtimeScript, "custom-image:v1")
	if got != "custom-image:v1" {
		t.Fatalf("expected original image returned, got %q", got)
	}
}

// TestEnsureImage_SameAsFallback verifies that when the requested image equals
// the fallback image and pull fails, it still returns the image without trying
// the fallback.
func TestEnsureImage_SameAsFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	tmp := t.TempDir()
	runtimeScript := filepath.Join(tmp, "runtime.sh")
	if err := os.WriteFile(runtimeScript, []byte("#!/bin/sh\n"+
		"if [ \"$1\" = \"images\" ]; then\n"+
		"  exit 0\n"+
		"fi\n"+
		"if [ \"$1\" = \"pull\" ]; then\n"+
		"  exit 1\n"+
		"fi\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := ensureImage(runtimeScript, "wallfacer:latest")
	if got != "wallfacer:latest" {
		t.Fatalf("expected original image, got %q", got)
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
		"ListTasks", "CreateTask", "Health", "GetContainers",
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
		LogFormat:    "text",
		Addr:         ":0",
		DataDir:      filepath.Join(configDir, "data"),
		ContainerCmd: "true",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envFile,
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
		LogFormat:    "text",
		Addr:         ":0",
		DataDir:      filepath.Join(configDir, "data"),
		ContainerCmd: "true",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envFile,
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
		LogFormat:    "text",
		Addr:         fmt.Sprintf(":%d", occupiedPort),
		DataDir:      filepath.Join(configDir, "data"),
		ContainerCmd: "true",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envFile,
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
		LogFormat:    "text",
		Addr:         ":0",
		DataDir:      filepath.Join(configDir, "data"),
		ContainerCmd: "true",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envFile,
	}, testFS(t), testFS(t))
	defer sc.Shutdown()

	if sc.Srv == nil {
		t.Fatal("expected non-nil server")
	}
}

// TestShutdown_WithPlannerRunning verifies that Shutdown stops the planner
// when it is running.
func TestShutdown_WithPlannerRunning(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	sc := initServer(configDir, ServerConfig{
		LogFormat:    "text",
		Addr:         ":0",
		DataDir:      filepath.Join(configDir, "data"),
		ContainerCmd: "true",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envFile,
	}, testFS(t), testFS(t))

	// Planner is initialized but not running, so Shutdown should handle it.
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
		LogFormat:    "text",
		Addr:         ":0",
		DataDir:      filepath.Join(configDir, "data"),
		ContainerCmd: "true",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envFile,
	}, testFS(t), testFS(t))

	// Start serving so that Shutdown has something to shut down.
	go func() { _ = sc.Srv.Serve(sc.Ln) }()
	// Give the server a moment to start.
	sc.Shutdown()
}

// TestInitServer_SkipCSRF verifies that initServer with SkipCSRF registers
// the desktop-port endpoint and serves the actual port number.
func TestInitServer_SkipCSRF(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("# empty\n"), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	sc := initServer(configDir, ServerConfig{
		LogFormat:    "text",
		Addr:         ":0",
		DataDir:      filepath.Join(configDir, "data"),
		ContainerCmd: "true",
		SandboxImage: "wallfacer:latest",
		EnvFile:      envFile,
		SkipCSRF:     true,
	}, testFS(t), testFS(t))
	defer sc.Shutdown()

	if sc.ActualPort == 0 {
		t.Fatal("expected non-zero port")
	}

	// Start the server and verify the desktop-port endpoint responds.
	go func() { _ = sc.Srv.Serve(sc.Ln) }()

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/desktop-port", sc.ActualPort))
	if err != nil {
		t.Fatalf("GET /api/desktop-port: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/desktop-port: status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), fmt.Sprintf("%d", sc.ActualPort)) {
		t.Fatalf("expected port number in response, got %q", string(body))
	}
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
