# Streamlined First-Run Experience

## Problem

The current getting-started flow requires too many steps and prerequisites for a first-time user:

1. **Go 1.25+ must be installed** just to compile the server binary — no pre-built binaries are published.
2. **Docs tell users to `make build`**, which builds sandbox images locally from scratch (~minutes, downloads Go/Node/Python into Ubuntu). But the server already auto-pulls `ghcr.io/changkun/wallfacer:latest` on first run — so building is unnecessary for most users.
3. **Must clone the repo** before doing anything.
4. **Credential setup is 4 steps**: run server → stop server → edit `~/.wallfacer/.env` → restart server.
5. **Podman/Docker** is a genuine prerequisite (no way around it for sandboxing), but discovery of which is available could be smoother.

Net effect: a user who just wants to try Wallfacer must install Go, clone a repo, build a binary, wait for image builds, then do a start-stop-edit-restart dance. This is developer-contributor friction, not user friction.

## Current Auto-Pull Mechanism (Already Exists)

The server already handles image availability gracefully (`server.go`, `ensureImage`):

1. Check if image exists locally.
2. If not, pull `ghcr.io/changkun/wallfacer:latest` automatically.
3. If pull fails, fall back to local `wallfacer:latest`.
4. Warn if nothing is available.

Published images exist at `ghcr.io/changkun/wallfacer:latest` and `ghcr.io/changkun/wallfacer-codex:latest` (multi-arch: amd64 + arm64), built by GitHub Actions on version tags.

## Options

### Option 1: Publish Pre-Built Binaries via GitHub Releases

**Effort:** Medium
**Impact:** High — eliminates Go installation requirement entirely

Add a GitHub Actions workflow that builds the `wallfacer` binary for common platforms on each version tag and attaches them to the GitHub Release:

- `wallfacer-linux-amd64`
- `wallfacer-linux-arm64`
- `wallfacer-darwin-amd64`
- `wallfacer-darwin-arm64`

Users download one file and run it. Could use GoReleaser or a simple matrix build workflow.

**New quick-start:**
```bash
# Download the binary for your platform
curl -L https://github.com/changkun/wallfacer/releases/latest/download/wallfacer-darwin-arm64 -o wallfacer
chmod +x wallfacer

# Run (auto-pulls sandbox image on first start)
./wallfacer run ~/projects/myapp
```

### Option 2: `go install` Support

**Effort:** Low
**Impact:** Medium — still requires Go, but eliminates clone step

Ensure the module path and `main.go` support:
```bash
go install changkun.de/wallfacer@latest
wallfacer run ~/projects/myapp
```

This may already work if the module is publicly accessible and Go version compatibility is met. Needs verification and documentation.

**Limitation:** Still requires Go 1.25+, which is a bleeding-edge version (not yet widely installed).

### Option 3: Update Docs to Skip Image Building

**Effort:** Low
**Impact:** High — removes the slowest step from the getting-started flow

The docs currently say `make build` as step 2, which builds images locally. Since the server auto-pulls from ghcr.io, docs should reflect the simpler path:

**Before (current):**
```
Prerequisites: Go 1.25+, Podman/Docker, credential
Step 1: Get credential
Step 2: make build              ← builds images locally (~minutes)
Step 3: go build -o wallfacer . ← builds binary
Step 4: Run, stop, edit .env, restart
Step 5: Run for real
```

**After:**
```
Prerequisites: Go 1.25+, Podman/Docker, credential
Step 1: Get credential
Step 2: go build -o wallfacer . ← builds binary only
Step 3: ./wallfacer run ~/project
        (sandbox image auto-pulled on first task)
Step 4: Add credential via Settings → API Configuration in the UI
```

Key changes:
- Remove `make build` from the happy path entirely; mention it only as "Development" or "Building from source" for contributors.
- Reorder: credential can be entered via the UI after the server starts (Settings → API Configuration), removing the start-stop-edit-restart dance.
- Add a note that first task execution pulls the sandbox image (~1 GB, takes a minute).

### Option 4: Homebrew Tap

**Effort:** Medium
**Impact:** Medium — familiar install mechanism for macOS/Linux users

Create a Homebrew tap (`changkun/tap`) with a formula that downloads the pre-built binary:

```bash
brew install changkun/tap/wallfacer
wallfacer run ~/projects/myapp
```

Depends on Option 1 (pre-built binaries) being implemented first.

### Option 5: Curl-to-Install Script

**Effort:** Low-Medium
**Impact:** Medium — one-liner install

Provide an install script that detects OS/arch, downloads the right binary, and puts it in `$PATH`:

```bash
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | bash
```

Depends on Option 1. Common pattern (similar to Homebrew, Rust, Deno installs).

### Option 6: Streamline Credential Setup

**Effort:** Low
**Impact:** Medium — reduces cognitive steps

Currently: run server → stop → edit `.env` → restart. Instead, allow configuring credentials through the UI on first run (Settings → API Configuration already exists and calls `PUT /api/env`). The docs just need to point users there instead of the manual file-editing flow.

The server already creates `~/.wallfacer/.env` on first run and the UI already has the configuration panel. This is purely a documentation change.

## Recommended Approach

Implement in priority order:

1. **Option 3 + Option 6** (docs-only, immediate) — Update getting-started docs and README quick-start to remove `make build`, point to UI for credential entry. Zero code changes, maximum impact on perceived complexity.

2. **Option 1** (binary releases) — Add a release workflow to publish pre-built binaries. Eliminates the Go prerequisite entirely.

3. **Option 2** (`go install`) — Verify and document. Quick win for Go users.

4. **Option 5** (install script) — Once binaries are published, add a one-liner installer.

5. **Option 4** (Homebrew) — Nice-to-have after binaries are published.

## Ideal First-Run Experience (After All Changes)

```bash
# Install (pick one)
curl -fsSL https://wallfacer.dev/install.sh | bash   # Option 5
brew install changkun/tap/wallfacer                    # Option 4
go install changkun.de/wallfacer@latest                # Option 2

# Run — sandbox image auto-pulls on first task
wallfacer run ~/projects/myapp

# Configure credential in browser UI (Settings → API Configuration)
# Create first task
```

Three steps. No clone. No Go required (except Option 2). No image building. No file editing.
