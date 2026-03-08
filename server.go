package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	fsLib "io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"changkun.de/wallfacer/internal/handler"
	"changkun.de/wallfacer/internal/instructions"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

//go:embed ui
var uiFiles embed.FS

func runServer(configDir string, args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)

	logFormat := fs.String("log-format", envOrDefault("LOG_FORMAT", "text"), `log output format: "text" or "json"`)
	addr := fs.String("addr", envOrDefault("ADDR", ":8080"), "listen address")
	dataDir := fs.String("data", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")), "data directory")
	containerCmd := fs.String("container", envOrDefault("CONTAINER_CMD", detectContainerRuntime()), "container runtime command (podman or docker)")
	sandboxImage := fs.String("image", envOrDefault("SANDBOX_IMAGE", defaultSandboxImage), "sandbox container image")
	envFile := fs.String("env-file", envOrDefault("ENV_FILE", filepath.Join(configDir, ".env")), "env file for container (Claude token)")
	noBrowser := fs.Bool("no-browser", false, "do not open browser on start")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: wallfacer run [flags] [workspace ...]\n\n")
		fmt.Fprintf(os.Stderr, "Start the Kanban server and open the web UI.\n\n")
		fmt.Fprintf(os.Stderr, "Positional arguments:\n")
		fmt.Fprintf(os.Stderr, "  workspace    directories to mount in the sandbox (default: current directory)\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	// Re-initialize loggers with the format chosen by the user.
	logger.Init(*logFormat)

	// Auto-initialize config directory and .env template.
	initConfigDir(configDir, *envFile)

	// Positional args are workspace directories.
	workspaces := fs.Args()
	if len(workspaces) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			logger.Fatal(logger.Main, "getwd", "error", err)
		}
		workspaces = []string{cwd}
	}

	// Resolve to absolute paths and validate.
	for i, ws := range workspaces {
		abs, err := filepath.Abs(ws)
		if err != nil {
			logger.Fatal(logger.Main, "resolve workspace", "workspace", ws, "error", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			logger.Fatal(logger.Main, "workspace", "path", abs, "error", err)
		}
		if !info.IsDir() {
			logger.Fatal(logger.Main, "workspace is not a directory", "path", abs)
		}
		workspaces[i] = abs
	}

	// Scope the data directory to the specific workspace combination.
	scopedDataDir := filepath.Join(*dataDir, instructions.Key(workspaces))

	s, err := store.NewStore(scopedDataDir)
	if err != nil {
		logger.Fatal(logger.Main, "store", "error", err)
	}
	defer s.Close()
	logger.Main.Info("store loaded", "path", scopedDataDir)

	worktreesDir := filepath.Join(configDir, "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		logger.Fatal(logger.Main, "create worktrees dir", "error", err)
	}

	instructionsPath, err := instructions.Ensure(configDir, workspaces)
	if err != nil {
		logger.Main.Warn("init workspace instructions", "error", err)
	} else {
		logger.Main.Info("workspace instructions", "path", instructionsPath)
	}

	resolvedImage := ensureImage(*containerCmd, *sandboxImage)

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:          *containerCmd,
		SandboxImage:     resolvedImage,
		EnvFile:          *envFile,
		Workspaces:       strings.Join(workspaces, " "),
		WorktreesDir:     worktreesDir,
		InstructionsPath: instructionsPath,
	})

	r.PruneOrphanedWorktrees(s)

	// Set up signal-based context so background workers stop on SIGTERM/Interrupt.
	// Created before recovery so orphan monitors can be cancelled on shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	runner.RecoverOrphanedTasks(ctx, s, r)

	logger.Main.Info("workspaces", "paths", strings.Join(workspaces, ", "))

	h := handler.NewHandler(s, r, configDir, workspaces)

	// Start the auto-promoter: watches for state changes and promotes
	// backlog tasks to in_progress when capacity is available.
	h.StartAutoPromoter(ctx)

	// Start the ideation watcher: when ideation is enabled and an idea-agent
	// task completes, automatically enqueues the next one.
	h.StartIdeationWatcher(ctx)

	// Start the waiting-sync watcher: periodically checks waiting tasks and
	// automatically syncs any whose worktrees have fallen behind the default branch.
	h.StartWaitingSyncWatcher(ctx)

	mux := buildMux(h, r)

	host, _, _ := net.SplitHostPort(*addr)
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		logger.Main.Warn("requested address unavailable, finding free port", "addr", *addr, "error", err)
		ln, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			logger.Fatal(logger.Main, "listen", "error", err)
		}
	}

	actualPort := ln.Addr().(*net.TCPAddr).Port
	if !*noBrowser {
		browserHost := host
		if browserHost == "" {
			browserHost = "localhost"
		}
		go openBrowser(fmt.Sprintf("http://%s:%d", browserHost, actualPort))
	}

	srv := &http.Server{
		Handler:     loggingMiddleware(mux),
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	// Serve in a background goroutine so we can react to the shutdown signal.
	srvErr := make(chan error, 1)
	go func() {
		srvErr <- srv.Serve(ln)
	}()

	logger.Main.Info("listening", "addr", ln.Addr().String())

	// Block until a shutdown signal arrives or the server exits on its own.
	select {
	case <-ctx.Done():
		logger.Main.Info("received shutdown signal, shutting down gracefully")
	case err := <-srvErr:
		if err != nil && err != http.ErrServerClosed {
			logger.Fatal(logger.Main, "server", "error", err)
		}
		return
	}

	// Give in-flight HTTP requests up to 5 seconds to complete.
	// SSE handlers exit promptly because their request contexts (derived from
	// the base context set above) are already cancelled at this point.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Main.Error("http server shutdown", "error", err)
	}

	// Wait for background runner goroutines (oversight generation, title
	// generation, etc.) to finish before the process exits.
	r.Shutdown()

	logger.Main.Info("shutdown complete")
}

