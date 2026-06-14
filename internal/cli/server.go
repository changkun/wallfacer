package cli

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	"latere.ai/x/wallfacer/internal/apicontract"
	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/handler"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/metrics"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/planner"
	"latere.ai/x/wallfacer/internal/prompts"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
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
func initServer(configDir string, cfg ServerConfig, vueDist, docsFS fs.FS) *ServerComponents {
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
		EnvFile:          cfg.EnvFile,
		DefaultEnvFile:   filepath.Join(configDir, ".env"),
		Workspaces:       workspaces,
		WorktreesDir:     worktreesDir,
		TmpDir:           tmpDir,
		InstructionsPath: snapshot.InstructionsPath,
		CodexAuthPath:    codexAuthPath,
		HostClaudeBinary: envCfg.HostClaudeBinary,
		HostCodexBinary:  envCfg.HostCodexBinary,
		HostCursorBinary: envCfg.HostCursorBinary,
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

	// Cloud mode: wire latere.ai sign-in. Both the WALLFACER_CLOUD flag
	// and the AUTH_* vars resolve from shell env first, .env file
	// second — users can drop everything in ~/.wallfacer/.env or export
	// them on the command line. Shell wins so `AUTH_CLIENT_ID=other
	// wallfacer run` is a clean override without editing the file.
	envFileKV, _ := envconfig.ReadRaw(cfg.EnvFile)
	cloudMode := envconfig.ParseBoolFlag(envconfig.Lookup(envFileKV, "WALLFACER_CLOUD"))
	var (
		jwtValidator *auth.Validator
		authClient   *auth.Client
	)
	// Sign-in is wired by default using the public (secret-less) "wallfacer"
	// client, so a plain `wallfacer run` presents a login button with no
	// env-var setup. Any AUTH_* value (shell or .env) overrides the defaults,
	// and a confidential client (AUTH_CLIENT_SECRET set) works unchanged.
	// WALLFACER_CLOUD only controls whether sign-in is *forced* (see ForceLogin),
	// not whether it is available.
	authCfg := auth.Config{
		AuthURL:      envconfig.Lookup(envFileKV, "AUTH_URL"),
		ClientID:     envconfig.Lookup(envFileKV, "AUTH_CLIENT_ID"),
		ClientSecret: envconfig.Lookup(envFileKV, "AUTH_CLIENT_SECRET"),
		RedirectURL:  envconfig.Lookup(envFileKV, "AUTH_REDIRECT_URL"),
		CookieKey:    envconfig.Lookup(envFileKV, "AUTH_COOKIE_KEY"),
	}
	authCfg, err = resolveAuthConfig(authCfg, cfg.Addr, configDir)
	if err != nil {
		logger.Fatal("auth: resolve configuration", "error", err)
	}
	authClient = auth.New(authCfg)
	if authClient == nil {
		logger.Fatal("auth: client construction failed; check AUTH_* configuration")
	}
	h.SetAuth(authClient)

	// JWT validator for API requests that carry Authorization: Bearer
	// <jwt>. Issuer and JWKS URL fall back to AuthURL derivatives when
	// the deployment doesn't override them; audience is the OAuth
	// client ID so tokens minted for other services are rejected.
	jwtValidator = auth.BuildValidator(
		authCfg,
		envconfig.Lookup(envFileKV, "AUTH_JWKS_URL"),
		envconfig.Lookup(envFileKV, "AUTH_ISSUER"),
	)
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
		Workspaces:       snapshot.Workspaces,
		EnvFile:          cfg.EnvFile,
		Fingerprint:      snapshot.Key,
		InstructionsPath: snapshot.InstructionsPath,
		ConfigDir:        configDir,
	})
	h.SetPlanner(p)
	r.SetPlanner(p)

	h.StartAutoPromoter(ctx)
	h.StartAutoRetrier(ctx)
	// Ideation is now one instance of the routine primitive — its timer
	// lives inside the scheduler engine via a system:ideation routine.
	h.StartRoutineEngine(ctx)
	h.StartWaitingSyncWatcher(ctx)
	h.StartAutoTester(ctx)
	h.StartAutoSubmitter(ctx)

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

	mux := BuildMux(h, reg, IndexViewData{ServerAPIKey: envCfg.ServerAPIKey}, docsFS, vueDist, cloudMode)

	// Middleware stack (outermost first): logging → CSRF → CookieAuth
	//   → JWT OptionalAuth → bearer auth → mux.
	// Both identity paths converge on the same *Identity context key: JWT wins
	// when a Bearer header is present (OptionalAuth runs first, downstream
	// from the cookie path), CookieAuth fills in when no Bearer was sent.
	// BearerAuth downstream bypasses its static-key check once an identity is
	// populated so a cookie-only browser request succeeds even in a deployment
	// that also sets WALLFACER_SERVER_API_KEY for scripts.
	// Sign-in is always available (the login button), but only *forced* in
	// cloud/hosted mode. A local `wallfacer run` leaves the board reachable
	// anonymously; the user signs in only if they choose to.
	var srvHandler http.Handler = mux
	if cloudMode {
		srvHandler = h.ForceLogin(mux)
	}
	srvHandler = handler.BearerAuthMiddleware(envCfg.ServerAPIKey)(srvHandler)
	srvHandler = auth.OptionalAuth(jwtValidator, srvHandler)
	srvHandler = auth.CookieAuth(authClient, srvHandler)
	srvHandler = handler.CSRFMiddleware(actualHostPort)(srvHandler)
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

