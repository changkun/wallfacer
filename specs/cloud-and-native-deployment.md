# Cloud & Native Deployment Options

**Date:** 2026-02-21

## Core Constraints

The app has three hard runtime dependencies that shape all deployment options:

1. **Container runtime** (`podman`/`docker` via `os/exec`) — required for every task execution
2. **Git** on the host — worktrees, rebase, merge all run on the host
3. **Workspace directories** must exist on the machine running the Go server
4. **No authentication** — the HTTP server is open to anyone who can reach port 8080

---

## Option A: Cloud Deployment

### A1. VPS + Reverse Proxy (lowest effort, works today)

Deploy the Go binary to any Linux VM (EC2, Hetzner, DigitalOcean, etc.) with Docker/Podman installed.

**What already works:**
- `-no-browser` flag exists
- `CONTAINER_CMD` env var is configurable
- Filesystem storage works fine on a persistent disk

**What is missing:**

| Gap | Fix |
|-----|-----|
| Authentication — server is fully open | Add HTTP basic auth middleware (~50 lines in Go), or put Caddy/Nginx in front with `basicauth` |
| HTTPS | Caddy with `tls` block handles it automatically |
| Workspace repos must be on the VM | `git clone` or rsync repos to the VM at setup |
| Container runtime | Install Docker or Podman on the VM |
| Persistent storage | Mount a volume at `~/.wallfacer/` |
| Survives reboots | Write a systemd unit file |

This is deployable with about a day of infrastructure work. The biggest practical friction is that workspace repos need to exist on the remote machine. Adding auth is a security requirement before exposing to the internet.

**Architecture:**
```
Internet → Caddy (HTTPS + basicauth) → wallfacer :8080
                                             ↓
                                        Podman (local tasks)
                                             ↓
                               /home/user/repos/<workspace>
```

**Systemd unit example:**
```ini
[Unit]
Description=Wallfacer
After=network.target

[Service]
User=wallfacer
ExecStart=/usr/local/bin/wallfacer run -no-browser /home/wallfacer/repos/myproject
Restart=on-failure
Environment=CONTAINER_CMD=docker

[Install]
WantedBy=multi-user.target
```

**Caddy example:**
```
wallfacer.example.com {
    basicauth {
        user $2a$14$...  # bcrypt hash
    }
    reverse_proxy localhost:8080
}
```

---

### A2. Docker-in-Docker (containerize the server itself)

Run the wallfacer Go server inside a container, which then needs to spawn task containers.

**Problem:** The server uses `os/exec` to call `podman run`. Inside a container this requires one of:
- Mounting the Docker socket (`-v /var/run/docker.sock:/var/run/docker.sock`) — gives the container root-equivalent access to the host; a deliberate security trade-off
- Docker-in-Docker (DinD) with `--privileged` — fragile, not recommended in production
- Podman rootless inside a container — complex, kernel version dependent

**When to choose this:** Only if a platform (Railway, Render, Fly.io) requires the server to be containerized. The socket-mount approach works but must be a conscious security decision.

---

### A3. Kubernetes with Job API (cloud-native, major refactoring)

Replace `os/exec` container spawning with the Kubernetes `batch/v1 Job` API. Tasks become K8s Jobs that mount PersistentVolumeClaims for worktrees.

**Required changes:**

| Component | Current | Cloud-native replacement |
|-----------|---------|--------------------------|
| Task execution | `podman run` via `os/exec` | `client-go` creating K8s Jobs |
| Persistence | `~/.wallfacer/data/` filesystem | PostgreSQL or similar |
| Worktrees | Local git worktrees | Per-task PVCs or init containers |
| Log streaming | Container stdout via `os/exec` pipe | `k8s.io/client-go` pod log stream |
| State | In-memory `sync.RWMutex` map | DB-backed, enables replicas |

**Verdict:** Multi-week refactor. Worth it for multi-user, horizontal scaling, or enterprise deployment. For personal/team use, A1 is far more practical.

---

## Option B: Native Desktop App

### B1. System Tray Wrapper (minimal changes, works today)

The binary already calls `openBrowser()` on startup. The gap is it requires a terminal to keep running. A system tray wrapper makes it behave like a proper desktop app: click icon → server starts → browser opens → tray menu appears.

**Tray menu actions:** Open Dashboard, Quit

