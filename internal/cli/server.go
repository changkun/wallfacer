package cli

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/agentsession"
	"latere.ai/x/wallfacer/internal/apicontract"
	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/handler"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/webserver"
	"latere.ai/x/wallfacer/internal/workspace"
)

// IndexViewData carries server-injected runtime config for the Vue SPA's
// index.html (delivered via the window.__WALLFACER__ script tag).
type IndexViewData struct {
	ServerAPIKey string
}

// ServerConfig holds the parsed flag values for RunServer.
// Each field corresponds to a CLI flag or environment variable override.
type ServerConfig struct {
	LogFormat string
	Addr      string
	DataDir   string
	EnvFile   string
}

// ServerComponents holds the initialized server components returned by initServer.
type ServerComponents struct {
	Srv          *http.Server
	Ln           net.Listener
	Runner       *runner.Runner
	AgentSession *agentsession.Runtime
	Ctx          context.Context
	Stop         context.CancelFunc

	// ActualPort is the TCP port the listener is bound to.
	ActualPort int
}

// shutdownTimeout is the budget for draining HTTP connections and stopping
// background workers before the process gives up and exits.
const shutdownTimeout = 5 * time.Second

// Serve runs the HTTP server on the already-bound listener until sc.Ctx is
// cancelled, then shuts everything down within shutdownTimeout. It returns nil
// on a clean stop; http.ErrServerClosed is the normal stop signal.
//
// The listener is bound up front by initServer rather than by
// srv.ListenAndServe, so the server can fall back to a free port and feed the
// resolved host:port into the CSRF middleware and the browser it opens.
func (sc *ServerComponents) Serve() error {
	srvErr := make(chan error, 1)
	go func() {
		err := sc.Srv.Serve(sc.Ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		srvErr <- err
	}()

	logger.Main.Info("listening", "addr", sc.Ln.Addr().String())

	select {
	case err := <-srvErr:
		// The server stopped on its own (accept failure or an early close).
		return err
	case <-sc.Ctx.Done():
		logger.Main.Info("received shutdown signal, shutting down gracefully")
	}
	sc.Shutdown()
	return nil
}

// Shutdown performs a graceful shutdown: drains HTTP connections and waits for
// background runner goroutines to finish.
func (sc *ServerComponents) Shutdown() {
	sc.Stop()

	logger.Main.Info("shutting down http server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := sc.Srv.Shutdown(shutdownCtx); err != nil {
		logger.Main.Error("http server shutdown", "error", err)
	}

	if sc.AgentSession != nil && sc.AgentSession.IsRunning() {
		logger.Main.Info("stopping agent session")
		sc.AgentSession.Stop()
	}

	logger.Main.Info("shutting down runner")
	sc.Runner.Shutdown()
	logger.Main.Info("shutdown complete")
}

// initServer performs the full server initialization sequence for RunServer.
// It creates the workspace manager, runner, handler,
// HTTP mux, listener, and http.Server. The caller is responsible for starting
// srv.Serve and managing the lifecycle (signals, shutdown).
func initServer(configDir string, cfg ServerConfig, vueDist fs.FS) *ServerComponents {
	// Configure the log format before anything else logs.
	logger.Init(cfg.LogFormat)

	initConfigDir(configDir, cfg.EnvFile)

	// One-time rename of the legacy <configDir>/planning state directory to
	// <configDir>/agent-sessions. Runs before any agent-session path is read.
	if moved, err := store.MigrateAgentSessionsDir(configDir); err != nil {
		logger.Main.Warn("agent-sessions dir migration failed", "error", err)
	} else if moved {
		logger.Main.Info("migrated legacy planning/ dir to agent-sessions/")
	}

	// One-time migration of the legacy path-keyed workspace-groups.json to the
	// stable-identity workspaces.json, adopting orphaned data directories as
	// dormant workspaces. Idempotent; runs before the workspace manager loads.
	migrationStamp := time.Now().UTC().Format("20060102-150405")
	if migrated, err := workspace.MigrateToWorkspaces(configDir, cfg.DataDir, migrationStamp); err != nil {
		logger.Main.Warn("workspace migration failed", "error", err)
	} else if migrated {
		logger.Main.Info("migrated workspace groups to workspaces.json")
	}

	// A nil initial set makes the manager restore the most recently used
	// workspace from workspaces.json. Workspaces are created and switched
	// later through the workspace picker (the /api/workspaces routes).
	var workspaces []string
	wsMgr, err := workspace.NewManager(configDir, cfg.DataDir, workspaces)
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

	codexAuthPath := ""
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		codexAuthPath = filepath.Join(home, ".codex")
	}

	envCfg := envconfig.Config{}
	parsed, err := envconfig.Parse(cfg.EnvFile)
	switch {
	case errors.Is(err, envconfig.ErrSecretStore):
		logger.Fatal("read provider credentials", "error", err)
	case err == nil:
		envCfg = parsed
	}

	r := runner.NewRunner(s, runner.RunnerConfig{
		EnvFile:            cfg.EnvFile,
		DefaultEnvFile:     filepath.Join(configDir, ".env"),
		Workspaces:         workspaces,
		WorktreesDir:       worktreesDir,
		TmpDir:             tmpDir,
		CodexAuthPath:      codexAuthPath,
		HostClaudeBinary:   envCfg.HostClaudeBinary,
		HostCodexBinary:    envCfg.HostCodexBinary,
		HostCursorBinary:   envCfg.HostCursorBinary,
		HostOpenCodeBinary: envCfg.HostOpenCodeBinary,
		HostPiBinary:       envCfg.HostPiBinary,
		AgentNice:          envCfg.AgentNice,
		MaxAgents:          envCfg.MaxAgents,
		WorkspaceManager:   wsMgr,
	})

	r.PruneUnknownWorktrees()

	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)

	// Recover tasks that were in_progress or committing when the server last
	// crashed or was killed (see runner.RecoverOrphanedTasks).
	if s != nil {
		runner.RecoverOrphanedTasks(ctx, s, r)
	}
	// Background goroutines for worktree maintenance: GC removes stale
	// worktrees from completed/cancelled tasks; health watcher detects and
	// repairs corrupted worktree checkouts.
	go r.StartWorktreeGC(ctx)
	go r.StartWorktreeHealthWatcher(ctx)

	h := handler.NewHandler(s, r, configDir, workspaces)

	// Delete the GitHub token cache under the config dir if one is present:
	// wallfacer holds no GitHub credential, so the files are unread secrets.
	removeGitHubTokenCache(configDir, logger.Main)

	// Say once per start that agent and fleet definitions left under the
	// config dir are not read; the files themselves stay where they are.
	warnRetiredFleetDirs(configDir, logger.Main)

	// A local instance is never open: without WALLFACER_SERVER_API_KEY the
	// API would accept any request from any host the listener is reachable
	// from, so generate a per-instance key and persist it for same-machine
	// clients. The browser receives it through index.html on a loopback
	// request (see indexKeyAllowed).
	if envCfg.ServerAPIKey == "" {
		keyPath := filepath.Join(configDir, serverAPIKeyFile)
		_, statErr := os.Stat(keyPath)
		key, kerr := loadOrCreateServerAPIKey(configDir)
		if kerr != nil {
			logger.Fatal("server api key", "error", kerr)
		}
		envCfg.ServerAPIKey = key
		if statErr != nil {
			logger.Main.Info("server api key: none configured; generated one for this machine", "path", keyPath)
		} else {
			logger.Main.Info("server api key: none configured; using the generated one", "path", keyPath)
		}
	}
	// When a dispatched task completes, update the source spec to "complete".
	if s != nil {
		// Drift pipeline: the agent-backed tester is wired, but gated behind
		// WALLFACER_DRIFT_TESTER (off by default). With the flag off the hook
		// preserves the historical complete-on-done behavior; with it on, a
		// completed task runs the drift tester before the spec lands.
		s.OnDone = handler.SpecCompletionHook(h.CurrentWorkspaces, handler.NewRunnerDriftTester(r), nil)
	}
	// Create and wire the agent-session runtime.
	p := agentsession.New(agentsession.Config{
		Backend:     r.SandboxBackend(),
		Workspaces:  snapshot.Workspaces,
		EnvFile:     cfg.EnvFile,
		Fingerprint: snapshot.Key,
		ConfigDir:   configDir,
	})
	h.SetAgentSession(p)
	r.SetAgentSession(p)

	// Bind the listening socket. If the requested port is taken (e.g. another
	// wallfacer instance), fall back to an OS-assigned free port so the server
	// still starts rather than failing outright. An address that does not
	// parse stops startup: read as an empty host, the fallback would bind
	// every interface.
	host, err := listenHost(cfg.Addr)
	if err != nil {
		logger.Fatal("listen", "error", err)
	}
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

	mux := BuildMux(h, IndexViewData{ServerAPIKey: envCfg.ServerAPIKey}, vueDist)

	// Middleware stack (outermost first): logging → CSRF → server key → mux.
	var srvHandler http.Handler = handler.BearerAuthMiddleware(envCfg.ServerAPIKey)(mux)
	srvHandler = handler.CSRFMiddleware(actualHostPort)(srvHandler)
	srv := &http.Server{
		Handler:     loggingMiddleware(srvHandler),
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	return &ServerComponents{
		Srv:          srv,
		Ln:           ln,
		Runner:       r,
		AgentSession: p,
		Ctx:          ctx,
		Stop:         stop,
		ActualPort:   actualPort,
	}
}