// resolveAuthConfig fills the AUTH_* config with public-client defaults so a
// plain `wallfacer run` signs in through the browser with no env setup. Any
// field already set (shell env or .env) is preserved. The default is the
// secret-less "wallfacer" public client; its session-cookie key is generated
// once and persisted under configDir. A loopback HTTP callback serves cookies
// insecurely (pkg/oidc then drops the __Host- prefix) since browsers reject
// Secure cookies over plain HTTP.
func resolveAuthConfig(cfg auth.Config, addr, configDir string) (auth.Config, error) {
	if cfg.AuthURL == "" {
		cfg.AuthURL = "https://auth.latere.ai"
	}
	if cfg.ClientID == "" {
		cfg.ClientID = "wallfacer"
	}
	if cfg.RedirectURL == "" {
		cfg.RedirectURL = defaultRedirectURL(addr)
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "email", "profile", "offline_access"}
	}
	// A public client has no secret to derive the cookie key from, so generate
	// and persist a local one. An explicit AUTH_COOKIE_KEY or AUTH_CLIENT_SECRET
	// (confidential client) takes precedence and skips this.
	if cfg.ClientSecret == "" && cfg.CookieKey == "" {
		key, err := loadOrCreateCookieKey(configDir)
		if err != nil {
			return cfg, fmt.Errorf("cookie key: %w", err)
		}
		cfg.CookieKey = key
	}
	if strings.HasPrefix(cfg.RedirectURL, "http://") {
		cfg.InsecureCookies = true
	}
	return cfg, nil
}

// defaultRedirectURL derives the OAuth callback URL from the listen address.
// A loopback or empty host yields http://localhost:<port>/callback (matching
// the redirect registered for the seeded "wallfacer" public client); any other
// host is assumed to terminate TLS and uses https.
func defaultRedirectURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "localhost", "8080"
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]", "127.0.0.1", "::1", "localhost":
		return fmt.Sprintf("http://localhost:%s/callback", port)
	default:
		return fmt.Sprintf("https://%s:%s/callback", host, port)
	}
}

