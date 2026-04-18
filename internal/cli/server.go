package cli

import (
	"bufio"
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
	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/handler"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/planner"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
	"github.com/google/uuid"
)

// prefixFS wraps an inner fs.FS so its contents appear under a single
// directory prefix. Used in dev mode to expose os.DirFS("…/ui") as if it
// were the embedded uiFiles filesystem (which has "ui" at its root), so
// that fs.Sub(uiFS, "ui") and subsequent template/static lookups work
// identically against either source.
type prefixFS struct {
	inner  fs.FS
	prefix string
}

// noCacheMiddleware sets response headers that disable browser caching,
// used in UI dev mode so edits are visible on reload.
func noCacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next.ServeHTTP(w, r)
	})
}

func (p prefixFS) Open(name string) (fs.File, error) {
	if name == p.prefix {
		return p.inner.Open(".")
	}
	if rest, ok := strings.CutPrefix(name, p.prefix+"/"); ok {
		return p.inner.Open(rest)
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// IndexViewData holds the data passed to the index.html template.
type IndexViewData struct {
	ServerAPIKey string
}

// ServerConfig holds the parsed flag values shared by RunServer and RunDesktop.
// Each field corresponds to a CLI flag or environment variable override.
type ServerConfig struct {
	LogFormat    string
	Addr         string
	DataDir      string
	ContainerCmd string
	SandboxImage string
	EnvFile      string
	SkipCSRF     bool // Desktop mode: requests come from the local WebView, not a browser.
	// UIDir, when non-empty, serves UI assets from this on-disk directory
	// instead of the embedded filesystem. Used during frontend development
	// so edits under ui/ take effect on reload without rebuilding the binary.
	UIDir string
}

// ServerComponents holds the initialized server components returned by initServer.
type ServerComponents struct {
	Srv     *http.Server
	Ln      net.Listener
	Runner  *runner.Runner
	Handler *handler.Handler
	Planner *planner.Planner
	WsMgr   *workspace.Manager
	Ctx     context.Context
	Stop    context.CancelFunc

	// ActualPort is the TCP port the listener is bound to.
	ActualPort int

	// ServerAPIKey is the configured API key for server authentication.
	ServerAPIKey string
}

// Shutdown performs a graceful shutdown: drains HTTP connections and waits
// for background runner goroutines to finish.
func (sc *ServerComponents) Shutdown() {
	sc.Stop()

	logger.Main.Info("shutting down http server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sc.Srv.Shutdown(shutdownCtx); err != nil {
		logger.Main.Error("http server shutdown", "error", err)
	}

	if sc.Planner != nil && sc.Planner.IsRunning() {
		logger.Main.Info("stopping planning sandbox")
		sc.Planner.Stop()
	}

	logger.Main.Info("shutting down runner")
	sc.Runner.Shutdown()
	logger.Main.Info("shutdown complete")
}

// initServer performs the full server initialization sequence shared by
// RunServer and RunDesktop. It creates the workspace manager, runner, handler,
// HTTP mux, listener, and http.Server. The caller is responsible for starting
// srv.Serve and managing the lifecycle (signals, shutdown).
func initServer(configDir string, cfg ServerConfig, uiFS, docsFS fs.FS) *ServerComponents {
	logger.Init(cfg.LogFormat)
	initConfigDir(configDir, cfg.EnvFile)

	// Workspaces start empty; the manager restores the last active set from
	// the persisted env file (WALLFACER_WORKSPACES). Users configure them
	// later via the Settings UI or PUT /api/workspaces.
	var workspaces []string
	wsMgr, err := workspace.NewManager(configDir, cfg.DataDir, cfg.EnvFile, workspaces)
	if err != nil {
		logger.Fatal("workspace manager", "error", err)
	}
	snapshot := wsMgr.Snapshot()
	s := snapshot.Store
	if s != nil {
		logger.Main.Info("store loaded", "path", snapshot.ScopedDataDir)

		tombstoneRetentionDays := constants.DefaultTombstoneRetentionDays
		if v, err := strconv.Atoi(os.Getenv("WALLFACER_TOMBSTONE_RETENTION_DAYS")); err == nil && v > 0 {
			tombstoneRetentionDays = v
		}
		s.PurgeExpiredTombstones(tombstoneRetentionDays)
	}

	worktreesDir := filepath.Join(configDir, "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		logger.Fatal("create worktrees dir", "error", err)
	}

	tmpDir := filepath.Join(configDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		logger.Fatal("create tmp dir", "error", err)
	}

	if snapshot.InstructionsPath != "" {
		logger.Main.Info("workspace instructions", "path", snapshot.InstructionsPath)
	}

	resolvedImage := ensureImage(cfg.ContainerCmd, cfg.SandboxImage)
	codexAuthPath := ""
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		codexAuthPath = filepath.Join(home, ".codex")
	}

	envCfg := envconfig.Config{}
	if parsed, err := envconfig.Parse(cfg.EnvFile); err == nil {
		envCfg = parsed
	}

	reg := metrics.NewRegistry()

	promptsDir := filepath.Join(configDir, "prompts")
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:          cfg.ContainerCmd,
		SandboxImage:     resolvedImage,
		EnvFile:          cfg.EnvFile,
		Workspaces:       workspaces,
		WorktreesDir:     worktreesDir,
		TmpDir:           tmpDir,
		InstructionsPath: snapshot.InstructionsPath,
		CodexAuthPath:    codexAuthPath,
		SandboxBackend:   envCfg.SandboxBackend,
		ContainerNetwork: envCfg.ContainerNetwork,
		ContainerCPUs:    envCfg.ContainerCPUs,
		ContainerMemory:  envCfg.ContainerMemory,
		Prompts:          prompts.NewManager(promptsDir),
		WorkspaceManager: wsMgr,
		Reg:              reg,
	})

	r.PruneUnknownWorktrees()

	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)

	// Recover tasks that were in_progress when the server last crashed or
	// was killed. They are moved back to backlog so the auto-promoter can
	// re-schedule them.
	if s != nil {
		runner.RecoverOrphanedTasks(ctx, s, r)
	}
	// Background goroutines for worktree maintenance: GC removes stale
	// worktrees from completed/cancelled tasks; health watcher detects and
	// repairs corrupted worktree checkouts.
	go r.StartWorktreeGC(ctx)
	go r.StartWorktreeHealthWatcher(ctx)

	h := handler.NewHandler(s, r, configDir, workspaces, reg)
	// When a dispatched task completes, update the source spec to "complete".
	if s != nil {
		s.OnDone = handler.SpecCompletionHook(h.CurrentWorkspaces)
	}
	// Safety valve: disable autopilot if any task hits the max_tokens limit,
	// which indicates context window exhaustion — continuing blindly would
	// waste budget without progress.
	r.SetStopReasonHandler(func(_ uuid.UUID, stopReason string) {
		if stopReason == "max_tokens" {
			h.SetAutopilot(false)
		}
	})
	r.SetAutosubmitFunc(h.AutosubmitEnabled)
	r.SetIdeationExploitRatioFunc(h.IdeationExploitRatio)

	// Create and wire the planning sandbox manager.
	p := planner.New(planner.Config{
		Backend:          r.SandboxBackend(),
		Command:          cfg.ContainerCmd,
		Image:            resolvedImage,
		Workspaces:       snapshot.Workspaces,
		EnvFile:          cfg.EnvFile,
		Fingerprint:      snapshot.Key,
		InstructionsPath: snapshot.InstructionsPath,
		Network:          envCfg.ContainerNetwork,
		CPUs:             envCfg.ContainerCPUs,
		Memory:           envCfg.ContainerMemory,
		ConfigDir:        configDir,
	})
	h.SetPlanner(p)
	r.SetPlanner(p)

	h.StartAutoPromoter(ctx)
	h.StartAutoRetrier(ctx)
	h.StartIdeationWatcher(ctx)
	h.StartWaitingSyncWatcher(ctx)
	h.StartAutoTester(ctx)
	h.StartAutoSubmitter(ctx)
	h.StartAutoRefiner(ctx)

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
			tasks, err := active.ListTasks(context.Background(), false)
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

	// Bind the listening socket. If the requested port is taken (e.g. another
	// wallfacer instance), fall back to an OS-assigned free port so the server
	// still starts rather than failing outright.
	host, _, _ := net.SplitHostPort(cfg.Addr)
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		logger.Main.Warn("requested address unavailable, finding free port", "addr", cfg.Addr, "error", err)
		ln, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			logger.Fatal("listen", "error", err)
		}
	}

	actualHostPort := normalizeBrowserVisibleHostPort(cfg.Addr, ln.Addr())
	actualPort := ln.Addr().(*net.TCPAddr).Port

	if cfg.UIDir != "" {
		absUI, err := filepath.Abs(cfg.UIDir)
		if err != nil {
			logger.Fatal("resolve ui-dir", "error", err)
		}
		info, err := os.Stat(absUI)
		if err != nil || !info.IsDir() {
			logger.Fatal("ui-dir is not a directory", "path", absUI, "error", err)
		}
		logger.Main.Info("serving UI from disk (dev mode)", "path", absUI)
		uiFS = prefixFS{inner: os.DirFS(absUI), prefix: "ui"}
	}

	mux := BuildMux(h, reg, IndexViewData{ServerAPIKey: envCfg.ServerAPIKey}, uiFS, docsFS)

	if cfg.SkipCSRF {
		// Desktop mode: the Wails asset server reverse-proxies HTTP requests
		// but cannot forward WebSocket upgrades. Expose the real server port
		// so the frontend JS can open WebSocket connections directly.
		portStr := strconv.Itoa(actualPort)
		mux.HandleFunc("GET /api/desktop-port", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(portStr))
		})
	}

	// Middleware stack (outermost first): logging → CSRF → bearer auth → mux.
	// Desktop mode skips CSRF because requests originate from the local WebView
	// (same-origin checks are not meaningful).
	srvHandler := handler.BearerAuthMiddleware(envCfg.ServerAPIKey)(mux)
	if !cfg.SkipCSRF {
		srvHandler = handler.CSRFMiddleware(actualHostPort)(srvHandler)
	}
	srv := &http.Server{
		Handler:     loggingMiddleware(srvHandler, reg),
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	return &ServerComponents{
		Srv:          srv,
		Ln:           ln,
		Runner:       r,
		Handler:      h,
		Planner:      p,
		WsMgr:        wsMgr,
		Ctx:          ctx,
		Stop:         stop,
		ActualPort:   actualPort,
		ServerAPIKey: envCfg.ServerAPIKey,
	}
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
	uiDir := fs.String("ui-dir", envOrDefault("UI_DIR", ""), "serve UI from this on-disk directory (dev mode; disables caching and reloads templates on every request)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: wallfacer run [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Start the task board server and open the web UI.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	sc := initServer(configDir, ServerConfig{
		LogFormat:    *logFormat,
		Addr:         *addr,
		DataDir:      *dataDir,
		ContainerCmd: *containerCmd,
		SandboxImage: *sandboxImage,
		EnvFile:      *envFile,
		UIDir:        *uiDir,
	}, uiFS, docsFS)
	defer sc.Stop()

	if !*noBrowser {
		host, _, _ := net.SplitHostPort(*addr)
		browserHost := host
		if browserHost == "" || browserHost == "0.0.0.0" || browserHost == "::" || browserHost == "[::]" {
			browserHost = "localhost"
		}
		go openBrowser(fmt.Sprintf("http://%s:%d", browserHost, sc.ActualPort))
	}

	srvErr := make(chan error, 1)
	go func() {
		srvErr <- sc.Srv.Serve(sc.Ln)
	}()

	logger.Main.Info("listening", "addr", sc.Ln.Addr().String())

	select {
	case <-sc.Ctx.Done():
		logger.Main.Info("received shutdown signal, shutting down gracefully")
	case err := <-srvErr:
		if err != nil && err != http.ErrServerClosed {
			logger.Fatal("server", "error", err)
		}
		return
	}

	sc.Shutdown()
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
	// In dev mode (ui served from disk), re-parse templates on every
	// request so edits to index.html and partials are picked up live.
	_, devMode := uiFS.(prefixFS)
	parseIndexTemplates := func() (*template.Template, error) {
		return template.New("index.html").ParseFS(uiSub, "index.html", "partials/*.html")
	}
	indexTemplates, err := parseIndexTemplates()
	if err != nil {
		logger.Fatal("parse ui templates", "error", err)
	}
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		tpl := indexTemplates
		if devMode {
			t, err := parseIndexTemplates()
			if err != nil {
				logger.Main.Error("reparse ui templates", "error", err)
				http.Error(w, "failed to parse templates", http.StatusInternalServerError)
				return
			}
			tpl = t
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tpl.ExecuteTemplate(w, "index.html", indexData); err != nil {
			logger.Main.Error("render index", "error", err)
			http.Error(w, "failed to render index", http.StatusInternalServerError)
		}
	}
	mux.HandleFunc("GET /", serveIndex)

	// Static asset directories served from the embedded (or on-disk) filesystem.
	staticFS := http.FileServer(http.FS(uiSub))
	if devMode {
		// Prevent browsers from caching stale assets during frontend edits.
		staticFS = noCacheMiddleware(staticFS)
	}
	mux.Handle("GET /css/", staticFS)
	mux.Handle("GET /js/", staticFS)
	mux.Handle("GET /assets/", staticFS)

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
		if err := json.NewEncoder(w).Encode(entries); err != nil {
			logger.Main.Debug("docs list response write failed", "error", err)
		}
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
		if _, err := w.Write(data); err != nil {
			logger.Main.Debug("docs content response write failed", "error", err)
		}
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
		"MkdirWorkspace":   h.MkdirWorkspace,
		"RenameWorkspace":  h.RenameWorkspace,
		"UpdateWorkspaces": h.UpdateWorkspaces,

		// Spec tree.
		"GetSpecTree":     h.GetSpecTree,
		"SpecTreeStream":  h.SpecTreeStream,
		"DispatchSpecs":   h.DispatchSpecs,
		"UndispatchSpecs": h.UndispatchSpecs,
		"ArchiveSpec":     h.ArchiveSpec,
		"UnarchiveSpec":   h.UnarchiveSpec,

		// Ideation agent.
		"GetIdeationStatus": h.GetIdeationStatus,
		"TriggerIdeation":   h.TriggerIdeation,
		"CancelIdeation":    h.CancelIdeation,

		// Routines.
		"ListRoutines":          h.ListRoutines,
		"CreateRoutine":         h.CreateRoutine,
		"UpdateRoutineSchedule": h.UpdateRoutineSchedule,
		"TriggerRoutine":        h.TriggerRoutine,

		// Planning sandbox.
		"GetPlanningStatus":        h.GetPlanningStatus,
		"StartPlanning":            h.StartPlanning,
		"StopPlanning":             h.StopPlanning,
		"GetPlanningMessages":      h.GetPlanningMessages,
		"SendPlanningMessage":      h.SendPlanningMessage,
		"ClearPlanningMessages":    h.ClearPlanningMessages,
		"StreamPlanningMessages":   h.StreamPlanningMessages,
		"InterruptPlanningMessage": h.InterruptPlanningMessage,
		"UndoPlanningRound":        h.UndoPlanningRound,
		"GetPlanningCommands":      h.GetPlanningCommands,

		// Planning chat threads.
		"ListPlanningThreads":     h.ListPlanningThreads,
		"CreatePlanningThread":    h.CreatePlanningThread,
		"RenamePlanningThread":    h.RenamePlanningThread,
		"ArchivePlanningThread":   h.ArchivePlanningThread,
		"UnarchivePlanningThread": h.UnarchivePlanningThread,
		"ActivatePlanningThread":  h.ActivatePlanningThread,

		// Environment configuration.
		"GetEnvConfig":    h.GetEnvConfig,
		"UpdateEnvConfig": h.UpdateEnvConfig,
		"TestSandbox":     h.TestSandbox,

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

		// File explorer.
		"ExplorerTree":      h.ExplorerTree,
		"ExplorerStream":    h.ExplorerStream,
		"ExplorerReadFile":  h.ExplorerReadFile,
		"ExplorerWriteFile": h.ExplorerWriteFile,

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

		// OAuth authentication
		"StartOAuth":  http.HandlerFunc(h.StartOAuth),
		"OAuthStatus": http.HandlerFunc(h.OAuthStatus),
		"CancelOAuth": http.HandlerFunc(h.CancelOAuth),
	}

	// bodyLimits restricts request body size for write endpoints. Routes
	// not listed here have no MaxBytesReader applied (e.g. GET, SSE, WebSocket).
	bodyLimits := map[string]int64{
		// Server configuration.
		"UpdateConfig": handler.BodyLimitDefault,

		// Ideation agent.
		"TriggerIdeation": handler.BodyLimitDefault,

		// Routines.
		"CreateRoutine":         handler.BodyLimitDefault,
		"UpdateRoutineSchedule": handler.BodyLimitDefault,
		"TriggerRoutine":        handler.BodyLimitDefault,

		// Planning sandbox.
		"StartPlanning":            handler.BodyLimitDefault,
		"SendPlanningMessage":      handler.BodyLimitDefault,
		"InterruptPlanningMessage": handler.BodyLimitDefault,
		"UndoPlanningRound":        handler.BodyLimitDefault,

		// Planning chat threads.
		"CreatePlanningThread":    handler.BodyLimitDefault,
		"RenamePlanningThread":    handler.BodyLimitDefault,
		"ArchivePlanningThread":   handler.BodyLimitDefault,
		"UnarchivePlanningThread": handler.BodyLimitDefault,
		"ActivatePlanningThread":  handler.BodyLimitDefault,

		// Environment configuration.
		"UpdateEnvConfig": handler.BodyLimitDefault,
		"TestSandbox":     handler.BodyLimitDefault,

		// Workspace instructions.
		"UpdateInstructions": handler.BodyLimitInstructions,
		"ReinitInstructions": handler.BodyLimitDefault,

		// System prompt templates.
		"UpdateSystemPrompt": handler.BodyLimitDefault,

		// Prompt templates.
		"CreateTemplate": handler.BodyLimitDefault,

		// Workspace browser.
		"MkdirWorkspace":  handler.BodyLimitDefault,
		"RenameWorkspace": handler.BodyLimitDefault,

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

	// WebSocket endpoint: interactive host terminal. Not in apicontract because
	// WebSocket upgrades don't follow REST request/response semantics.
	mux.HandleFunc("GET /api/terminal/ws", h.HandleTerminalWS)

	// Prometheus metrics endpoint (not an API route; excluded from the contract).
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		reg.WritePrometheus(w)
	})

	return mux
}

