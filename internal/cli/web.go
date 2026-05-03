package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"changkun.de/x/wallfacer/internal/webserver"
	"latere.ai/x/pkg/oidc"
)

func RunWeb(args []string) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	fs := flag.NewFlagSet("web", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	_ = fs.Parse(args)

	if env := os.Getenv("WALLFACERD_ADDR"); env != "" {
		*addr = env
	}

	authClient := oidc.New(oidc.LoadConfig())

	mux := http.NewServeMux()

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
		mux.HandleFunc("GET /logout/notify", func(w http.ResponseWriter, _ *http.Request) {
			oidc.ClearSession(w)
			w.WriteHeader(http.StatusOK)
		})
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
				*oidc.User
				AuthURL string `json:"auth_url,omitempty"`
			}{User: user, AuthURL: authClient.AuthURL()}
			_ = json.NewEncoder(w).Encode(resp)
		})
	}

	webserver.MountSPA(mux)
	webserver.SPAFallback(mux)

	srv := &http.Server{Addr: *addr, Handler: mux}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wallfacer web: listen %s: %v\n", *addr, err)
		os.Exit(1)
	}

	slog.Info("wallfacer web started", "addr", ln.Addr().String())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
