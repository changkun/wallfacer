# Streamlined First-Run Experience

## Status Update (2026-03-21)

The original spec was written when the getting-started flow required cloning the repo, building from source, manually building sandbox images, and editing `.env` files by hand. Most of those pain points have since been resolved:

| Original Problem | Status | What Changed |
|---|---|---|
| No pre-built binaries | **Solved** | `.github/workflows/release-binary.yml` publishes binaries for linux/darwin × amd64/arm64 on each version tag |
| Docs say `make build` | **Solved** | Getting-started docs now show three install paths (binary download, `go install`, build from source); `make build` is contributor-only |
| Must clone the repo | **Solved** | Binary download and `go install` both work without cloning |
| Credential setup is 4 steps | **Solved** | Docs point users to Settings → API Configuration in the UI; no restart needed |
| Sandbox image auto-pull | **Already existed** | `ensureImage()` in `internal/cli/server.go` pulls from ghcr.io on first run |

**Current first-run flow (as documented):**
```
Step 1: Get credential (OAuth token or API key)
Step 2: Download binary / go install / build from source
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

### 2. No install script for binary download

Users must manually construct the download URL with their OS/arch. A one-liner install script would reduce this to:
```bash
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | bash
```

**Effort:** Low
**Impact:** Medium — removes the manual OS/arch URL construction step

The script would:
- Detect `uname -s` / `uname -m` → map to release artifact names
- Download from GitHub Releases latest
- Place in a sensible location (`/usr/local/bin` or `~/.local/bin`)
- Print next-steps instructions

### 3. No Homebrew tap

For macOS users, `brew install` is the most familiar installation pattern. A tap formula is straightforward once pre-built binaries exist (which they do).

**Effort:** Medium (create `changkun/homebrew-tap` repo with formula)
**Impact:** Medium — familiar for macOS/Linux users

### 4. Container runtime prerequisite discovery

Podman or Docker must be installed, but the current flow doesn't help users who have neither. The server detects the runtime at startup and logs it, but:
- If neither is found, the error message could be more actionable
- Users on macOS may not realize they need Podman Desktop or Docker Desktop running (not just installed)
- No guidance on which to choose

**Effort:** Low
**Impact:** Low-Medium — helps the subset of users who don't already have a container runtime

Possible improvements:
- Add a `wallfacer doctor` subcommand that checks all prerequisites (container runtime, image availability, credentials) and prints actionable guidance
- Improve the startup error message when no runtime is found

### 5. First-task sandbox image pull is opaque

The ~1 GB image pull on first task execution happens silently in the background. Users see their task stuck in "In Progress" with no visible progress for 1-2 minutes. The image pull logs appear in the server terminal but not in the UI.

**Effort:** Medium
**Impact:** Medium — reduces confusion during first run

Possible improvements:
- Show image pull progress in the UI task logs
- Add a startup banner or notification when the image isn't cached yet
- Pre-pull the image at server startup (current behavior) but surface the pull progress in the UI

### 6. Go 1.25+ is bleeding-edge

Go 1.25 is very new. Users who want to build from source or use `go install` likely have Go 1.23 or 1.24 installed. The binary download path avoids this entirely, but users who reach for `go install` first will hit a confusing version error.

**Recommendation:** This is self-resolving as Go 1.25 becomes mainstream. In the meantime, the docs already list binary download as the primary path. No action needed beyond keeping the version requirement visible.

## Revised Priority

Given what's already been implemented, the remaining work in priority order:

1. **Install script** (Option 2 above) — Low effort, removes the most common remaining friction for new users. Depends on nothing new.

2. **`wallfacer doctor` command** (Option 4 above) — Low effort, catches misconfiguration early. Checks: container runtime available and running, sandbox image cached, credentials configured, Git installed.

3. **Homebrew tap** (Option 3 above) — Medium effort, familiar install path for macOS users.

4. **Verify `go install` from module proxy** (Option 1 above) — Trivial effort, just needs testing after next release.

5. **Surface image pull progress in UI** (Option 5 above) — Medium effort, improves first-run UX but not a blocker.

## Ideal First-Run Experience (Current vs Target)

**Current (already good):**
```bash
# Download binary (must know OS/arch)
curl -L https://github.com/changkun/wallfacer/releases/latest/download/wallfacer-darwin-arm64 -o wallfacer
chmod +x wallfacer
./wallfacer run ~/project
# Configure credential in browser → create task
```

**Target (with remaining improvements):**
```bash
# One-liner install (detects OS/arch automatically)
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | bash

# OR: Homebrew
brew install changkun/tap/wallfacer

# Check prerequisites
wallfacer doctor

# Run
wallfacer run ~/project
# Configure credential in browser → create task
# (image pull progress visible in UI)
```

The delta is small. The biggest wins are the install script and the doctor command.