// requireClaudeOrExit fails fast when the claude CLI cannot be resolved, so
// the user-facing `run`/`desktop` commands surface an actionable message at
// startup instead of on the first task. The runner itself is built
// best-effort (see runner.NewRunner) so it stays usable for tests and
// env-config probing; this gate lives only at the command boundary.
func requireClaudeOrExit(envFile string) {
	explicit := ""
	if parsed, err := envconfig.Parse(envFile); err == nil {
		explicit = parsed.HostClaudeBinary
	}
	if err := executor.RequireClaude(explicit); err != nil {
		logger.Fatal("host sandbox backend", "error", err)
	}
}

// RunServer implements the `wallfacer run` subcommand.
// vueDist is the embedded filesystem containing the Vue SPA dist
// (frontend/dist).
func RunServer(configDir string, args []string, vueDist fs.FS) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)

	logFormat := fs.String("log-format", cmp.Or(os.Getenv("LOG_FORMAT"), "text"), `log output format: "text" or "json"`)
	addr := fs.String("addr", cmp.Or(os.Getenv("ADDR"), ":8080"), "listen address")
	dataDir := fs.String("data", cmp.Or(os.Getenv("DATA_DIR"), filepath.Join(configDir, "data")), "data directory")
	envFile := fs.String("env-file", cmp.Or(os.Getenv("ENV_FILE"), filepath.Join(configDir, ".env")), "env file with credentials and runtime settings")
	noBrowser := fs.Bool("no-browser", false, "do not open browser on start")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: wallfacer run [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Start the task board server and open the web UI.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if err := checkRunArgs(fs.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "wallfacer: %v\n\n", err)
		fs.Usage()
		os.Exit(2)
	}

	requireClaudeOrExit(*envFile)

	sc := initServer(configDir, ServerConfig{
		LogFormat: *logFormat,
		Addr:      *addr,
		DataDir:   *dataDir,
		EnvFile:   *envFile,
	}, vueDist)
	defer sc.Stop()

	if !*noBrowser {
		if url, err := browserURL(*addr, sc.ActualPort); err != nil {
			logger.Main.Warn("open browser skipped; open the board's address manually", "error", err)
		} else {
			go func() {
				if err := openBrowser(url); err != nil {
					logger.Main.Warn("open browser failed; open the address manually", "url", url, "error", err)
				}
			}()
		}
	}

	if err := sc.Serve(); err != nil {
		logger.Fatal("server", "error", err)
	}
}

