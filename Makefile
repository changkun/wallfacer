SHELL            := /bin/bash

# Load .env if it exists
-include .env
export

.PHONY: build build-binary install-wails build-desktop build-desktop-darwin build-desktop-windows build-desktop-linux server frontend-build api-contract fmt fmt-go lint lint-go lint-js test test-backend test-frontend e2e-lifecycle e2e-dependency-dag commit-seq push-once release-notes release

# Full build gate: fmt + lint + binary.
build: fmt lint frontend-build build-binary

# Build the wallfacer Go binary.
# Pass VERSION= to embed a version (e.g., make build-binary VERSION=0.0.6).
VERSION ?=
LDFLAGS := -s -w
ifneq ($(VERSION),)
LDFLAGS += -X changkun.de/x/wallfacer/internal/cli.Version=$(VERSION)
endif

build-binary:
	go build -trimpath -ldflags "$(LDFLAGS)" -o wallfacer .

# Install the Wails CLI (tracked as a tool dependency in go.mod).
install-wails:
	go install github.com/wailsapp/wails/v2/cmd/wails

# Build the native desktop app for the current platform (requires wails CLI).
# -skipbindings: we use a reverse proxy, not Wails Go bindings
# -s: frontend is embedded via go:embed, not built by Wails
build-desktop:
	go tool wails build -tags desktop -skipbindings -s

# Build macOS universal .app bundle.
build-desktop-darwin:
	go tool wails build -tags desktop -skipbindings -s -platform darwin/universal

# Build Windows .exe.
build-desktop-windows:
	go tool wails build -tags desktop -skipbindings -s -platform windows/amd64

# Build Linux desktop binary.
build-desktop-linux:
	go tool wails build -tags desktop -skipbindings -s -platform linux/amd64

# Build and run the Go server natively.
server:
	go build -o wallfacer . && ./wallfacer run

# Regenerate derived API artifacts from the contract definition.
# Run this after editing internal/apicontract/routes.go.
# Staleness is enforced automatically by the tests in internal/apicontract/generate_test.go.
# The route registration test verifies every contract route has a handler in BuildMux.
api-contract:
	go run scripts/gen-api-contract.go
	go test ./internal/cli/ -run TestContractRoutes_AllRegisteredInMux -count=1

# Build the Vue frontend SPA into frontend/dist/ for embedding.
frontend-build:
	cd frontend && bun install --frozen-lockfile && bun run build

# Format all source files (Go).
fmt: fmt-go

# Format all Go source files
fmt-go:
	gofmt -w .

# Run all linters (Go + frontend)
lint: lint-go lint-js

# Run Go linters (golangci-lint if available, otherwise go vet)
lint-go:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found, falling back to go vet"; \
		go vet ./...; \
	fi

# Type-check the Vue frontend (vue-tsc --noEmit).
lint-js:
	cd frontend && bun run typecheck

# Run all checks (fmt + lint + backend tests + frontend tests)
test: fmt lint test-backend test-frontend

# Run Go unit tests
test-backend:
	go test ./...

# Run Vue SPA unit tests under frontend/.
test-frontend:
	cd frontend && bunx vitest run

# End-to-end: task lifecycle (create, run, archive) for both Claude and Codex.
# Requires a running wallfacer server with valid credentials.
# Usage:
#   make e2e-lifecycle                  # both agents
#   make e2e-lifecycle SANDBOX=claude   # claude only
SANDBOX ?=
e2e-lifecycle:
	sh scripts/e2e-lifecycle.sh $(SANDBOX)

# End-to-end: dependency DAG (8 tasks with fan-out/fan-in, conflict resolution, autopilot).
# Requires a running wallfacer server. Pass WORKSPACE= pointing at a fresh git repo.
# Usage:
#   WORKSPACE=$$(mktemp -d) && git -C $$WORKSPACE init -b main && git -C $$WORKSPACE commit --allow-empty -m init
#   make e2e-dependency-dag WORKSPACE=$$WORKSPACE
e2e-dependency-dag:
ifndef WORKSPACE
	$(error WORKSPACE is required. Create a fresh git repo and pass its path.)
endif
	sh scripts/e2e-dependency-dag.sh $(WORKSPACE)

# ---- wallfacerd (wf.latere.ai) ----

web-frontend:                                                            ## Build wallfacerd frontend and copy dist for embedding
	cd frontend && bun run build
	rm -rf internal/webserver/spa/dist
	cp -r frontend/dist internal/webserver/spa/dist

web-run: web-frontend                                                    ## Run wallfacerd locally (embedded SPA)
	go run . web -addr :8080

web-dev:                                                                 ## Run wallfacerd dev stack (Go :8080 + Vite :5173)
	go run . web -addr :8080 & cd frontend && bun run dev

web-docker:                                                              ## Build wallfacerd Docker image
	docker build -f Dockerfile.wallfacerd -t wallfacerd:dev .

# Create one sequential commit with style-checked message + required body.
# Usage:
#   git add <files>
#   make commit-seq MSG="internal/handler: fix sandbox gating" DESC="Validate codex readiness before allowing task creation."
commit-seq:
ifndef MSG
	$(error MSG is required. Example: MSG="internal/runner: fix fallback logic")
endif
ifndef DESC
	$(error DESC is required. Example: DESC="Explain what changed and why.")
endif
	./scripts/commit-seq.sh "$(MSG)" "$(DESC)"

# Push once after all sequential commits are created.
# Usage:
#   make push-once
#   make push-once REMOTE=origin BRANCH=main
REMOTE ?= origin
BRANCH ?= $(shell git branch --show-current)
push-once:
	./scripts/push-once.sh "$(REMOTE)" "$(BRANCH)"

# Generate release notes via LLM and save to docs/releases/.
# The script builds a prompt from the git diff, pipes it through claude,
# and writes the result to docs/releases/<version>.md.
# Usage:
#   make release-notes RELEASE_VERSION=v0.0.6
release-notes:
ifndef RELEASE_VERSION
	$(error RELEASE_VERSION is required. Usage: make release-notes RELEASE_VERSION=v0.0.6)
endif
	@./scripts/release-notes.sh "$(RELEASE_VERSION)"

# Create a GitHub release.
# Expects docs/releases/<version>.md to exist (run make release-notes first).
# Commits the notes, tags, pushes, and creates the GitHub release.
# Usage:
#   make release-notes RELEASE_VERSION=v0.0.6   # step 1: generate + review
#   make release RELEASE_VERSION=v0.0.6          # step 2: publish
release:
ifndef RELEASE_VERSION
	$(error RELEASE_VERSION is required. Usage: make release RELEASE_VERSION=v0.0.6)
endif
	@test -f docs/releases/$(RELEASE_VERSION).md || (echo "Error: docs/releases/$(RELEASE_VERSION).md not found. Run 'make release-notes' first." >&2; exit 1)
	git add docs/releases/$(RELEASE_VERSION).md
	git commit -m "docs: add $(RELEASE_VERSION) release notes"
	git tag -a "$(RELEASE_VERSION)" -m "$(RELEASE_VERSION)"
	git push origin main "$(RELEASE_VERSION)"
	gh release create "$(RELEASE_VERSION)" --title "$(RELEASE_VERSION)" --notes-file docs/releases/$(RELEASE_VERSION).md
