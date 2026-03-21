package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/handler"
	"changkun.de/wallfacer/internal/metrics"
	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
)

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

func TestBuildMux_RoutesServeKnownPaths(t *testing.T) {
	workdir := t.TempDir()
	worktrees := filepath.Join(workdir, "worktrees")
	dataDir := filepath.Join(workdir, "data")
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	s, err := store.NewStore(dataDir)
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
	mux := BuildMux(h, reg, IndexViewData{})

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

func TestEnsureImage_UsesFallbackWhenPullFails(t *testing.T) {
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

func TestGauge_FailedTasksByCategory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	s, err := store.NewStore(dataDir)
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

func TestGauge_CircuitBreakerOpen(t *testing.T) {
	workdir := t.TempDir()
	s, err := store.NewStore(filepath.Join(workdir, "data"))
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
	for i := 0; i < runner.DefaultCBThreshold+1; i++ {
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