// buildMux constructs the HTTP request router.
func buildMux(h *handler.Handler, _ *runner.Runner) *http.ServeMux {
	mux := http.NewServeMux()

	// Static files (Kanban UI).
	uiFS, _ := fsLib.Sub(uiFiles, "ui")
	mux.Handle("GET /", http.FileServer(http.FS(uiFS)))

	// Operational health check (goroutine count, task counts, uptime).
	mux.HandleFunc("GET /api/debug/health", h.Health)

	// Container monitoring.
	mux.HandleFunc("GET /api/containers", h.GetContainers)

	// File listing for @ mention autocomplete.
	mux.HandleFunc("GET /api/files", h.GetFiles)

	// Configuration & instructions.
	mux.HandleFunc("GET /api/config", h.GetConfig)
	mux.HandleFunc("PUT /api/config", h.UpdateConfig)

	// Brainstorm / ideation agent.
	mux.HandleFunc("GET /api/ideate", h.GetIdeationStatus)
	mux.HandleFunc("POST /api/ideate", h.TriggerIdeation)
	mux.HandleFunc("DELETE /api/ideate", h.CancelIdeation)
	mux.HandleFunc("GET /api/env", h.GetEnvConfig)
	mux.HandleFunc("PUT /api/env", h.UpdateEnvConfig)
	mux.HandleFunc("GET /api/instructions", h.GetInstructions)
	mux.HandleFunc("PUT /api/instructions", h.UpdateInstructions)
	mux.HandleFunc("POST /api/instructions/reinit", h.ReinitInstructions)

	// Git workspace operations.
	mux.HandleFunc("GET /api/git/status", h.GitStatus)
	mux.HandleFunc("GET /api/git/stream", h.GitStatusStream)
	mux.HandleFunc("POST /api/git/push", h.GitPush)
	mux.HandleFunc("POST /api/git/sync", h.GitSyncWorkspace)
	mux.HandleFunc("POST /api/git/rebase-on-main", h.GitRebaseOnMain)
	mux.HandleFunc("GET /api/git/branches", h.GitBranches)
	mux.HandleFunc("POST /api/git/checkout", h.GitCheckout)
	mux.HandleFunc("POST /api/git/create-branch", h.GitCreateBranch)

	// Task collection.
	mux.HandleFunc("GET /api/tasks", h.ListTasks)
	mux.HandleFunc("GET /api/tasks/stream", h.StreamTasks)
	mux.HandleFunc("POST /api/tasks", h.CreateTask)
	mux.HandleFunc("POST /api/tasks/generate-titles", h.GenerateMissingTitles)
	mux.HandleFunc("POST /api/tasks/generate-oversight", h.GenerateMissingOversight)

	// Task instance routes (require UUID parsing).
	withID := func(fn func(http.ResponseWriter, *http.Request, uuid.UUID)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id, err := uuid.Parse(r.PathValue("id"))
			if err != nil {
				http.Error(w, "invalid task id", http.StatusBadRequest)
				return
			}
			fn(w, r, id)
		}
	}

	mux.HandleFunc("PATCH /api/tasks/{id}", withID(h.UpdateTask))
	mux.HandleFunc("DELETE /api/tasks/{id}", withID(h.DeleteTask))
	mux.HandleFunc("GET /api/tasks/{id}/events", withID(h.GetEvents))
	mux.HandleFunc("POST /api/tasks/{id}/feedback", withID(h.SubmitFeedback))
	mux.HandleFunc("POST /api/tasks/{id}/done", withID(h.CompleteTask))
	mux.HandleFunc("POST /api/tasks/{id}/cancel", withID(h.CancelTask))
	mux.HandleFunc("POST /api/tasks/{id}/resume", withID(h.ResumeTask))
	mux.HandleFunc("POST /api/tasks/archive-done", h.ArchiveAllDone)
	mux.HandleFunc("POST /api/tasks/{id}/archive", withID(h.ArchiveTask))
	mux.HandleFunc("POST /api/tasks/{id}/unarchive", withID(h.UnarchiveTask))
	mux.HandleFunc("POST /api/tasks/{id}/sync", withID(h.SyncTask))
	mux.HandleFunc("POST /api/tasks/{id}/test", withID(h.TestTask))
	mux.HandleFunc("POST /api/tasks/{id}/refine", withID(h.StartRefinement))
	mux.HandleFunc("DELETE /api/tasks/{id}/refine", withID(h.CancelRefinement))
	mux.HandleFunc("GET /api/tasks/{id}/refine/logs", withID(h.StreamRefineLogs))
	mux.HandleFunc("POST /api/tasks/{id}/refine/apply", withID(h.RefineApply))
	mux.HandleFunc("POST /api/tasks/{id}/refine/dismiss", withID(h.RefineDismiss))
	mux.HandleFunc("GET /api/tasks/{id}/oversight", withID(h.GetOversight))
	mux.HandleFunc("GET /api/tasks/{id}/oversight/test", withID(h.GetTestOversight))
	mux.HandleFunc("GET /api/tasks/{id}/spans", withID(h.GetTaskSpans))
	mux.HandleFunc("GET /api/tasks/{id}/diff", withID(h.TaskDiff))
	mux.HandleFunc("GET /api/tasks/{id}/logs", withID(h.StreamLogs))
	mux.HandleFunc("GET /api/tasks/{id}/outputs/{filename}", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		h.ServeOutput(w, r, id, r.PathValue("filename"))
	})

	return mux
}

