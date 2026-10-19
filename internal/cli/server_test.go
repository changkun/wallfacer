package cli

import (
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

	"latere.ai/x/pkg/metrics"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/handler"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/store/storetest"
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
	}, testFS(t))
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

	s, err := storetest.NewFileStore(t, dataDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "task prompt", Timeout: 10})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, stubVueFS(t))

	paths := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/", http.StatusOK},
		{http.MethodGet, "/api/config", http.StatusOK},
		{http.MethodGet, "/api/debug/health", http.StatusOK},
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

// TestBuildMux_WithIDInvalidUUID verifies that routes using withID return 400
// when the UUID is malformed.
func TestBuildMux_WithIDInvalidUUID(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, nil)

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
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, nil)

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
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{}, nil)

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

// TestBuildMux_ServesVueSPA verifies that BuildMux mounts the Vue SPA at "/"
// with the window.__WALLFACER__ runtime config injected, and that unmatched
// non-API GET paths fall back to the SPA index for client-side routing.
func TestBuildMux_ServesVueSPA(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	reg := metrics.NewRegistry()
	mux := BuildMux(h, reg, IndexViewData{ServerAPIKey: "test-key"}, stubVueFS(t))

	// "/" serves the injected SPA index. The key is released to a loopback
	// peer (httptest's default RemoteAddr is not loopback).
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	mux.ServeHTTP(rr, req)
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

	// A non-loopback anonymous peer gets the shell without the key: GET / is
	// public, so this is the one place an unauthenticated caller could read it.
	for _, path := range []string{"/", "/some/client/route"} {
		rr = httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s (remote peer): status %d, want 200", path, rr.Code)
		}
		body = rr.Body.String()
		if strings.Contains(body, "test-key") {
			t.Fatalf("GET %s (remote peer): server API key leaked to an anonymous caller: %q", path, body)
		}
		if !strings.Contains(body, `serverApiKey:""`) {
			t.Fatalf("GET %s (remote peer): expected empty serverApiKey, got %q", path, body)
		}
	}

	// A remote peer that already holds the key (?token=, the form the SPA
	// uses for streams) receives it, so a LAN client can bootstrap with one URL.
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/?token=test-key", nil))
	if !strings.Contains(rr.Body.String(), `"test-key"`) {
		t.Fatalf("GET /?token=<key> (remote peer): expected injected key, got %q", rr.Body.String())
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
		"RenameWorkspace", "GetEnvConfig",
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
	}, testFS(t))
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
	}, testFS(t))
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
	}, testFS(t))
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
	}, testFS(t))
	defer sc.Shutdown()

	if sc.Srv == nil {
		t.Fatal("expected non-nil server")
	}
}

// TestGauge_BackgroundGoroutines validates the pending goroutines gauge.
func TestGauge_BackgroundGoroutines(t *testing.T) {
	workdir := t.TempDir()
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
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
	s, err := storetest.NewFileStore(t, dataDir)
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
	s, err := storetest.NewFileStore(t, filepath.Join(workdir, "data"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
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
	for range constants.DefaultCBThreshold + 1 {
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
