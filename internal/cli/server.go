package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/apicontract"
	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/handler"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
	"changkun.de/x/wallfacer/prompts"
	"github.com/google/uuid"
)

// IndexViewData holds the data passed to the index.html template.
type IndexViewData struct {
	ServerAPIKey string
}

// RunServer implements the `wallfacer run` subcommand.
// uiFS and docsFS are the embedded (or on-disk) filesystems containing the
// ui/ and docs/ directory trees respectively.
func RunServer(configDir string, args []string, uiFS, docsFS fs.FS) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)

	logFormat := fs.String("log-format", envOrDefault("LOG_FORMAT", "text"), `log output format: "text" or "json"`)
	addr := fs.String("addr", envOrDefault("ADDR", ":8080"), "listen address")
	dataDir := fs.String("data", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")), "data directory")
	containerCmd := fs.String("container", envOrDefault("CONTAINER_CMD", detectContainerRuntime()), "container runtime command (podman or docker)")
	sandboxImage := fs.String("image", envOrDefault("SANDBOX_IMAGE", defaultSandboxImage()), "sandbox container image")
	envFile := fs.String("env-file", envOrDefault("ENV_FILE", filepath.Join(configDir, ".env")), "env file for container (Claude token)")
	noBrowser := fs.Bool("no-browser", false, "do not open browser on start")
	noWorkspaces := fs.Bool("no-workspaces", false, "start with no active workspaces")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: wallfacer run [flags] [workspace ...]\n\n")
		fmt.Fprintf(os.Stderr, "Start the task board server and open the web UI.\n\n")
		fmt.Fprintf(os.Stderr, "Positional arguments:\n")
		fmt.Fprintf(os.Stderr, "  workspace    directories to mount in the sandbox (default: current directory)\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	// Re-initialize loggers with the format chosen by the user.
	logger.Init(*logFormat)

	// Auto-initialize config directory and .env template.
	initConfigDir(configDir, *envFile)

	workspaces := resolveStartupWorkspaces(*noWorkspaces, fs.Args(), *envFile)
	wsMgr, err := workspace.NewManager(configDir, *dataDir, *envFile, workspaces)
	if err != nil {
		logger.Fatal("workspace manager", "error", err)
	}
	snapshot := wsMgr.Snapshot()
	s := snapshot.Store
	if s != nil {
		logger.Main.Info("store loaded", "path", snapshot.ScopedDataDir)

		// Purge tombstoned tasks older than the retention period.
		tombstoneRetentionDays := 7
		if v, err := strconv.Atoi(os.Getenv("WALLFACER_TOMBSTONE_RETENTION_DAYS")); err == nil && v > 0 {
			tombstoneRetentionDays = v
		}
		s.PurgeExpiredTombstones(tombstoneRetentionDays)
	}

	worktreesDir := filepath.Join(configDir, "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		logger.Fatal("create worktrees dir", "error", err)
	}

	if snapshot.InstructionsPath != "" {
		logger.Main.Info("workspace instructions", "path", snapshot.InstructionsPath)
	}

	resolvedImage := ensureImage(*containerCmd, *sandboxImage)
	codexAuthPath := ""
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		codexAuthPath = filepath.Join(home, ".codex")
	}

	// Read initial container settings from env file (if present) so the runner
	// starts with the configured policies without waiting for the first
	// container launch to re-read the file.
	envCfg := envconfig.Config{}
	if cfg, err := envconfig.Parse(*envFile); err == nil {
		envCfg = cfg
	}

	// Build the Prometheus metrics registry early so it can be threaded into
	// the runner (for worktree health counters) and handler (for autopilot
	// action counters).
	reg := metrics.NewRegistry()

	promptsDir := filepath.Join(configDir, "prompts")
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:          *containerCmd,
		SandboxImage:     resolvedImage,
		EnvFile:          *envFile,
		Workspaces:       workspaces,
		WorktreesDir:     worktreesDir,
		InstructionsPath: snapshot.InstructionsPath,
		CodexAuthPath:    codexAuthPath,
		ContainerNetwork: envCfg.ContainerNetwork,
		ContainerCPUs:    envCfg.ContainerCPUs,
		ContainerMemory:  envCfg.ContainerMemory,
		Prompts:          prompts.NewManager(promptsDir),
		WorkspaceManager: wsMgr,
		Reg:              reg,
	})

	r.PruneUnknownWorktrees()

	// Set up signal-based context so background workers stop on SIGTERM/Interrupt.
	// Created before recovery so orphan monitors can be cancelled on shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)
	defer stop()

	if s != nil {
		runner.RecoverOrphanedTasks(ctx, s, r)
	}
	go r.StartWorktreeGC(ctx)
	go r.StartWorktreeHealthWatcher(ctx)

	h := handler.NewHandler(s, r, configDir, workspaces, reg)
	r.SetStopReasonHandler(func(_ uuid.UUID, stopReason string) {
		if stopReason == "max_tokens" {
			h.SetAutopilot(false)
		}
	})
	r.SetAutosubmitFunc(h.AutosubmitEnabled)
	r.SetIdeationExploitRatioFunc(h.IdeationExploitRatio)

	// Start the auto-promoter: watches for state changes and promotes
	// backlog tasks to in_progress when capacity is available.
	h.StartAutoPromoter(ctx)
	h.StartAutoRetrier(ctx)

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

	// Start the auto-refiner: triggers refinement for backlog tasks that
	// have not yet been refined, when auto-refine is enabled.
	h.StartAutoRefiner(ctx)

	// Start the webhook notifier if a URL is configured in the env file.
	var wn *runner.WebhookNotifier
	if envCfg.WebhookURL != "" {
		wn = runner.NewWorkspaceWebhookNotifier(wsMgr, envCfg)
		go wn.Start(ctx)
	}

	// Register scrape-time gauge collectors. HTTP counter and histogram are
	// created inside loggingMiddleware so they are available via the same
	// registry for /metrics.
	reg.Gauge(
		"wallfacer_tasks_total",
		"Number of tasks grouped by status and archived flag.",
		func() []metrics.LabeledValue {
			active, ok := wsMgr.Store()
			if !ok {
				return nil
			}
			tasks, err := active.ListTasks(context.Background(), true)
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
			active, ok := wsMgr.Store()
			if !ok {
				return []metrics.LabeledValue{{Value: 0}}
			}
			return []metrics.LabeledValue{{Value: float64(active.SubscriberCount())}}
		},
	)
	reg.Gauge(
		"wallfacer_failed_tasks_by_category",
		"Number of currently-failed (non-archived) tasks grouped by failure_category.",
		func() []metrics.LabeledValue {
			active, ok := wsMgr.Store()
			if !ok {
				return nil
			}
			tasks, err := active.ListTasks(context.Background(), false /* exclude archived */)
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
		},
	)
	reg.Gauge(
		"wallfacer_circuit_breaker_open",
		"1 when the container launch circuit breaker is open (runtime unavailable), 0 when closed.",
		func() []metrics.LabeledValue {
			v := 0.0
			if r.ContainerCircuitOpen() {
				v = 1.0
			}
			return []metrics.LabeledValue{{Value: v}}
		},
	)
	reg.Counter(
		"wallfacer_autopilot_actions_total",
		"Total number of autonomous actions taken by autopilot watchers, by watcher and outcome.",
	)

	mux := BuildMux(h, reg, IndexViewData{ServerAPIKey: envCfg.ServerAPIKey}, uiFS, docsFS)

	host, _, _ := net.SplitHostPort(*addr)
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		logger.Main.Warn("requested address unavailable, finding free port", "addr", *addr, "error", err)
		ln, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			logger.Fatal("listen", "error", err)
		}
	}

	actualHostPort := normalizeBrowserVisibleHostPort(*addr, ln.Addr())
	actualPort := ln.Addr().(*net.TCPAddr).Port
	if !*noBrowser {
		browserHost := host
		if browserHost == "" || browserHost == "0.0.0.0" || browserHost == "::" || browserHost == "[::]" {
			browserHost = "localhost"
		}
		go openBrowser(fmt.Sprintf("http://%s:%d", browserHost, actualPort))
	}

	srv := &http.Server{
		Handler:     loggingMiddleware(handler.CSRFMiddleware(actualHostPort)(handler.BearerAuthMiddleware(envCfg.ServerAPIKey)(mux)), reg),
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
			logger.Fatal("server", "error", err)
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

	// Drain any in-flight webhook deliveries before process exit.
	if wn != nil {
		wn.Wait()
	}

	logger.Main.Info("shutdown complete")
}

