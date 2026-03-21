# Streamlined First-Run Experience

## Status Update (2026-03-21)

The original spec was written when the getting-started flow required cloning the repo, building from source, manually building sandbox images, and editing `.env` files by hand. Most of those pain points have since been resolved:

| Original Problem | Status | What Changed |
|---|---|---|
| No pre-built binaries | **Solved** | `.github/workflows/release-binary.yml` publishes binaries for linux/darwin × amd64/arm64 on each version tag |
| Docs say `make build` | **Solved** | Getting-started docs now show three install paths (binary download, `go install`, build from source); `make build` is contributor-only |
| Must clone the repo | **Solved** | Binary download, install script, and `go install` all work without cloning |
| Credential setup is 4 steps | **Solved** | Docs point users to Settings → API Configuration in the UI; no restart needed |
| Sandbox image auto-pull | **Already existed** | `ensureImage()` in `internal/cli/server.go` pulls from ghcr.io on first run |

**Current first-run flow (as documented):**
```
Step 1: Get credential (OAuth token or API key)
Step 2: curl ... install.sh | sh  (or go install / manual download / build from source)
Step 3: wallfacer run ~/project
Step 4: Configure credential in browser UI
Step 5: Create first task
```

This is already a reasonable experience. The remaining friction points are different from what the original spec targeted.

## Remaining Friction

### 1. `go install` silently fails due to `//go:embed`

`main.go` embeds `ui/` and `docs/` directories:
```go
//go:embed ui
var uiFiles embed.FS

//go:embed docs
var docsFiles embed.FS
```

When `go install changkun.de/x/wallfacer@latest` fetches the module from the proxy, these directories are included in the module zip, so the install **should work** (Go's module system includes non-Go files in embedded directories). However, this depends on the module being properly published and the proxy having the correct version cached. If it fails, the error message ("pattern ui: no matching files found") is confusing. This needs verification with a real published version.

**Recommendation:** Test `go install` from a clean environment after the next release. If it works, no action needed. If the embed fails via module proxy, consider either (a) removing the embed in favor of a separate asset download, or (b) documenting the limitation clearly.

### ~~2. No install script for binary download~~ — Solved

`install.sh` was added at the repo root. It detects OS/arch, downloads the correct binary from GitHub Releases, and installs to `/usr/local/bin` or `~/.local/bin`. Supports `WALLFACER_INSTALL_DIR` and `WALLFACER_VERSION` overrides. Documented as the primary install method in README and `docs/guide/getting-started.md`.

```bash
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh
```

### ~~3. Container runtime prerequisite discovery~~ — Partially solved

`wallfacer doctor` now checks whether the container runtime is installed and responsive, with actionable messages (e.g. "podman machine start" or "Ensure Docker Desktop is running"). Remaining: the server startup error when no runtime is found could still be more user-friendly.

### ~~4. First-task sandbox image pull is opaque~~ — Solved

The server now reports `image_cached` in the `GET /api/config` response. When the sandbox image is not cached locally, the UI shows a dismissible warning banner above the board: "Sandbox image not cached yet. It will be pulled automatically (~1 GB) when you run your first task."

### 5. Go 1.25+ is bleeding-edge

Go 1.25 is very new. Users who want to build from source or use `go install` likely have Go 1.23 or 1.24 installed. The binary download path avoids this entirely, but users who reach for `go install` first will hit a confusing version error.

**Recommendation:** This is self-resolving as Go 1.25 becomes mainstream. In the meantime, the docs already list binary download as the primary path. No action needed beyond keeping the version requirement visible.

## Revised Priority

Given what's already been implemented, the remaining work in priority order:

1. ~~**Install script**~~ — **Done.** `install.sh` added; documented as primary install method.

2. ~~**`wallfacer doctor` command**~~ — **Done.** Merged with `env` subcommand. Checks config paths, credentials (masked), container runtime responsiveness, sandbox images, and Git. `wallfacer env` is kept as an alias.

3. **Verify `go install` from module proxy** (Option 1 above) — Trivial effort, just needs testing after next release.

4. ~~**Surface image pull progress in UI**~~ — **Done.** `GET /api/config` returns `image_cached`; UI shows a dismissible warning banner when false.

## Ideal First-Run Experience

```bash
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh
wallfacer doctor
wallfacer run ~/project
# UI warns if image not cached; configure credential in browser → create task
```

The only remaining item is verifying `go install` from the module proxy after the next release.
