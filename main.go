package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	fsLib "io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

//go:embed ui
var uiFiles embed.FS

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: wallfacer <command> [arguments]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  run          start the Kanban server\n")
	fmt.Fprintf(os.Stderr, "  env          show configuration and env file status\n")
	fmt.Fprintf(os.Stderr, "\nRun 'wallfacer <command> -help' for more information on a command.\n")
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fatal(logMain, "home dir", "error", err)
	}
	configDir := filepath.Join(home, ".wallfacer")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "env":
		runEnvCheck(configDir)
	case "run":
		runServer(configDir, os.Args[2:])
	case "-help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "wallfacer: unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runServer(configDir string, args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)

	logFormat := fs.String("log-format", envOrDefault("LOG_FORMAT", "text"), `log output format: "text" (colored, human-friendly) or "json" (structured JSON for log aggregators)`)
	addr := fs.String("addr", envOrDefault("ADDR", ":8080"), "listen address")
	dataDir := fs.String("data", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")), "data directory")
	containerCmd := fs.String("container", envOrDefault("CONTAINER_CMD", "/opt/podman/bin/podman"), "container runtime command")
	sandboxImage := fs.String("image", envOrDefault("SANDBOX_IMAGE", "wallfacer:latest"), "sandbox container image")
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
	initLogger(*logFormat)

	// Auto-initialize config directory and .env template.
	initConfigDir(configDir, *envFile)

	// Positional args are workspace directories.
	workspaces := fs.Args()
	if len(workspaces) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			fatal(logMain, "getwd", "error", err)
		}
		workspaces = []string{cwd}
	}

	// Resolve to absolute paths and validate.
	for i, ws := range workspaces {
		abs, err := filepath.Abs(ws)
		if err != nil {
			fatal(logMain, "resolve workspace", "workspace", ws, "error", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			fatal(logMain, "workspace", "path", abs, "error", err)
		}
		if !info.IsDir() {
			fatal(logMain, "workspace is not a directory", "path", abs)
		}
		workspaces[i] = abs
	}

	store, err := NewStore(*dataDir)
	if err != nil {
		fatal(logMain, "store", "error", err)
	}
	defer store.Close()
	logMain.Info("store loaded", "path", *dataDir)

	// Recover orphaned in_progress tasks from a previous server crash.
	recoverOrphanedTasks(store)

	runner := NewRunner(store, RunnerConfig{
		Command:      *containerCmd,
		SandboxImage: *sandboxImage,
		EnvFile:      *envFile,
		Workspaces:   strings.Join(workspaces, " "),
	})

	logMain.Info("workspaces", "paths", strings.Join(workspaces, ", "))

	handler := NewHandler(store, runner)

	mux := http.NewServeMux()

	// Static files (Kanban UI)
	uiFS, _ := fsLib.Sub(uiFiles, "ui")
	mux.Handle("GET /", http.FileServer(http.FS(uiFS)))

	// API routes
	mux.HandleFunc("GET /api/config", handler.GetConfig)
	mux.HandleFunc("GET /api/git/status", handler.GitStatus)
	mux.HandleFunc("GET /api/git/stream", handler.GitStatusStream)
	mux.HandleFunc("POST /api/git/push", handler.GitPush)
	mux.HandleFunc("GET /api/tasks", handler.ListTasks)
	mux.HandleFunc("GET /api/tasks/stream", handler.StreamTasks)
	mux.HandleFunc("POST /api/tasks", handler.CreateTask)

	mux.HandleFunc("PATCH /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.UpdateTask(w, r, id)
	})

	mux.HandleFunc("DELETE /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.DeleteTask(w, r, id)
	})

	mux.HandleFunc("POST /api/tasks/{id}/feedback", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.SubmitFeedback(w, r, id)
	})

	mux.HandleFunc("GET /api/tasks/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.GetEvents(w, r, id)
	})

	mux.HandleFunc("POST /api/tasks/{id}/done", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.CompleteTask(w, r, id)
	})

	mux.HandleFunc("POST /api/tasks/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.ResumeTask(w, r, id)
	})

	mux.HandleFunc("POST /api/tasks/{id}/archive", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.ArchiveTask(w, r, id)
	})

	mux.HandleFunc("POST /api/tasks/{id}/unarchive", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.UnarchiveTask(w, r, id)
	})

	mux.HandleFunc("GET /api/tasks/{id}/outputs/{filename}", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.ServeOutput(w, r, id, r.PathValue("filename"))
	})

	mux.HandleFunc("GET /api/tasks/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		handler.StreamLogs(w, r, id)
	})

	if !*noBrowser {
		url := "http://localhost" + *addr
		if !strings.HasPrefix(*addr, ":") {
			url = "http://" + *addr
		}
		go openBrowser(url)
	}

	logMain.Info("listening", "addr", *addr)
	if err := http.ListenAndServe(*addr, loggingMiddleware(mux)); err != nil {
		fatal(logMain, "server", "error", err)
	}
}