// loadOrCreateCookieKey returns a stable hex-encoded 32-byte key for encrypting
// the session cookie of a public (secret-less) client, persisted at
// <configDir>/cookie-key so sessions survive a restart. Generated on first use.
func loadOrCreateCookieKey(configDir string) (string, error) {
	path := filepath.Join(configDir, "cookie-key")
	if b, err := os.ReadFile(path); err == nil {
		if k := strings.TrimSpace(string(b)); len(k) >= 32 {
			return k, nil
		}
	}
	raw := make([]byte, 32)
	if _, err := cryptorand.Read(raw); err != nil {
		return "", err
	}
	key := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return "", fmt.Errorf("persist cookie key: %w", err)
	}
	return key, nil
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
// vueDist and docsFS are the embedded filesystems containing the Vue SPA
// dist (frontend/dist) and docs/ directory tree respectively.
func RunServer(configDir string, args []string, vueDist, docsFS fs.FS) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)

	logFormat := fs.String("log-format", envOrDefault("LOG_FORMAT", "text"), `log output format: "text" or "json"`)
	addr := fs.String("addr", envOrDefault("ADDR", ":8080"), "listen address")
	dataDir := fs.String("data", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")), "data directory")
	envFile := fs.String("env-file", envOrDefault("ENV_FILE", filepath.Join(configDir, ".env")), "env file with credentials and runtime settings")
	noBrowser := fs.Bool("no-browser", false, "do not open browser on start")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: wallfacer run [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Start the task board server and open the web UI.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	requireClaudeOrExit(*envFile)

	sc := initServer(configDir, ServerConfig{
		LogFormat: *logFormat,
		Addr:      *addr,
		DataDir:   *dataDir,
		EnvFile:   *envFile,
	}, vueDist, docsFS)
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

// stripSSGContent disables vite-ssg hydration and injects a script that
// clears the pre-rendered HTML before Vue mounts. This prevents the
// hydration mismatch when the SSG-rendered page (ProductPage for cloud)
// differs from the client route (BoardPage for local).
func stripSSGContent(html string) string {
	html = strings.Replace(html, `data-server-rendered="true"`, "", 1)
	// Insert a classic (non-deferred) script right before the first module
	// script. It runs synchronously before Vue's deferred module, clearing
	// the stale SSG content so Vue does a fresh mount.
	const clearScript = `<script>var e=document.getElementById("app");if(e)e.textContent=""</script>`
	html = strings.Replace(html, `<script type="module"`, clearScript+`<script type="module"`, 1)
	return html
}

// mountVueSPA overlays Vue SPA routes onto an existing mux, overriding
// the legacy Go-templated UI for the root path and static assets. The
// API routes registered by BuildMux are preserved because the SPA handler
// only claims GET / and the /assets/ prefix, not /api/*.
func mountVueSPA(mux *http.ServeMux, vueDist fs.FS, serverAPIKey string, cloudMode bool) {
	dist, err := fs.Sub(vueDist, "frontend/dist")
	if err != nil {
		logger.Main.Warn("vue-ui: no frontend/dist embedded", "error", err)
		return
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		logger.Main.Warn("vue-ui: frontend/dist has no index.html; run 'cd frontend && bun run build'")
		return
	}

	mode := "local"
	if cloudMode {
		mode = "cloud"
	}
	apiKey := serverAPIKey
	version := Version

	rawHTML, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		logger.Main.Warn("vue-ui: failed to read index.html", "error", err)
		return
	}
	inject := fmt.Sprintf(
		`<script>window.__WALLFACER__={mode:%q,serverApiKey:%q,version:%q};</script>`,
		mode, apiKey, version,
	)
	indexHTML := strings.Replace(string(rawHTML), "</head>", inject+"</head>", 1)
	if !cloudMode {
		// Strip SSG pre-rendered content so Vue does a fresh client-side
		// render instead of hydrating the wrong page component.
		indexHTML = stripSSGContent(indexHTML)
	}

	serveVueIndex := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(indexHTML))
	}

	files := http.FS(dist)
	fileServer := http.FileServer(files)
	cachedFileServer := withAssetCache(fileServer)
	mux.HandleFunc("GET /", serveVueIndex)
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
	logger.Main.Info("ui: serving Vue SPA", "mode", mode)
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

