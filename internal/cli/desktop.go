//go:build desktop

package cli

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"

	"changkun.de/x/wallfacer/internal/logger"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// RunDesktop launches the native desktop window backed by the wallfacer HTTP server.
// The HTTP server starts in a background goroutine and a Wails WebView
// window proxies all requests to it.
func RunDesktop(configDir string, args []string, uiFS, docsFS fs.FS) error {
	fs := flag.NewFlagSet("desktop", flag.ExitOnError)

	logFormat := fs.String("log-format", envOrDefault("LOG_FORMAT", "text"), `log output format: "text" or "json"`)
	addr := fs.String("addr", envOrDefault("ADDR", ":0"), "listen address (default: random port)")
	dataDir := fs.String("data", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")), "data directory")
	containerCmd := fs.String("container", envOrDefault("CONTAINER_CMD", detectContainerRuntime()), "container runtime command (podman or docker)")
	sandboxImage := fs.String("image", envOrDefault("SANDBOX_IMAGE", defaultSandboxImage()), "sandbox container image")
	envFile := fs.String("env-file", envOrDefault("ENV_FILE", filepath.Join(configDir, ".env")), "env file for container (Claude token)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: wallfacer desktop [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Launch the native desktop application.\n\n")
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
	}, uiFS, docsFS)

	// Start the HTTP server in the background.
	go func() {
		if err := sc.Srv.Serve(sc.Ln); err != nil && err != http.ErrServerClosed {
			logger.Main.Error("http server", "error", err)
		}
	}()

	logger.Main.Info("desktop server listening", "addr", sc.Ln.Addr().String())

	// Reverse proxy all WebView requests to the running HTTP server.
	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", sc.ActualPort),
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	err := wails.Run(&options.App{
		Title:  "Wallfacer",
		Width:  1400,
		Height: 900,
		AssetServer: &assetserver.Options{
			Handler: proxy,
		},
		OnStartup: func(_ context.Context) {
			logger.Main.Info("desktop window opened")
		},
		OnShutdown: func(_ context.Context) {
			sc.Shutdown()
		},
	})

	return err
}
