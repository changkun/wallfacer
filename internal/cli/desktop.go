//go:build desktop

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"changkun.de/x/wallfacer/internal/logger"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// RunDesktop launches the native desktop window backed by the wallfacer HTTP server.
// The HTTP server starts in a background goroutine and a Wails WebView
// proxies all requests to it. A system tray icon provides
// "Open Dashboard" and "Quit" actions.
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
		SkipCSRF:     true, // Desktop WebView origin doesn't match localhost.
	}, uiFS, docsFS)

	// Start the HTTP server in the background.
	go func() {
		if err := sc.Srv.Serve(sc.Ln); err != nil && err != http.ErrServerClosed {
			logger.Main.Error("http server", "error", err)
		}
	}()

	logger.Main.Info("desktop server listening", "addr", sc.Ln.Addr().String())

	serverURL := fmt.Sprintf("http://localhost:%d", sc.ActualPort)
	serverHost := fmt.Sprintf("localhost:%d", sc.ActualPort)

	// wailsCtx is set by OnStartup and used by the tray to show/focus the window.
	var wailsCtx context.Context

	// Set up the system tray before wails.Run so it is ready when the app starts.
	tm := NewTrayManager(
		func() {
			if wailsCtx != nil {
				wailsRuntime.WindowShow(wailsCtx)
			}
		},
		func() {
			if wailsCtx != nil {
				wailsRuntime.Quit(wailsCtx)
			}
		},
		serverURL,
		sc.ServerAPIKey,
	)
	tm.Start()

	hideOnClose := runtime.GOOS == "darwin" || runtime.GOOS == "windows"

	// Proxy handler that forwards both HTTP and WebSocket requests to the
	// real server. The standard httputil.ReverseProxy handles HTTP; for
	// WebSocket upgrades we tunnel the raw TCP connection.
	target, _ := url.Parse(serverURL)
	httpProxy := httputil.NewSingleHostReverseProxy(target)
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketUpgrade(r) {
			proxyWebSocket(w, r, serverHost)
			return
		}
		httpProxy.ServeHTTP(w, r)
	})

	appOpts := &options.App{
		Title:             "Wallfacer",
		Width:             1400,
		Height:            900,
		HideWindowOnClose: hideOnClose,
		AssetServer: &assetserver.Options{
			Handler: proxyHandler,
		},
		OnStartup: func(ctx context.Context) {
			wailsCtx = ctx
			logger.Main.Info("desktop window opened")
		},
		OnShutdown: func(_ context.Context) {
			tm.Stop()
			sc.Shutdown()
		},
	}

	if runtime.GOOS == "darwin" {
		appOpts.Mac = &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		}
		appOpts.CSSDragProperty = "--wails-draggable"
		appOpts.CSSDragValue = "drag"
	}
	if runtime.GOOS == "windows" {
		appOpts.Frameless = true
		appOpts.CSSDragProperty = "--wails-draggable"
		appOpts.CSSDragValue = "drag"
	}

	return wails.Run(appOpts)
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Connection"), "Upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// proxyWebSocket tunnels a WebSocket connection by hijacking the client
// connection and opening a raw TCP connection to the backend server.
func proxyWebSocket(w http.ResponseWriter, r *http.Request, targetHost string) {
	// Connect to the backend server.
	backendConn, err := net.Dial("tcp", targetHost)
	if err != nil {
		http.Error(w, "websocket proxy: dial failed", http.StatusBadGateway)
		return
	}
	defer backendConn.Close()

	// Hijack the client connection.
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket proxy: hijack not supported", http.StatusInternalServerError)
		return
	}
	clientConn, clientBuf, err := hj.Hijack()
	if err != nil {
		http.Error(w, "websocket proxy: hijack failed", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Forward the original HTTP upgrade request to the backend.
	if err := r.Write(backendConn); err != nil {
		return
	}

	// Flush any buffered data from the hijacked reader.
	if clientBuf.Reader.Buffered() > 0 {
		buffered := make([]byte, clientBuf.Reader.Buffered())
		if _, err := clientBuf.Read(buffered); err == nil {
			backendConn.Write(buffered)
		}
	}

	// Bidirectional copy.
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(clientConn, backendConn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(backendConn, clientConn)
		done <- struct{}{}
	}()
	<-done
}
