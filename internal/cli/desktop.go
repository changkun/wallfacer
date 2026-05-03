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
	"runtime"
	"strings"
	"time"

	"changkun.de/x/wallfacer/assets/icons"
	"changkun.de/x/wallfacer/internal/logger"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// RunDesktop launches the native desktop window backed by the wallfacer HTTP server.
func RunDesktop(configDir string, args []string, uiFS, vueDist, docsFS fs.FS) error {
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
		SkipCSRF:     true,
	}, uiFS, vueDist, docsFS)

	go func() {
		if err := sc.Srv.Serve(sc.Ln); err != nil && err != http.ErrServerClosed {
			logger.Main.Error("http server", "error", err)
		}
	}()

	logger.Main.Info("desktop server listening", "addr", sc.Ln.Addr().String())

	serverURL := fmt.Sprintf("http://localhost:%d", sc.ActualPort)

	var wailsCtx context.Context

	// doShutdown shows a fullscreen overlay in the WebView, performs graceful
	// shutdown (HTTP drain → runner goroutine drain), and terminates the process.
	// Called from the tray "Quit" button or Cmd+Q menu item.
	doShutdown := func() {
		if wailsCtx != nil {
			wailsRuntime.WindowShow(wailsCtx)
			showShutdownOverlay(wailsCtx, "Stopping server…")
		}

		sc.Stop()
		if wailsCtx != nil {
			updateShutdownStatus(wailsCtx, "Draining HTTP connections…")
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := sc.Srv.Shutdown(shutdownCtx); err != nil {
			logger.Main.Error("http server shutdown", "error", err)
		}
		cancel()

		// Wait for all background runner goroutines (in-progress tasks, title
		// generation, oversight) to finish. Update the overlay text every second
		// so the user knows what's still draining.
		done := make(chan struct{})
		go func() { sc.Runner.Shutdown(); close(done) }()

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				logger.Main.Info("shutdown complete")
				os.Exit(0)
			case <-ticker.C:
				if wailsCtx == nil {
					continue
				}
				if p := sc.Runner.PendingGoroutines(); len(p) > 0 {
					updateShutdownStatus(wailsCtx,
						fmt.Sprintf("Waiting for %d tasks: %s",
							len(p), strings.Join(p, ", ")))
				}
			}
		}
	}

	var tm *TrayManager
	tm = NewTrayManager(
		func() {
			if wailsCtx != nil {
				wailsRuntime.WindowShow(wailsCtx)
			}
		},
		func() { go doShutdown() },
		func() {
			if wailsCtx != nil {
				wailsRuntime.WindowExecJS(wailsCtx, "if(typeof fetchConfig==='function')fetchConfig()")
			}
		},
		serverURL,
		sc.ServerAPIKey,
	)
	tm.Start()

	// On macOS and Windows, closing the window hides it to the tray rather
	// than quitting the app (standard platform convention). Linux closes.
	hideOnClose := runtime.GOOS == "darwin" || runtime.GOOS == "windows"

	// All WebView requests are reverse-proxied to the local HTTP server.
	// This lets Wails serve the UI without duplicating routing logic.
	target, _ := url.Parse(serverURL)
	proxy := httputil.NewSingleHostReverseProxy(target)

	// App menu: override Cmd+W to hide the window (not quit), and
	// Cmd+Q to route through doShutdown so the user sees the shutdown overlay.
	appMenu := menu.NewMenu()
	appMenu.Append(menu.AppMenu())  // standard macOS app menu (About, Services, …)
	appMenu.Append(menu.EditMenu()) // standard Edit menu (Copy, Paste, …)
	fileMenu := appMenu.AddSubmenu("File")
	fileMenu.AddText("Close Window", keys.CmdOrCtrl("w"), func(_ *menu.CallbackData) {
		if wailsCtx != nil {
			wailsRuntime.WindowHide(wailsCtx)
		}
	})
	fileMenu.AddSeparator()
	fileMenu.AddText("Quit Wallfacer", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
		go doShutdown()
	})

	appOpts := &options.App{
		Title:             "Wallfacer",
		Width:             1400,
		Height:            900,
		HideWindowOnClose: hideOnClose,
		Menu:              appMenu,
		AssetServer: &assetserver.Options{
			Handler: proxy,
		},
		OnStartup: func(ctx context.Context) {
			wailsCtx = ctx
			installDockReopenHandler(func() {
				wailsRuntime.WindowShow(ctx)
			})
			logger.Main.Info("desktop window opened")
		},
		OnShutdown: func(_ context.Context) {
			// Best-effort cleanup if the app exits without going through
			// doShutdown (e.g. SIGKILL). Non-blocking: os.Exit from
			// doShutdown will terminate anyway.
			tm.Stop()
		},
	}

	if runtime.GOOS == "darwin" {
		appOpts.Mac = &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "Wallfacer",
				Message: "Task-board runner for AI agents",
				Icon:    icons.AppIcon,
			},
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

// showShutdownOverlay injects a fullscreen overlay into the WebView.
func showShutdownOverlay(ctx context.Context, msg string) {
	js := fmt.Sprintf(`(function(){
  if(document.getElementById('wf-shutdown-overlay'))return;
  var d=document.createElement('div');
  d.id='wf-shutdown-overlay';
  d.style.cssText='position:fixed;inset:0;z-index:99999;display:flex;flex-direction:column;align-items:center;justify-content:center;background:rgba(15,23,42,0.92);color:#e2e8f0;font-family:Inter,system-ui,sans-serif;-webkit-app-region:drag';
  d.innerHTML='<div style="font-size:18px;font-weight:600;margin-bottom:12px">Shutting down…</div>'
    +'<div id="wf-shutdown-status" style="font-size:13px;color:#94a3b8;max-width:420px;text-align:center;line-height:1.5">%s</div>'
    +'<div style="margin-top:20px;width:32px;height:32px;border:3px solid #334155;border-top-color:#60a5fa;border-radius:50%%;animation:wf-spin 0.8s linear infinite"></div>';
  var s=document.createElement('style');
  s.textContent='@keyframes wf-spin{to{transform:rotate(360deg)}}';
  document.head.appendChild(s);
  document.body.appendChild(d);
})()`, escapeJS(msg))
	wailsRuntime.WindowExecJS(ctx, js)
}

// updateShutdownStatus updates the status text in the shutdown overlay.
func updateShutdownStatus(ctx context.Context, msg string) {
	js := fmt.Sprintf(`(function(){
  var el=document.getElementById('wf-shutdown-status');
  if(el)el.textContent=%q;
})()`, msg)
	wailsRuntime.WindowExecJS(ctx, js)
}

// escapeJS escapes a string for safe embedding in a JS string literal.
func escapeJS(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return r.Replace(s)
}