// checkRunArgs refuses positional arguments to `wallfacer run`. The command
// takes flags only: the board reopens the most recently used workspace, and a
// workspace (one folder) is chosen in the board's workspace picker. Refusing
// keeps a folder named on the command line, or several of them, from being
// silently ignored.
func checkRunArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("wallfacer run takes no folder arguments (got %s); choose the workspace folder in the workspace picker on the board",
		strings.Join(args, " "))
}

// listenHost returns the host part of a listen address such as ":8080" or
// "127.0.0.1:8080". An address without a port, such as "127.0.0.1", is an
// error naming the address, so a caller reports it instead of reading the
// host as empty, which means every interface.
func listenHost(addr string) (string, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("listen address %q: %w", addr, err)
	}
	return host, nil
}

// browserURL is the address the browser opens for a server asked to listen on
// addr and bound to port. A wildcard or empty host becomes localhost, since a
// browser cannot dial a wildcard address.
func browserURL(addr string, port int) (string, error) {
	host, err := listenHost(addr)
	if err != nil {
		return "", err
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// mountVueSPA overlays Vue SPA routes onto an existing mux, claiming every
// path no other route matches, the static asset prefixes, and the unmatched
// paths under /api. Every route BuildMux registers is more specific than the
// catch-alls, so it keeps its requests. A hard load of a client route gets the
// shell; an unmatched /api path gets a JSON 404 (or 405 when a route serves the
// path under other methods), never the shell. See webserver.MountCatchAll for
// why the catch-alls are method-agnostic.
func mountVueSPA(mux *http.ServeMux, vueDist fs.FS, serverAPIKey string) {
	dist, err := fs.Sub(vueDist, "frontend/dist")
	if err != nil {
		logger.Main.Warn("vue-ui: no frontend/dist embedded", "error", err)
		return
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		logger.Main.Warn("vue-ui: frontend/dist has no index.html; run 'cd frontend && bun run build'")
		return
	}

	version := Version

	rawHTML, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		logger.Main.Warn("vue-ui: failed to read index.html", "error", err)
		return
	}
	// render bakes the runtime config into the shell.
	render := func(apiKey string) string {
		inject := fmt.Sprintf(
			`<script>window.__WALLFACER__={serverApiKey:%q,version:%q};</script>`,
			apiKey, version,
		)
		return strings.Replace(string(rawHTML), "</head>", inject+"</head>", 1)
	}
	// Two variants: the key is released only to callers indexKeyAllowed
	// admits; everyone else gets the shell with an empty serverApiKey.
	keyedHTML := render(serverAPIKey)
	anonHTML := render("")

	serveVueIndex := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		page := anonHTML
		if indexKeyAllowed(r, serverAPIKey) {
			page = keyedHTML
		}
		_, _ = w.Write([]byte(page))
	}

	files := http.FS(dist)
	fileServer := http.FileServer(files)
	cachedFileServer := withAssetCache(fileServer)
	webserver.MountCatchAll(mux, serveVueIndex)
	mux.Handle("GET /assets/", cachedFileServer)
	mux.Handle("GET /fonts/", cachedFileServer)
	mux.Handle("GET /static/", fileServer)
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(dist, "favicon.ico"); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
	logger.Main.Info("ui: serving Vue SPA")
}