func runEnvCheck(configDir string) {
	envFile := envOrDefault("ENV_FILE", filepath.Join(configDir, ".env"))

	fmt.Printf("Config directory:  %s\n", configDir)
	fmt.Printf("Data directory:    %s\n", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")))
	fmt.Printf("Env file:          %s\n", envFile)
	fmt.Printf("Container command: %s\n", envOrDefault("CONTAINER_CMD", "/opt/podman/bin/podman"))
	fmt.Printf("Sandbox image:     %s\n", envOrDefault("SANDBOX_IMAGE", "wallfacer:latest"))
	fmt.Println()

	// Check config dir exists.
	if info, err := os.Stat(configDir); err != nil {
		fmt.Printf("[!] Config directory does not exist (run 'wallfacer run' to auto-create)\n")
	} else if !info.IsDir() {
		fmt.Printf("[!] %s is not a directory\n", configDir)
	} else {
		fmt.Printf("[ok] Config directory exists\n")
	}

	// Check env file and token.
	raw, err := os.ReadFile(envFile)
	if err != nil {
		fmt.Printf("[!] Env file not found: %s\n", envFile)
		fmt.Printf("    Run 'wallfacer run' once to auto-create a template, then set your token.\n")
		return
	}
	fmt.Printf("[ok] Env file exists\n")

	tokenSet := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "CLAUDE_CODE_OAUTH_TOKEN" {
			if v == "" || v == "your-oauth-token-here" {
				fmt.Printf("[!] CLAUDE_CODE_OAUTH_TOKEN is not set — edit %s\n", envFile)
			} else {
				fmt.Printf("[ok] CLAUDE_CODE_OAUTH_TOKEN is set (%s...%s)\n", v[:4], v[len(v)-4:])
				tokenSet = true
			}
		}
	}
	if !tokenSet {
		fmt.Printf("[!] CLAUDE_CODE_OAUTH_TOKEN not found in %s\n", envFile)
	}

	// Check container runtime.
	containerCmd := envOrDefault("CONTAINER_CMD", "/opt/podman/bin/podman")
	if _, err := exec.LookPath(containerCmd); err != nil {
		fmt.Printf("[!] Container runtime not found: %s\n", containerCmd)
	} else {
		fmt.Printf("[ok] Container runtime found: %s\n", containerCmd)
	}
}

func initConfigDir(configDir, envFile string) {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		fatal(logMain, "create config dir", "error", err)
	}

	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		content := "CLAUDE_CODE_OAUTH_TOKEN=your-oauth-token-here\n"
		if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
			fatal(logMain, "create env file", "error", err)
		}
		logMain.Info("created env file — edit it and set your CLAUDE_CODE_OAUTH_TOKEN", "path", envFile)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func openBrowser(url string) {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	default:
		return
	}
	exec.Command(cmd, url).Start()
}

// statusResponseWriter wraps http.ResponseWriter to capture the HTTP status code
// written by the handler, so it can be included in access log entries.
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

// loggingMiddleware logs each HTTP request with method, path, status, and wall-clock
// duration. API requests are logged at INFO; static-asset requests at DEBUG so they
// don't drown out application events during normal browsing.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		dur := time.Since(start).Round(time.Millisecond)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			logHandler.Info(r.Method+" "+r.URL.Path, "status", sw.status, "dur", dur)
		} else {
			logHandler.Debug(r.Method+" "+r.URL.Path, "status", sw.status, "dur", dur)
		}
	})
}

func recoverOrphanedTasks(store *Store) {
	ctx := context.Background()
	tasks, err := store.ListTasks(ctx, true)
	if err != nil {
		logRecovery.Error("list tasks", "error", err)
		return
	}
	for _, t := range tasks {
		if t.Status != "in_progress" {
			continue
		}
		logRecovery.Warn("task was in_progress at startup, marking as failed", "task", t.ID)
		store.UpdateTaskStatus(ctx, t.ID, "failed")
		store.InsertEvent(ctx, t.ID, "error", map[string]string{
			"error": "server restarted while task was in progress",
		})
		store.InsertEvent(ctx, t.ID, "state_change", map[string]string{
			"from": "in_progress", "to": "failed",
		})
	}
}