// BuildMux constructs the HTTP request router.
//
// All API routes are registered from apicontract.Routes (the single source of
// truth). The handlers map below pairs each route Name with its http.HandlerFunc,
// applying per-route middleware (e.g. UUID parsing via withID) at map
// construction time. A startup panic is triggered if a route in the contract
// has no corresponding handler entry, preventing silent drift.
func BuildMux(h *handler.Handler, reg *metrics.Registry, indexData IndexViewData, uiFS, docsFS fs.FS) *http.ServeMux {
	mux := http.NewServeMux()

	// Static files (task board UI).
	uiSub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		logger.Fatal("sub ui fs", "error", err)
	}
	indexTemplates, err := template.New("index.html").ParseFS(uiSub, "index.html", "partials/*.html")
	if err != nil {
		logger.Fatal("parse ui templates", "error", err)
	}
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := indexTemplates.ExecuteTemplate(w, "index.html", indexData); err != nil {
			logger.Main.Error("render index", "error", err)
			http.Error(w, "failed to render index", http.StatusInternalServerError)
		}
	}
	mux.HandleFunc("GET /", serveIndex)

	// Static asset directories served from the embedded filesystem.
	mux.Handle("GET /css/", http.FileServer(http.FS(uiSub)))
	mux.Handle("GET /js/", http.FileServer(http.FS(uiSub)))

	// Docs API — list and serve embedded documentation.
	//
	// Guide reading order is derived from docs/guide/usage.md: the server
	// parses markdown links of the form [Title](file.md) under numbered
	// "### N." headings in the "Reading Order" section. This keeps the
	// order in a single place (the index doc) rather than hardcoded here.
	// parseReadingOrder extracts an ordered list of .md filenames from
	// the "## Reading Order" section of an index document. Each link of
	// the form [Title](file.md) under a numbered ### heading is collected
	// in order. This is used for both guide and internals indexes.
	parseReadingOrder := func(path string) []string {
		var order []string
		indexData, err := fs.ReadFile(docsFS, path)
		if err != nil {
			return order
		}
		inReadingOrder := false
		for line := range strings.SplitSeq(string(indexData), "\n") {
			trimmed := strings.TrimSpace(line)
			// Enter the reading order section.
			if trimmed == "## Reading Order" {
				inReadingOrder = true
				continue
			}
			// Exit on next ## heading.
			if inReadingOrder && strings.HasPrefix(trimmed, "## ") && trimmed != "## Reading Order" {
				break
			}
			if !inReadingOrder {
				continue
			}
			// Match markdown links like [Title](file.md).
			if _, after, ok := strings.Cut(trimmed, "]("); ok {
				if target, _, ok := strings.Cut(after, ")"); ok {
					// Only .md files in the same directory (no path separators).
					if strings.HasSuffix(target, ".md") && !strings.Contains(target, "/") {
						name := strings.TrimSuffix(target, ".md")
						order = append(order, name)
					}
				}
			}
		}
		return order
	}
	guideOrder := parseReadingOrder("docs/guide/usage.md")
	internalsOrder := parseReadingOrder("docs/internals/internals.md")
	mux.HandleFunc("GET /api/docs", func(w http.ResponseWriter, _ *http.Request) {
		type docEntry struct {
			Slug     string `json:"slug"`
			Title    string `json:"title"`
			Category string `json:"category"`
			Order    int    `json:"order"` // 1-based reading order; 0 = unordered
		}

		// Helper: read title from first "# " line in a markdown file.
		readTitle := func(path, fallback string) string {
			data, _ := fs.ReadFile(docsFS, path)
			for _, line := range strings.SplitN(string(data), "\n", 10) {
				if title, ok := strings.CutPrefix(line, "# "); ok {
					return title
				}
			}
			return fallback
		}

		var entries []docEntry

		// Guide: emit usage.md (the index page) first, then the
		// defined reading order, then any remaining guide files.
		if title := readTitle("docs/guide/usage.md", "User Manual"); true {
			entries = append(entries, docEntry{Slug: "guide/usage", Title: title, Category: "guide"})
		}
		ordered := make(map[string]bool, len(guideOrder)+1)
		ordered["usage"] = true
		seq := 0
		for _, name := range guideOrder {
			ordered[name] = true
			path := "docs/guide/" + name + ".md"
			if _, err := fs.ReadFile(docsFS, path); err != nil {
				continue
			}
			seq++
			slug := "guide/" + name
			title := readTitle(path, name)
			entries = append(entries, docEntry{Slug: slug, Title: title, Category: "guide", Order: seq})
		}
		// Append any guide docs not in the explicit order.
		if dir, err := fs.ReadDir(docsFS, "docs/guide"); err == nil {
			for _, f := range dir {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
					continue
				}
				name := strings.TrimSuffix(f.Name(), ".md")
				if ordered[name] {
					continue
				}
				slug := "guide/" + name
				title := readTitle("docs/guide/"+f.Name(), name)
				entries = append(entries, docEntry{Slug: slug, Title: title, Category: "guide", Order: 0})
			}
		}

		// Internals: emit internals.md (the index page) first, then
		// the defined reading order, then any remaining internals files.
		if title := readTitle("docs/internals/internals.md", "Technical Internals"); true {
			entries = append(entries, docEntry{Slug: "internals/internals", Title: title, Category: "internals"})
		}
		intOrdered := make(map[string]bool, len(internalsOrder)+1)
		intOrdered["internals"] = true
		intSeq := 0
		for _, name := range internalsOrder {
			intOrdered[name] = true
			path := "docs/internals/" + name + ".md"
			if _, err := fs.ReadFile(docsFS, path); err != nil {
				continue
			}
			intSeq++
			slug := "internals/" + name
			title := readTitle(path, name)
			entries = append(entries, docEntry{Slug: slug, Title: title, Category: "internals", Order: intSeq})
		}
		// Append any internals docs not in the explicit order.
		if dir, err := fs.ReadDir(docsFS, "docs/internals"); err == nil {
			for _, f := range dir {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
					continue
				}
				name := strings.TrimSuffix(f.Name(), ".md")
				if intOrdered[name] {
					continue
				}
				slug := "internals/" + name
				title := readTitle("docs/internals/"+f.Name(), name)
				entries = append(entries, docEntry{Slug: slug, Title: title, Category: "internals", Order: 0})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries) //nolint:errcheck
	})
	mux.HandleFunc("GET /api/docs/{slug...}", func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		// Prevent path traversal.
		if strings.Contains(slug, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		data, err := fs.ReadFile(docsFS, "docs/"+slug+".md")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write(data) //nolint:errcheck
	})

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
		"Health":            h.Health,
		"GetSpanStats":      h.GetSpanStats,
		"GetRuntimeStatus":  h.GetRuntimeStatus,
		"BoardManifest":     h.BoardManifest,
		"TaskBoardManifest": withID(h.TaskBoardManifest),

		// Container monitoring.
		"GetContainers": h.GetContainers,

		// Sandbox image management.
		"GetImageStatus":  h.GetImageStatus,
		"PullImage":       h.PullImage,
		"DeleteImage":     h.DeleteImage,
		"StreamImagePull": h.StreamImagePull,

		// File listing.
		"GetFiles": h.GetFiles,

		// Server configuration.
		"GetConfig":        h.GetConfig,
		"UpdateConfig":     h.UpdateConfig,
		"BrowseWorkspaces": h.BrowseWorkspaces,
		"UpdateWorkspaces": h.UpdateWorkspaces,

		// Ideation agent.
		"GetIdeationStatus": h.GetIdeationStatus,
		"TriggerIdeation":   h.TriggerIdeation,
		"CancelIdeation":    h.CancelIdeation,

		// Environment configuration.
		"GetEnvConfig":    h.GetEnvConfig,
		"UpdateEnvConfig": h.UpdateEnvConfig,
		"TestSandbox":     h.TestSandbox,
		"TestWebhook":     h.TestWebhook,

		// Workspace instructions.
		"GetInstructions":    h.GetInstructions,
		"UpdateInstructions": h.UpdateInstructions,
		"ReinitInstructions": h.ReinitInstructions,

		// Prompt templates.
		"ListSystemPrompts":  h.ListSystemPrompts,
		"GetSystemPrompt":    h.GetSystemPrompt,
		"UpdateSystemPrompt": h.UpdateSystemPrompt,
		"DeleteSystemPrompt": h.DeleteSystemPrompt,

		"ListTemplates":  h.ListTemplates,
		"CreateTemplate": h.CreateTemplate,
		"DeleteTemplate": h.DeleteTemplate,

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
		"ListDeletedTasks":         h.ListDeletedTasks,

		// Task instance operations (UUID extracted via withID).
		"UpdateTask":     withID(h.UpdateTask),
		"DeleteTask":     withID(h.DeleteTask),
		"GetEvents":      withID(h.GetEvents),
		"SubmitFeedback": withID(h.SubmitFeedback),
		"CompleteTask":   withID(h.CompleteTask),
		"CancelTask":     withID(h.CancelTask),
		"ResumeTask":     withID(h.ResumeTask),
		"RestoreTask":    withID(h.RestoreTask),
		"ArchiveTask":    withID(h.ArchiveTask),
		"UnarchiveTask":  withID(h.UnarchiveTask),
		"SyncTask":       withID(h.SyncTask),
		"TestTask":       withID(h.TestTask),

		"TaskDiff":   withID(h.TaskDiff),
		"StreamLogs": withID(h.StreamLogs),

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

	bodyLimits := map[string]int64{
		// Server configuration.
		"UpdateConfig": handler.BodyLimitDefault,

		// Ideation agent.
		"TriggerIdeation": handler.BodyLimitDefault,

		// Environment configuration.
		"UpdateEnvConfig": handler.BodyLimitDefault,
		"TestSandbox":     handler.BodyLimitDefault,
		"TestWebhook":     handler.BodyLimitDefault,

		// Workspace instructions.
		"UpdateInstructions": handler.BodyLimitInstructions,
		"ReinitInstructions": handler.BodyLimitDefault,

		// System prompt templates.
		"UpdateSystemPrompt": handler.BodyLimitDefault,

		// Prompt templates.
		"CreateTemplate": handler.BodyLimitDefault,

		// Git workspace operations.
		"GitPush":          handler.BodyLimitDefault,
		"GitSyncWorkspace": handler.BodyLimitDefault,
		"GitRebaseOnMain":  handler.BodyLimitDefault,
		"GitCheckout":      handler.BodyLimitDefault,
		"GitCreateBranch":  handler.BodyLimitDefault,
		"OpenFolder":       handler.BodyLimitDefault,

		// Task collection.
		"CreateTask":               handler.BodyLimitDefault,
		"BatchCreateTasks":         handler.BodyLimitDefault,
		"GenerateMissingTitles":    handler.BodyLimitDefault,
		"GenerateMissingOversight": handler.BodyLimitDefault,
		"ArchiveAllDone":           handler.BodyLimitDefault,

		// Task instance operations.
		"UpdateTask":     handler.BodyLimitDefault,
		"DeleteTask":     handler.BodyLimitDefault,
		"SubmitFeedback": handler.BodyLimitFeedback,
		"CompleteTask":   handler.BodyLimitDefault,
		"ResumeTask":     handler.BodyLimitDefault,
		"TestTask":       handler.BodyLimitDefault,

		// Refinement agent.
		"StartRefinement": handler.BodyLimitDefault,
		"RefineApply":     handler.BodyLimitDefault,
		"RefineDismiss":   handler.BodyLimitDefault,
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
		var registered http.Handler = fn
		if limit, ok := bodyLimits[route.Name]; ok {
			registered = handler.MaxBytesMiddleware(limit)(registered)
		}
		if requiresStore(route.Name) {
			registered = h.RequireStoreMiddleware(registered)
		}
		mux.Handle(route.FullPattern(), registered)
	}

	// Prometheus metrics endpoint (not an API route; excluded from the contract).
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		reg.WritePrometheus(w)
	})

	return mux
}

