# Streamlined First-Run Experience

## Status: Solved (2026-03-22)

All items from the original spec have been implemented. This document records what was done and the one remaining verification task.

## What Was Built

| Problem | Solution |
|---|---|
| No pre-built binaries | `.github/workflows/release-binary.yml` publishes binaries for linux/darwin × amd64/arm64 on each version tag |
| Docs say `make build` | Getting-started docs show three install paths (binary download, `go install`, build from source); `make build` is contributor-only |
| Must clone the repo | Binary download, install script, and `go install` all work without cloning |
| Credential setup is 4 steps | Settings → API Configuration in the UI; no restart needed |
| Sandbox image auto-pull | `ensureImage()` in `internal/cli/server.go` pulls from ghcr.io on first run |
| No install script | `install.sh` detects OS/arch, downloads from GitHub Releases, installs to `/usr/local/bin` or `~/.local/bin` |
| Container runtime discovery | `wallfacer doctor` checks runtime availability with actionable messages |
| Image pull status opaque | `GET /api/config` returns `image_cached`; `wallfacer doctor` reports image availability |

## Current First-Run Flow

```bash
# 1. Install
curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh

# 2. Verify prerequisites
wallfacer doctor

# 3. Start
wallfacer run ~/project

# 4. In browser: configure credential in Settings → API Configuration, then create first task
```

## Open Item

### Verify `go install` from module proxy

`main.go` embeds `ui/` and `docs/` directories. When `go install changkun.de/x/wallfacer@latest` fetches the module from the proxy, these directories should be included in the module zip (Go's module system includes non-Go files in embedded directories). This needs verification with a real published version.

Additionally, Go 1.25 is relatively new — users with Go 1.23/1.24 will see a version error. The binary download path (documented as primary) avoids this entirely.

**Action:** Test `go install` from a clean environment after the next release. If it works, close this item. If the embed fails via module proxy, either remove the embed in favor of a separate asset download, or document the limitation.
