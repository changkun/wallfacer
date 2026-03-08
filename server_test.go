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
	apiHandler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
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
	})).ServeHTTP(uiRR, uiReq)
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
	task, err := s.CreateTask(context.Background(), "task prompt", 10, false, "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      filepath.Join(workdir, ".env"),
		WorktreesDir: worktrees,
		Workspaces:   workdir,
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir})
	mux := buildMux(h, r)

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