**What needs to change:**
- Add a systray library (e.g. `github.com/getlantern/systray` or `fyne.io/systray`)
- Move server startup out of the terminal into a background goroutine
- Supply a tray icon (PNG/ICO, embedded with `//go:embed`)
- Fix Windows: `openBrowser()` currently does nothing on `windows` — add `exec.Command("cmd", "/c", "start", url)`
- macOS: build as `.app` bundle with `Info.plist` and an icon

**Integration sketch:**
```go
func main() {
    systray.Run(func() {
        systray.SetIcon(iconData)
        systray.SetTitle("Wallfacer")
        mOpen := systray.AddMenuItem("Open Dashboard", "")
        mQuit := systray.AddMenuItem("Quit", "")

        go runServer(configDir, os.Args[1:])  // existing logic

        go func() {
            for {
                select {
                case <-mOpen.ClickedCh:
                    openBrowser("http://localhost:8080")
                case <-mQuit.ClickedCh:
                    systray.Quit()
                    os.Exit(0)
                }
            }
        }()
    }, func() {})
}
```

**Effort:** Low — a few hundred lines of new code, no architectural changes. Existing server, runner, store untouched.

**Limitation:** The UI is still in a browser window; the "app" is the background server.

---

### B2. Wails — True Native App (medium effort, best UX)

[Wails](https://wails.io) packages a Go backend + web frontend into a native desktop binary using the OS's native WebView (WKWebView on macOS, WebView2 on Windows, WebKitGTK on Linux). No Electron, no bundled Chromium, small binary.

**Why this fits well:**
- Backend is already Go — no rewrite
- Frontend is already vanilla HTML/JS — Wails renders it in a WebView
- Existing HTTP handlers stay as-is; the WebView connects to `localhost:8080`
- Output: `Wallfacer.app` on macOS, `Wallfacer.exe` on Windows, binary on Linux

**What needs to change:**

| Area | Change |
|------|--------|
| `main.go` | Wrap `runServer` inside `wails.Run()` app lifecycle |
| Browser launch | Remove `openBrowser()` — Wails window replaces it |
| `net.Listen` | Keep port binding; Wails WebView points at it |
| Data dir default | Optionally use `wails.App.DataPath()` for OS path conventions |
| First-run setup | Wails dialogs for token entry (optional quality-of-life) |
| Build toolchain | `wails build` replaces `go build` |

**Wails does NOT replace:**
- Container runtime — user still needs Docker Desktop or Podman installed
- Git — still required on the host

**Wails app skeleton:**
```go
// main.go
func main() {
    app := NewApp()  // wraps runServer
    err := wails.Run(&options.App{
        Title:     "Wallfacer",
        Width:     1400,
        Height:    900,
        AssetServer: &assetserver.Options{
            Assets: embeddedAssets,  // ui/ directory
        },
        OnStartup: app.startup,     // calls runServer
        Bind:      []interface{}{app},
    })
}
```

**Effort:** Medium. The architectural fit is very good — the main work is the Wails integration layer and packaging (icons, code signing for distribution).

---

### B3. Electron (not recommended)

Would run the Go binary as a child process. Adds ~150 MB for bundled Chromium, introduces a Node.js runtime, more complex build pipeline. No meaningful capability gain over Wails for this use case.

---

## Decision Matrix

| Approach | Effort | Container Runtime | Auth | Multi-user | Cross-platform |
|----------|--------|-------------------|------|------------|----------------|
| **A1: VPS + Caddy** | Low | Install on VM | Add middleware or Caddy | No | N/A (Linux VM) |
| **A2: Docker-in-Docker** | Medium | DinD / socket mount | Available via proxy | No | N/A |
| **A3: K8s + Job API** | High | K8s runtime | K8s RBAC | Yes | N/A |
| **B1: System tray** | Low | Still required locally | N/A | Single-user | macOS / Linux / Win |
| **B2: Wails native app** | Medium | Still required locally | N/A | Single-user | macOS / Linux / Win |

---

## Recommended Path

**For cloud:** Start with **A1 (VPS + Caddy)**. The only code change is an auth middleware or Caddy `basicauth` directive. Everything else is infra config. Migrate to A3 only if multi-user or horizontal scaling becomes a real need.

**For native desktop:** **B2 (Wails)** gives the best end-user experience — no browser tab, proper window, dock icon, OS-native feel. The existing Go + vanilla JS architecture is a near-perfect fit. **B1 (system tray)** is a lower-risk stepping stone and can ship first.

**Container runtime is unavoidable in both paths.** Users need Docker Desktop or Podman installed. That is a fundamental requirement baked into the current architecture, not a deployment detail.