// statusResponseWriter wraps http.ResponseWriter to capture the HTTP status code.
type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// loggingMiddleware logs each HTTP request with method, path, status, and duration.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		dur := time.Since(start).Round(time.Millisecond)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			logger.Handler.Info(r.Method+" "+r.URL.Path, "status", sw.status, "dur", dur)
		} else {
			logger.Handler.Debug(r.Method+" "+r.URL.Path, "status", sw.status, "dur", dur)
		}
	})
}

// ensureImage checks whether the sandbox image is present locally and pulls it
// from the registry if it is not.  When the pull fails and a local fallback
// image (wallfacer:latest) is available, that image is used instead.
// Returns the image reference that should actually be used.
func ensureImage(containerCmd, image string) string {
	out, err := exec.Command(containerCmd, "images", "-q", image).Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return image // already present
	}
	logger.Main.Info("sandbox image not found locally, pulling from registry", "image", image)
	cmd := exec.Command(containerCmd, "pull", image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logger.Main.Warn("failed to pull sandbox image", "image", image, "error", err)
		// Try the local fallback image if it differs from the requested one.
		if image != fallbackSandboxImage {
			fallbackOut, fallbackErr := exec.Command(containerCmd, "images", "-q", fallbackSandboxImage).Output()
			if fallbackErr == nil && strings.TrimSpace(string(fallbackOut)) != "" {
				logger.Main.Info("using local fallback sandbox image", "image", fallbackSandboxImage)
				return fallbackSandboxImage
			}
		}
		logger.Main.Warn("no sandbox image available; tasks may fail")
	}
	return image
}

