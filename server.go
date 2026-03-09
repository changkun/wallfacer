package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"html/template"
	fsLib "io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"changkun.de/wallfacer/internal/apicontract"
	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/handler"
	"changkun.de/wallfacer/internal/instructions"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/metrics"
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
		fmt.Fprintf(os.Stderr, "Start the task board server and open the web UI.\n\n")
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
	codexAuthPath := ""
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		codexAuthPath = filepath.Join(home, ".codex")
	}

	// Read initial ContainerNetwork from env file (if present) so the runner
	// starts with the configured network policy without waiting for the first
	// container launch to re-read the file.
	containerNetwork := ""
	if cfg, err := envconfig.Parse(*envFile); err == nil {
		containerNetwork = cfg.ContainerNetwork
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:          *containerCmd,
		SandboxImage:     resolvedImage,
		EnvFile:          *envFile,
		Workspaces:       strings.Join(workspaces, " "),
		WorktreesDir:     worktreesDir,
		InstructionsPath: instructionsPath,
		CodexAuthPath:    codexAuthPath,
		ContainerNetwork: containerNetwork,
	})

	r.PruneOrphanedWorktrees(s)

	// Set up signal-based context so background workers stop on SIGTERM/Interrupt.
	// Created before recovery so orphan monitors can be cancelled on shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	runner.RecoverOrphanedTasks(ctx, s, r)

	logger.Main.Info("workspaces", "paths", strings.Join(workspaces, ", "))

	h := handler.NewHandler(s, r, configDir, workspaces)
	r.SetStopReasonHandler(func(taskID uuid.UUID, stopReason string) {
		if stopReason == "max_tokens" {
			h.SetAutopilot(false)
		}
	})

	// Start the auto-promoter: watches for state changes and promotes
	// backlog tasks to in_progress when capacity is available.
	h.StartAutoPromoter(ctx)

	// Start the ideation watcher: when ideation is enabled and an idea-agent
	// task completes, automatically enqueues the next one.
	h.StartIdeationWatcher(ctx)

	// Start the waiting-sync watcher: periodically checks waiting tasks and
	// automatically syncs any whose worktrees have fallen behind the default branch.
	h.StartWaitingSyncWatcher(ctx)

	// Start the auto-tester: triggers the test agent for waiting tasks that are
	// untested and not behind the default branch tip, when auto-test is enabled.
	h.StartAutoTester(ctx)

	// Start the auto-submitter: moves waiting tasks to done when they are
	// verified (pass), not behind the default branch, and conflict-free.
	h.StartAutoSubmitter(ctx)

	// Build the Prometheus metrics registry and register scrape-time gauge
	// collectors. HTTP counter and histogram are created inside loggingMiddleware
	// so they are available via the same registry for /metrics.
	reg := metrics.NewRegistry()
	reg.Gauge(
		"wallfacer_tasks_total",
		"Number of tasks grouped by status and archived flag.",
		func() []metrics.LabeledValue {
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
		},
	)
	reg.Gauge(
		"wallfacer_running_containers",
		"Number of wallfacer sandbox containers currently tracked by the container runtime.",
		func() []metrics.LabeledValue {
			containers, err := r.ListContainers()
			if err != nil {
				return []metrics.LabeledValue{{Value: 0}}
			}
			return []metrics.LabeledValue{{Value: float64(len(containers))}}
		},
	)
	reg.Gauge(
		"wallfacer_background_goroutines",
		"Number of outstanding background goroutines tracked by the runner.",
		func() []metrics.LabeledValue {
			return []metrics.LabeledValue{{Value: float64(len(r.PendingGoroutines()))}}
		},
	)
	reg.Gauge(
		"wallfacer_store_subscribers",
		"Number of active SSE subscribers listening for task state changes.",
		func() []metrics.LabeledValue {
			return []metrics.LabeledValue{{Value: float64(s.SubscriberCount())}}
		},
	)

	mux := buildMux(h, r, reg)

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
		Handler:     loggingMiddleware(mux, reg),
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
//
// All API routes are registered from apicontract.Routes (the single source of
// truth). The handlers map below pairs each route Name with its http.HandlerFunc,
// applying per-route middleware (e.g. UUID parsing via withID) at map
// construction time. A startup panic is triggered if a route in the contract
// has no corresponding handler entry, preventing silent drift.
func buildMux(h *handler.Handler, _ *runner.Runner, reg *metrics.Registry) *http.ServeMux {
	mux := http.NewServeMux()

	// Static files (task board UI).
	uiFS, _ := fsLib.Sub(uiFiles, "ui")
	indexTemplates, err := template.New("index.html").ParseFS(uiFS, "index.html", "partials/*.html")
	if err != nil {
		logger.Fatal(logger.Main, "parse ui templates", "error", err)
	}
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := indexTemplates.ExecuteTemplate(w, "index.html", nil); err != nil {
			logger.Main.Error("render index", "error", err)
			http.Error(w, "failed to render index", http.StatusInternalServerError)
		}
	}
	mux.HandleFunc("GET /", serveIndex)

	// Static asset directories served from the embedded filesystem.
	mux.Handle("GET /css/", http.FileServer(http.FS(uiFS)))
	mux.Handle("GET /js/", http.FileServer(http.FS(uiFS)))

	// withID wraps a handler that needs a parsed task UUID from the {id} path
	// segment, converting the UUID-accepting signature to http.HandlerFunc.
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

	// handlers maps each Route.Name from apicontract.Routes to its handler.
	// All per-route middleware (UUID parsing, extra path values) is applied here
	// so the registration loop below stays trivial.
	handlers := map[string]http.HandlerFunc{
		// Admin operations.
		"RebuildIndex": h.RebuildIndex,

		// Debug & monitoring.
		"Health":          h.Health,
		"GetSpanStats":    h.GetSpanStats,
		"GetRuntimeStatus": h.GetRuntimeStatus,

		// Container monitoring.
		"GetContainers": h.GetContainers,

		// File listing.
		"GetFiles": h.GetFiles,

		// Server configuration.
		"GetConfig":    h.GetConfig,
		"UpdateConfig": h.UpdateConfig,

		// Ideation agent.
		"GetIdeationStatus": h.GetIdeationStatus,
		"TriggerIdeation":   h.TriggerIdeation,
		"CancelIdeation":    h.CancelIdeation,

		// Environment configuration.
		"GetEnvConfig":    h.GetEnvConfig,
		"UpdateEnvConfig": h.UpdateEnvConfig,
		"TestSandbox":     h.TestSandbox,

		// Workspace instructions.
		"GetInstructions":    h.GetInstructions,
		"UpdateInstructions": h.UpdateInstructions,
		"ReinitInstructions": h.ReinitInstructions,

		// Prompt templates.
		"ListTemplates":   h.ListTemplates,
		"CreateTemplate":  h.CreateTemplate,
		"DeleteTemplate":  h.DeleteTemplate,

		// Git workspace operations.
		"GitStatus":        h.GitStatus,
		"GitStatusStream":  h.GitStatusStream,
		"GitPush":          h.GitPush,
		"GitSyncWorkspace": h.GitSyncWorkspace,
		"GitRebaseOnMain":  h.GitRebaseOnMain,
		"GitBranches":      h.GitBranches,
		"GitCheckout":      h.GitCheckout,
		"GitCreateBranch":  h.GitCreateBranch,
		"OpenFolder":       h.OpenFolder,

		// Usage & statistics.
		"GetUsageStats": h.GetUsageStats,
		"GetStats":      h.GetStats,

		// Task collection (no {id}).
		"ListTasks":                h.ListTasks,
		"StreamTasks":              h.StreamTasks,
		"CreateTask":               h.CreateTask,
		"BatchCreateTasks":         h.BatchCreateTasks,
		"GenerateMissingTitles":    h.GenerateMissingTitles,
		"GenerateMissingOversight": h.GenerateMissingOversight,
		"SearchTasks":              h.SearchTasks,
		"ArchiveAllDone":           h.ArchiveAllDone,
		"ListSummaries":            h.ListSummaries,

		// Task instance operations (UUID extracted via withID).
		"UpdateTask":    withID(h.UpdateTask),
		"DeleteTask":    withID(h.DeleteTask),
		"GetEvents":     withID(h.GetEvents),
		"SubmitFeedback": withID(h.SubmitFeedback),
		"CompleteTask":  withID(h.CompleteTask),
		"CancelTask":    withID(h.CancelTask),
		"ResumeTask":    withID(h.ResumeTask),
		"ArchiveTask":   withID(h.ArchiveTask),
		"UnarchiveTask": withID(h.UnarchiveTask),
		"SyncTask":      withID(h.SyncTask),
		"TestTask":      withID(h.TestTask),
		"TaskDiff":      withID(h.TaskDiff),
		"StreamLogs":    withID(h.StreamLogs),

		// GetTurnUsage reads {id} internally (not via withID).
		"GetTurnUsage": h.GetTurnUsage,

		// ServeOutput needs both {id} (UUID) and {filename} path values.
		"ServeOutput": func(w http.ResponseWriter, r *http.Request) {
			id, err := uuid.Parse(r.PathValue("id"))
			if err != nil {
				http.Error(w, "invalid task id", http.StatusBadRequest)
				return
			}
			h.ServeOutput(w, r, id, r.PathValue("filename"))
		},

		// Task span / oversight analytics.
		"GetTaskSpans":     withID(h.GetTaskSpans),
		"GetOversight":     withID(h.GetOversight),
		"GetTestOversight": withID(h.GetTestOversight),

		// Refinement agent.
		"StartRefinement":  withID(h.StartRefinement),
		"CancelRefinement": withID(h.CancelRefinement),
		"StreamRefineLogs": withID(h.StreamRefineLogs),
		"RefineApply":      withID(h.RefineApply),
		"RefineDismiss":    withID(h.RefineDismiss),
	}

	// Register all routes from the contract. A missing handler entry panics at
	// startup, making it impossible to deploy with a route in the contract but
	// no handler wired up.
	for _, route := range apicontract.Routes {
		fn, ok := handlers[route.Name]
		if !ok {
			panic(fmt.Sprintf("buildMux: no handler registered for contract route %q (%s %s)",
				route.Name, route.Method, route.Pattern))
		}
		mux.HandleFunc(route.FullPattern(), fn)
	}

	// Prometheus metrics endpoint (not an API route; excluded from the contract).
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		reg.WritePrometheus(w)
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