const (
	immutableAssetCache = "public, max-age=31536000, immutable"
	staticAssetCache    = "public, max-age=604800, stale-while-revalidate=86400"
)

// assetCacheControl returns the Cache-Control value for a static asset
// path, or "" when none applies. Hashed /assets/* are immutable; the
// preloaded /fonts/ get a long stale-while-revalidate so the FOUT fix
// benefits repeat visits too.
func assetCacheControl(p string) string {
	if strings.HasPrefix(p, "/assets/") {
		return immutableAssetCache
	}
	if strings.HasPrefix(p, "/fonts/") {
		return staticAssetCache
	}
	return ""
}

// withAssetCache wraps a file server, stamping a Cache-Control header
// on hashed assets and preloaded fonts before delegating.
func withAssetCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cc := assetCacheControl(r.URL.Path); cc != "" {
			w.Header().Set("Cache-Control", cc)
		}
		next.ServeHTTP(w, r)
	})
}

// BuildMux constructs the HTTP request router.
//
// All API routes are registered from apicontract.Routes (the single source of
// truth). The handlers map below pairs each route Name with its http.HandlerFunc,
// applying per-route middleware (e.g. UUID parsing via withID) at map
// construction time. A startup panic is triggered if a route in the contract
// has no corresponding handler entry, preventing silent drift.
func BuildMux(h *handler.Handler, indexData IndexViewData, vueDist fs.FS) *http.ServeMux {
	mux := http.NewServeMux()

	if vueDist != nil {
		mountVueSPA(mux, vueDist, indexData.ServerAPIKey)
	}

	// withID wraps a handler that needs a parsed task UUID from the {id} path
	// segment, converting the UUID-accepting signature to http.HandlerFunc.
	// Delegates parsing + error response to internal/pkg/httpjson.PathUUID so
	// the parse contract matches every other {name}-keyed route.
	withID := func(fn func(http.ResponseWriter, *http.Request, uuid.UUID)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id, ok := httpjson.PathUUID(w, r, "id")
			if !ok {
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
		"GetRuntimeStatus":  h.GetRuntimeStatus,
		"BoardManifest":     h.BoardManifest,
		"TaskBoardManifest": withID(h.TaskBoardManifest),

		// File listing.
		"GetFiles": h.GetFiles,

		// Server configuration.
		"GetConfig":         h.GetConfig,
		"UpdateConfig":      h.UpdateConfig,
		"BrowseWorkspaces":  h.BrowseWorkspaces,
		"PickFolder":        h.PickFolder,
		"MkdirWorkspace":    h.MkdirWorkspace,
		"RenameWorkspace":   h.RenameWorkspace,
		"ListWorkspaces":    h.ListWorkspaces,
		"CreateWorkspace":   h.CreateWorkspace,
		"UpdateWorkspace":   h.UpdateWorkspace,
		"DeleteWorkspace":   h.DeleteWorkspace,
		"ActivateWorkspace": h.ActivateWorkspace,

		// Spec tree.
		"GetSpecTree":               h.GetSpecTree,
		"SpecTreeStream":            h.SpecTreeStream,
		"SpecTransition":            h.SpecTransition,
		"StaleCandidates":           h.StaleCandidates,
		"DismissAllStaleCandidates": h.DismissAllStaleCandidates,

		// Agent session.
		"GetAgentSessionStatus": h.GetAgentSessionStatus,
		"StartAgentSession":     h.StartAgentSession,
		"StopAgentSession":      h.StopAgentSession,
		"GetAgentMessages":      h.GetAgentMessages,
		"SendAgentMessage":      h.SendAgentMessage,
		"ClearAgentMessages":    h.ClearAgentMessages,
		"StreamAgentMessages":   h.StreamAgentMessages,
		"InterruptAgentMessage": h.InterruptAgentMessage,
		"UndoPlanningRound":     h.UndoPlanningRound,
		"GetAgentCommands":      h.GetAgentCommands,
		"UpdateTaskPromptTool":  h.UpdateTaskPromptTool,

		// Agent sessions.
		"ListAgentSessions":  h.ListAgentSessions,
		"CreateAgentSession": h.CreateAgentSession,
		"PatchAgentSession":  h.PatchAgentSession,
		"DeleteAgentSession": h.DeleteAgentSession,

		// Environment configuration.
		"GetEnvConfig":    h.GetEnvConfig,
		"UpdateEnvConfig": h.UpdateEnvConfig,
		"TestSandbox":     h.TestSandbox,

		// File explorer.
		"ExplorerTree":        h.ExplorerTree,
		"ExplorerStream":      h.ExplorerStream,
		"ExplorerReadFile":    h.ExplorerReadFile,
		"ExplorerWriteFile":   h.ExplorerWriteFile,
		"ExplorerFileStream":  h.ExplorerFileStream,
		"ExplorerTaskPrompts": h.ExplorerTaskPrompts,

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

		// Task collection (no {id}).
		"ListTasks":                h.ListTasks,
		"StreamTasks":              h.StreamTasks,
		"CreateTask":               h.CreateTask,
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
		"ResumeTask":     withID(h.ResumeTask),
		"SyncTask":       withID(h.SyncTask),

		"TaskCommits":  withID(h.TaskCommits),
		"TaskDiff":     withID(h.TaskDiff),
		"StreamLogs":   withID(h.StreamLogs),
		"GetTurnUsage": withID(h.GetTurnUsage),

		// ServeOutput needs both {id} (UUID) and {filename} path values.
		"ServeOutput": func(w http.ResponseWriter, r *http.Request) {
			id, err := uuid.Parse(r.PathValue("id"))
			if err != nil {
				http.Error(w, "invalid task id", http.StatusBadRequest)
				return
			}
			h.ServeOutput(w, r, id, r.PathValue("filename"))
		},

		// Task spans and oversight.
		"GetTaskSpans": withID(h.GetTaskSpans),
		"GetOversight": withID(h.GetOversight),
	}

	// bodyLimits restricts request body size for write endpoints. Routes
	// not listed here have no MaxBytesReader applied (e.g. GET, SSE).
	bodyLimits := map[string]int64{
		// Server configuration.
		"UpdateConfig": handler.BodyLimitDefault,

		// Spec tree.
		"SpecTransition": handler.BodyLimitDefault,

		// Agent session.
		"StartAgentSession":     handler.BodyLimitDefault,
		"SendAgentMessage":      handler.BodyLimitDefault,
		"InterruptAgentMessage": handler.BodyLimitDefault,
		"UndoPlanningRound":     handler.BodyLimitDefault,

		// Agent sessions.
		"CreateAgentSession": handler.BodyLimitDefault,
		"PatchAgentSession":  handler.BodyLimitDefault,

		// Environment configuration.
		"UpdateEnvConfig": handler.BodyLimitDefault,
		"TestSandbox":     handler.BodyLimitDefault,

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
		"GenerateMissingOversight": handler.BodyLimitDefault,
		"ArchiveAllDone":           handler.BodyLimitDefault,

		// Task instance operations.
		"UpdateTask":     handler.BodyLimitDefault,
		"DeleteTask":     handler.BodyLimitDefault,
		"SubmitFeedback": handler.BodyLimitFeedback,
		"CompleteTask":   handler.BodyLimitDefault,
		"ResumeTask":     handler.BodyLimitDefault,
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

// loggingMiddleware logs each HTTP request with its status and duration: API
// requests at info level, everything else (the SPA shell and its assets) at
// debug.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		dur := time.Since(start)

		if strings.HasPrefix(r.URL.Path, "/api/") {
			logger.Handler.Info(r.Method+" "+r.URL.Path, "status", sw.status, "dur", dur.Round(time.Millisecond))
		} else {
			logger.Handler.Debug(r.Method+" "+r.URL.Path, "status", sw.status, "dur", dur.Round(time.Millisecond))
		}
	})
}

// requiresStore returns true for route names that need an active workspace
// store. Routes that operate without a store (configuration, env settings,
// workspace browsing, git status) return false so the RequireStoreMiddleware
// is not applied and requests succeed even before workspaces are configured.
func requiresStore(name string) bool {
	switch name {
	case "GetConfig", "UpdateConfig", "BrowseWorkspaces", "PickFolder", "MkdirWorkspace", "RenameWorkspace", "GetEnvConfig", "UpdateEnvConfig", "TestSandbox", "GitStatus", "GitStatusStream",
		// Workspace management works before any workspace is open (the picker
		// needs to list/create/activate without an active store).
		"ListWorkspaces", "CreateWorkspace", "UpdateWorkspace", "DeleteWorkspace", "ActivateWorkspace":
		return false
	default:
		return true
	}
}
