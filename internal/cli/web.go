package cli

import (
	"context"
	"encoding/json"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"latere.ai/x/pkg/otel"

	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/webserver"
)

// RunWeb starts the wallfacer web frontend server with OIDC authentication.
func RunWeb(args []string, frontendFS fs.FS) {
	if err := runWeb(args, frontendFS); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func runWeb(args []string, frontendFS fs.FS) error {
	logger, shutdown, logsErr := otel.Bootstrap(context.Background(), otel.Config{ServiceName: "wallfacer-web"})
	if logsErr != nil {
		logger.Warn("otlp logs init failed; continuing on stdout", "err", logsErr)
	}
	defer func() { _ = shutdown(context.Background()) }()

	fs := flag.NewFlagSet("web", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	_ = fs.Parse(args)

	if env := os.Getenv("WALLFACERD_ADDR"); env != "" {
		*addr = env
	}

	authCfg := auth.Config{
		AuthURL:      os.Getenv("AUTH_URL"),
		ClientID:     os.Getenv("AUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("AUTH_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("AUTH_REDIRECT_URL"),
		CookieKey:    os.Getenv("AUTH_COOKIE_KEY"),
	}
	if authCfg.AuthURL == "" {
		authCfg.AuthURL = "https://auth.latere.ai"
	}
	authClient := auth.New(authCfg)

	// JWT validator for the coordination WebSocket: a local instance dials with
	// Authorization: Bearer <jwt>, validated on the same internal/auth path as
	// every API request. Issuer/JWKS fall back to AuthURL derivatives.
	jwtValidator := auth.BuildValidator(authCfg, os.Getenv("AUTH_JWKS_URL"), os.Getenv("AUTH_ISSUER"))

	mux := http.NewServeMux()

	// Coordination plane (accept side): terminate the one outbound WebSocket
	// each signed-in, opted-in local instance dials. The registry it feeds is
	// in-memory and ephemeral (the single-replica view); capability code reads
	// it. Behind Auth so the principal is the validated JWT, never the manifest.
	coordReg := coordinator.NewRegistry()
	coord := coordinator.NewCoordinator(coordReg)
	// Spec comments are cloud-authoritative (the one relay-not-mirror exception).
	// The durable store is Postgres when WALLFACER_DATABASE_URL is set; otherwise
	// an in-memory store (single-replica dev). Either way the capability mints
	// ids, stamps the principal, and fans out per org+repo.
	commentStore := newCommentStore(context.Background())
	coord.SetCommentService(coordinator.NewCommentService(commentStore, coordReg))
	mux.Handle("GET /api/coordination/ws", auth.Auth(jwtValidator, http.HandlerFunc(coord.HandleWS)))

	// Same-origin RUM ingest for the SPA; forwards browser OTLP to the
	// in-cluster collector. POST-scoped to avoid a ServeMux conflict with the
	// SPA's GET / fallback.
	mux.Handle("POST /v1/telemetry/", otel.TelemetryProxy("/v1/telemetry"))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	if authClient != nil {
		mux.HandleFunc("GET /login", authClient.HandleLogin)
		mux.HandleFunc("GET /callback", authClient.HandleCallback)
		mux.HandleFunc("GET /logout", authClient.HandleLogout)
		mux.HandleFunc("GET /logout/notify", authClient.HandleLogoutNotify)
		mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
			user := authClient.UserFromRequest(w, r)
			if user == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"not authenticated"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			resp := struct {
				*auth.User
				AuthURL string `json:"auth_url,omitempty"`
			}{User: user, AuthURL: authClient.AuthURL()}
			_ = json.NewEncoder(w).Encode(resp)
		})
		slog.Info("auth: OIDC enabled", "auth_url", authCfg.AuthURL)
	} else {
		slog.Info("auth: disabled (no AUTH_CLIENT_ID)")
	}

	webserver.MountSPA(mux, frontendFS)
	webserver.SPAFallback(mux, frontendFS)

	// otel.Handler wraps the mux with server-request tracing/metrics and the
	// X-Trace-Id response header.
	srv := &http.Server{Addr: *addr, Handler: otel.Handler(mux, "wallfacer-web")}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("wallfacer web started", "addr", *addr)
	return otel.RunServer(ctx, srv, 10*time.Second, nil)
}