// loggingMiddleware logs each HTTP request and records Prometheus metrics.
// It uses r.Pattern (set by ServeMux in Go 1.22+) as the route label so that
// parameterised routes like "GET /api/tasks/{id}" are collapsed to a single
// time series. When r.Pattern is empty it falls back to r.URL.Path.
func loggingMiddleware(next http.Handler, reg *metrics.Registry) http.Handler {
	httpReqs := reg.Counter(
		"wallfacer_http_requests_total",
		"Total number of HTTP requests partitioned by method, route, and status code.",
	)
	httpDur := reg.Histogram(
		"wallfacer_http_request_duration_seconds",
		"HTTP request latency in seconds partitioned by method and route.",
		metrics.DefaultDurationBuckets,
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		dur := time.Since(start)

		// Use the matched pattern when available so parameterised routes are
		// collapsed (e.g. "GET /api/tasks/{id}" rather than a unique path per task).
		route := r.Pattern
		if route == "" {
			route = r.URL.Path
		}

		httpReqs.Inc(map[string]string{
			"method": r.Method,
			"route":  route,
			"status": strconv.Itoa(sw.status),
		})
		httpDur.Observe(map[string]string{
			"method": r.Method,
			"route":  route,
		}, dur.Seconds())

		if strings.HasPrefix(r.URL.Path, "/api/") {
			logger.Handler.Info(r.Method+" "+r.URL.Path, "status", sw.status, "dur", dur.Round(time.Millisecond))
		} else {
			logger.Handler.Debug(r.Method+" "+r.URL.Path, "status", sw.status, "dur", dur.Round(time.Millisecond))
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