func normalizeBrowserVisibleHostPort(requestedAddr string, addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		reqHost, _, splitErr := net.SplitHostPort(requestedAddr)
		if splitErr == nil && reqHost != "" && reqHost != "0.0.0.0" && reqHost != "::" && reqHost != "[::]" {
			host = reqHost
		} else {
			host = "localhost"
		}
	}
	return net.JoinHostPort(host, port)
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

func resolveStartupWorkspaces(noWorkspaces bool, cliArgs []string, envFile string) []string {
	if noWorkspaces {
		return []string{}
	}
	if len(cliArgs) > 0 {
		return mustResolveWorkspaces(cliArgs)
	}
	cfg, err := envconfig.Parse(envFile)
	if err == nil && len(cfg.Workspaces) > 0 {
		if resolved, err := tryResolveWorkspaces(cfg.Workspaces); err == nil {
			return resolved
		}
		logger.Main.Warn("persisted workspaces invalid; starting without workspaces")
		return nil
	}
	return nil
}

func mustResolveWorkspaces(paths []string) []string {
	resolved, err := tryResolveWorkspaces(paths)
	if err != nil {
		logger.Fatal("resolve workspaces", "error", err)
	}
	return resolved
}

func tryResolveWorkspaces(paths []string) ([]string, error) {
	resolved := make([]string, 0, len(paths))
	for _, ws := range paths {
		abs, err := filepath.Abs(ws)
		if err != nil {
			return nil, err
		}
		clean := filepath.Clean(abs)
		info, err := os.Stat(clean)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", clean)
		}
		resolved = append(resolved, clean)
	}
	return resolved, nil
}

func requiresStore(name string) bool {
	switch name {
	case "GetConfig", "UpdateConfig", "BrowseWorkspaces", "UpdateWorkspaces", "GetEnvConfig", "UpdateEnvConfig", "TestSandbox", "TestWebhook", "GitStatus", "GitStatusStream":
		return false
	default:
		return true
	}
}