// docAssetContentType maps a docs asset path to its image content type, or ""
// if the extension is not an embeddable doc image. The whitelist doubles as an
// access guard: only image files under docs/ are reachable via the asset route,
// never markdown or other embedded content.
func docAssetContentType(p string) string {
	switch {
	case strings.HasSuffix(p, ".png"):
		return "image/png"
	case strings.HasSuffix(p, ".jpg"), strings.HasSuffix(p, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(p, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(p, ".webp"):
		return "image/webp"
	case strings.HasSuffix(p, ".gif"):
		return "image/gif"
	default:
		return ""
	}
}

// BuildMux constructs the HTTP request router.
//
// All API routes are registered from apicontract.Routes (the single source of
// truth). The handlers map below pairs each route Name with its http.HandlerFunc,
// applying per-route middleware (e.g. UUID parsing via withID) at map
// construction time. A startup panic is triggered if a route in the contract
// has no corresponding handler entry, preventing silent drift.
func BuildMux(h *handler.Handler, reg *metrics.Registry, indexData IndexViewData, docsFS, vueDist fs.FS, cloudMode bool) *http.ServeMux {
	mux := http.NewServeMux()

	if vueDist != nil {
		mountVueSPA(mux, vueDist, indexData.ServerAPIKey, cloudMode)
	}

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

	// Docs assets — serve embedded images referenced by guide markdown, e.g.
	// ![](images/board.png). The client rewrites a doc-relative src to
	// /api/docs-asset/<category>/<path>; only whitelisted image extensions are
	// served, so this cannot leak markdown or other embedded files.
	mux.HandleFunc("GET /api/docs-asset/{path...}", func(w http.ResponseWriter, r *http.Request) {
		p := r.PathValue("path")
		if strings.Contains(p, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		ctype := docAssetContentType(p)
		if ctype == "" {
			http.Error(w, "unsupported asset type", http.StatusBadRequest)
			return
		}
		data, err := fs.ReadFile(docsFS, "docs/"+p)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if _, err := w.Write(data); err != nil {
			logger.Main.Debug("docs asset response write failed", "error", err)
		}
	})

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
	//
	// adminOnly wraps a handler so cloud deployments require the superadmin
	// claim; in local mode it is identity. Cloud mode is detected by whether
	// the Handler has an OIDC client wired (h.HasAuth()) — the same signal
	// used elsewhere to decide whether cloud surfaces render.
	adminOnly := func(next http.HandlerFunc) http.HandlerFunc {
		if !h.HasAuth() {
			return next // local mode: no claims path exists; pass through
		}
		wrapped := auth.RequireSuperadmin(next)
		return wrapped.ServeHTTP
	}

	handlers := map[string]http.HandlerFunc{
		// Admin operations.
		"RebuildIndex": adminOnly(h.RebuildIndex),

		// Debug & monitoring.
		"Health":            h.Health,
		"GetSpanStats":      h.GetSpanStats,
		"GetRuntimeStatus":  h.GetRuntimeStatus,
		"BoardManifest":     h.BoardManifest,
		"TaskBoardManifest": withID(h.TaskBoardManifest),

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
		"GetSpecTree":    h.GetSpecTree,
		"SpecTreeStream": h.SpecTreeStream,
		"SpecTransition": h.SpecTransition,

		// Ideation agent.
		"GetIdeationStatus": h.GetIdeationStatus,
		"TriggerIdeation":   h.TriggerIdeation,
		"CancelIdeation":    h.CancelIdeation,

		// Agents catalog (read + user-authored CRUD).
		"ListAgents":  h.ListAgents,
		"GetAgent":    h.GetAgent,
		"CreateAgent": h.CreateAgent,
		"UpdateAgent": h.UpdateAgent,
		"DeleteAgent": h.DeleteAgent,

		// Flows catalog (read + user-authored CRUD).
		"ListFlows":  h.ListFlows,
		"GetFlow":    h.GetFlow,
		"CreateFlow": h.CreateFlow,
		"UpdateFlow": h.UpdateFlow,
		"DeleteFlow": h.DeleteFlow,

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
		"UpdateTaskPromptTool":     h.UpdateTaskPromptTool,

		// Planning chat threads.
		"ListPlanningThreads":  h.ListPlanningThreads,
		"CreatePlanningThread": h.CreatePlanningThread,
		"PatchPlanningThread":  h.PatchPlanningThread,

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
		"ExplorerTree":        h.ExplorerTree,
		"ExplorerStream":      h.ExplorerStream,
		"ExplorerReadFile":    h.ExplorerReadFile,
		"ExplorerWriteFile":   h.ExplorerWriteFile,
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
		"ResumeTask":     withID(h.ResumeTask),
		"SyncTask":       withID(h.SyncTask),
		"TestTask":       withID(h.TestTask),

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

		// Task span / oversight analytics.
		"GetTaskSpans": withID(h.GetTaskSpans),
		"GetOversight": withID(h.GetOversight),

		// OAuth authentication
		"StartOAuth":  http.HandlerFunc(h.StartOAuth),
		"OAuthStatus": http.HandlerFunc(h.OAuthStatus),
		"CancelOAuth": http.HandlerFunc(h.CancelOAuth),

		// Latere.ai sign-in (cloud mode). Always mounted; handlers self-gate
		// to 503/204 when the auth provider is not configured.
		"Login":        http.HandlerFunc(h.Login),
		"Callback":     http.HandlerFunc(h.Callback),
		"Logout":       http.HandlerFunc(h.Logout),
		"LogoutNotify": http.HandlerFunc(h.LogoutNotify),
		"AuthMe":       http.HandlerFunc(h.AuthMe),
		"AuthOrgs":     http.HandlerFunc(h.AuthOrgs),
		"PatchAuthMe":  http.HandlerFunc(h.PatchAuthMe),
		"SwitchOrg":    http.HandlerFunc(h.SwitchOrg),

		// Local-mode RFC 8628 device-code sign-in. The local SPA's prompt
		// drives the flow over these three routes; the resulting token is
		// stored at <UserConfigDir>/latere/token.json so it is shared with
		// the `latere` CLI and the `wallfacer auth login` terminal command.
		// h.DeviceAuth nil-safe: Mount falls back to 503 stubs.
		"AuthDeviceStart":  http.HandlerFunc(h.AuthDeviceStart),
		"AuthDevicePoll":   http.HandlerFunc(h.AuthDevicePoll),
		"AuthDeviceCancel": http.HandlerFunc(h.AuthDeviceCancel),
	}

	// bodyLimits restricts request body size for write endpoints. Routes
	// not listed here have no MaxBytesReader applied (e.g. GET, SSE, WebSocket).
	bodyLimits := map[string]int64{
		// Server configuration.
		"UpdateConfig": handler.BodyLimitDefault,

		// Spec tree.
		"SpecTransition": handler.BodyLimitDefault,

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
		"CreatePlanningThread": handler.BodyLimitDefault,
		"PatchPlanningThread":  handler.BodyLimitDefault,

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

	// Sandbox trust-plane proxy. Not in apicontract because these
	// are server-to-server calls the sandbox credential sidecar
	// makes, not part of the browser client contract. Handlers 503
	// when SandboxProxyConfig.Enabled is false (local / no
	// credentials configured), so it's safe to wire them
	// unconditionally.
	//
	// The JWT validator is built from SANDBOX_PROXY_AUTH_URL (= the
	// auth service's base URL). If unset, validation is skipped and
	// the trust-plane endpoints rely solely on the Enabled flag —
	// acceptable in single-tenant local runs.
	var sandboxProxyValidator *auth.Validator
	if u := os.Getenv("SANDBOX_PROXY_AUTH_URL"); u != "" {
		sandboxProxyValidator = auth.BuildValidator(
			auth.Config{AuthURL: u},
			os.Getenv("SANDBOX_PROXY_AUTH_JWKS_URL"),
			os.Getenv("SANDBOX_PROXY_AUTH_ISSUER"),
		)
	}
	sandboxProxy := handler.NewSandboxProxy(
		handler.LoadSandboxProxyConfig(), sandboxProxyValidator)
	mux.HandleFunc("POST /internal/sandbox-proxy/llm/anthropic/", sandboxProxy.LLMAnthropic)
	mux.HandleFunc("POST /internal/sandbox-proxy/llm/openai/", sandboxProxy.LLMOpenAI)
	mux.HandleFunc("GET /internal/sandbox-proxy/github-token", sandboxProxy.GitHubToken)

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
		// Unmatched requests (404, e.g. write methods to unknown paths) have an
		// empty r.Pattern; collapse them into a single sentinel series instead of
		// the raw URL path, which would give unbounded metric label cardinality.
		route := r.Pattern
		if route == "" {
			route = "<unmatched>"
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