// normalizeBrowserVisibleHostPort derives a host:port string suitable for
// display and CSRF origin checks. When the listener bound to a wildcard
// address (0.0.0.0 or ::), it substitutes the originally requested host or
// falls back to "localhost" so the resulting address is reachable from a browser.
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

// WriteHeader captures the status code before delegating to the wrapped writer.
func (w *statusResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush delegates to the wrapped writer's Flush if it implements http.Flusher.
// This is required for SSE streaming through the logging middleware.
func (w *statusResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack delegates to the wrapped writer's Hijack if it implements http.Hijacker.
// This is required for WebSocket upgrades through the logging middleware.
func (w *statusResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
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

// ensureImage checks whether the sandbox image is present locally and pulls
// it from the registry if it is not. For sandbox-agents images, a locally-
// built sandbox-agents:latest (e.g. from a sibling latere-ai/images checkout)
// is preferred over a network pull — mirroring the Makefile's pull-images
// behavior so `wallfacer run` and `make build` stay in sync. If the pull
// fails, the same local fallback is used as a last resort.
// Returns the image reference that should actually be used.
func ensureImage(containerCmd, image string) string {
	out, err := cmdexec.New(containerCmd, "images", "-q", image).Output()
	if err == nil && out != "" {
		return image // already present
	}
	if fb, ok := localSandboxFallback(containerCmd, image); ok {
		logger.Main.Info("using local fallback sandbox image instead of pulling", "image", fb)
		return fb
	}
	logger.Main.Info("sandbox image not found locally, pulling from registry", "image", image)
	cmd := exec.Command(containerCmd, "pull", image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logger.Main.Warn("failed to pull sandbox image", "image", image, "error", err)
		if fb, ok := localSandboxFallback(containerCmd, image); ok {
			logger.Main.Info("using local fallback sandbox image after pull failure", "image", fb)
			return fb
		}
		logger.Main.Warn("no sandbox image available; tasks may fail")
	}
	return image
}

// localSandboxFallback returns sandbox-agents:latest if the requested image
// belongs to the sandbox-agents family and a local copy of the fallback is
// available. The scoping check prevents unrelated custom images from being
// silently substituted with the sandbox-agents fallback.
func localSandboxFallback(containerCmd, image string) (string, bool) {
	if image == fallbackSandboxImage || !isSandboxAgentsImage(image) {
		return "", false
	}
	out, err := cmdexec.New(containerCmd, "images", "-q", fallbackSandboxImage).Output()
	if err != nil || out == "" {
		return "", false
	}
	return fallbackSandboxImage, true
}

// isSandboxAgentsImage reports whether image refers to the sandbox-agents
// repository, regardless of registry prefix (e.g. matches both
// "ghcr.io/latere-ai/sandbox-agents:v0.0.6" and "sandbox-agents:latest").
func isSandboxAgentsImage(image string) bool {
	name := image
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.Index(name, ":"); i >= 0 {
		name = name[:i]
	}
	return name == "sandbox-agents"
}

// requiresStore returns true for route names that need an active workspace
// store. Routes that operate without a store (configuration, env settings,
// workspace browsing, git status) return false so the RequireStoreMiddleware
// is not applied and requests succeed even before workspaces are configured.
func requiresStore(name string) bool {
	switch name {
	case "GetConfig", "UpdateConfig", "BrowseWorkspaces", "MkdirWorkspace", "RenameWorkspace", "UpdateWorkspaces", "GetEnvConfig", "UpdateEnvConfig", "TestSandbox", "GitStatus", "GitStatusStream":
		return false
	default:
		return true
	}
}
