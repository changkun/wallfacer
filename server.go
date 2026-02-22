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
	"path/filepath"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/handler"
	"changkun.de/wallfacer/internal/instructions"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

const containerPollInterval = 5 * time.Second

//go:embed ui
var uiFiles embed.FS

func runServer(configDir string, args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)

	logFormat := fs.String("log-format", envOrDefault("LOG_FORMAT", "text"), `log output format: "text" or "json"`)
	addr := fs.String("addr", envOrDefault("ADDR", ":8080"), "listen address")
	dataDir := fs.String("data", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")), "data directory")
	containerCmd := fs.String("container", envOrDefault("CONTAINER_CMD", "/opt/podman/bin/podman"), "container runtime command")
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
	recoverOrphanedTasks(s, r)

	logger.Main.Info("workspaces", "paths", strings.Join(workspaces, ", "))

	h := handler.NewHandler(s, r, configDir, workspaces)

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

	logger.Main.Info("listening", "addr", ln.Addr().String())
	if err := http.Serve(ln, loggingMiddleware(mux)); err != nil {
		logger.Fatal(logger.Main, "server", "error", err)
	}
}

// buildMux constructs the HTTP request router.
func buildMux(h *handler.Handler, _ *runner.Runner) *http.ServeMux {
	mux := http.NewServeMux()

	// Static files (Kanban UI).
	uiFS, _ := fsLib.Sub(uiFiles, "ui")
	mux.Handle("GET /", http.FileServer(http.FS(uiFS)))

	// Container monitoring.
	mux.HandleFunc("GET /api/containers", h.GetContainers)

	// Configuration & instructions.
	mux.HandleFunc("GET /api/config", h.GetConfig)
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

	// Task collection.
	mux.HandleFunc("GET /api/tasks", h.ListTasks)
	mux.HandleFunc("GET /api/tasks/stream", h.StreamTasks)
	mux.HandleFunc("POST /api/tasks", h.CreateTask)
	mux.HandleFunc("POST /api/tasks/generate-titles", h.GenerateMissingTitles)

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
	mux.HandleFunc("POST /api/tasks/{id}/archive", withID(h.ArchiveTask))
	mux.HandleFunc("POST /api/tasks/{id}/unarchive", withID(h.UnarchiveTask))
	mux.HandleFunc("POST /api/tasks/{id}/sync", withID(h.SyncTask))
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

// recoverOrphanedTasks reconciles in_progress/committing tasks on startup by
// checking which containers are still running.
//
//   - committing tasks are always moved to failed; the commit pipeline cannot be
//     safely resumed after a restart.
//   - in_progress tasks whose container is still running are left in_progress; a
//     background goroutine monitors the container and moves the task to waiting
//     once it stops.
//   - in_progress tasks whose container is already gone are moved to waiting so
//     the user can inspect the partial results and decide what to do next.
func recoverOrphanedTasks(s *store.Store, r *runner.Runner) {
	ctx := context.Background()
	tasks, err := s.ListTasks(ctx, true)
	if err != nil {
		logger.Recovery.Error("list tasks", "error", err)
		return
	}

	// Build a set of task IDs whose containers are currently running.
	runningContainers := map[string]bool{}
	if containers, listErr := r.ListContainers(); listErr != nil {
		logger.Recovery.Warn("could not list containers during recovery; treating all in_progress tasks as stopped",
			"error", listErr)
	} else {
		for _, c := range containers {
			if c.State == "running" && c.TaskID != "" {
				runningContainers[c.TaskID] = true
			}
		}
	}

	for _, t := range tasks {
		switch t.Status {
		case "committing":
			// Commit pipeline cannot be resumed — mark failed.
			logger.Recovery.Warn("task was committing at startup, marking as failed",
				"task", t.ID)
			s.UpdateTaskStatus(ctx, t.ID, "failed")
			s.InsertEvent(ctx, t.ID, store.EventTypeError, map[string]string{
				"error": "server restarted during commit",
			})
			s.InsertEvent(ctx, t.ID, store.EventTypeStateChange, map[string]string{
				"from": "committing", "to": "failed",
			})

		case "in_progress":
			if runningContainers[t.ID.String()] {
				// Container is still active — leave the task in_progress and
				// monitor it; move to waiting once the container stops.
				logger.Recovery.Info("container still running after restart, monitoring",
					"task", t.ID)
				s.InsertEvent(ctx, t.ID, store.EventTypeSystem, map[string]string{
					"result": "Server restarted while task was running. Container is still active — monitoring for completion.",
				})
				go monitorContainerUntilStopped(s, r, t.ID)
			} else {
				// Container is gone — move to waiting so the user can review
				// partial results and decide whether to continue or finish.
				logger.Recovery.Warn("task container gone after restart, moving to waiting",
					"task", t.ID)
				s.UpdateTaskStatus(ctx, t.ID, "waiting")
				s.InsertEvent(ctx, t.ID, store.EventTypeSystem, map[string]string{
					"result": "Server restarted while task was running. Container is no longer active — please review the output and decide whether to continue or mark as done.",
				})
				s.InsertEvent(ctx, t.ID, store.EventTypeStateChange, map[string]string{
					"from": "in_progress", "to": "waiting",
				})
			}
		}
	}
}

// monitorContainerUntilStopped polls the container runtime until the container
// for taskID is no longer running, then transitions the task from in_progress
// to waiting so the user can decide what to do next.
func monitorContainerUntilStopped(s *store.Store, r *runner.Runner, taskID uuid.UUID) {
	ctx := context.Background()
	containerName := "wallfacer-" + taskID.String()
	ticker := time.NewTicker(containerPollInterval)
	defer ticker.Stop()

	for range ticker.C {
		containers, err := r.ListContainers()
		if err != nil {
			logger.Recovery.Warn("monitor: list containers error", "task", taskID, "error", err)
			continue
		}
		running := false
		for _, c := range containers {
			if c.Name == containerName && c.State == "running" {
				running = true
				break
			}
		}
		if running {
			continue
		}

		// Container stopped — move the task to waiting if it is still in_progress.
		cur, getErr := s.GetTask(ctx, taskID)
		if getErr != nil || cur == nil {
			return
		}
		if cur.Status != "in_progress" {
			// Task was already transitioned by another path (e.g. cancelled).
			return
		}
		logger.Recovery.Info("monitored container stopped, moving task to waiting", "task", taskID)
		s.UpdateTaskStatus(ctx, taskID, "waiting")
		s.InsertEvent(ctx, taskID, store.EventTypeSystem, map[string]string{
			"result": "Container has stopped. Please review the output and decide whether to continue or mark as done.",
		})
		s.InsertEvent(ctx, taskID, store.EventTypeStateChange, map[string]string{
			"from": "in_progress", "to": "waiting",
		})
		return
	}
}
