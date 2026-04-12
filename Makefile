SHELL            := /bin/bash
PODMAN           := /opt/podman/bin/podman
# Resolve the latest versioned tag (e.g. v0.0.4) of latere-ai/images.
# Tries the GitHub releases API first (one HTTP call); on failure — rate-limit,
# offline, jq missing, field absent — falls back to `git ls-remote --tags` on
# the images repo, which is unauthenticated and works in more environments.
# The result is the highest semver-sorted tag matching `vMAJOR.MINOR.PATCH`.
# If both lookups fail the expansion is empty and the `ifeq` below fails the
# build with a clear message (old behaviour silently pulled `image:` with a
# bare colon). Override on the command line: `make SANDBOX_TAG=v0.0.3 …`.
SANDBOX_TAG      := $(shell \
  tag=$$(curl -sf https://api.github.com/repos/latere-ai/images/releases/latest 2>/dev/null | jq -r '.tag_name // empty' 2>/dev/null); \
  if [ -z "$$tag" ]; then \
    tag=$$(git ls-remote --tags https://github.com/latere-ai/images 2>/dev/null | awk -F/ '{print $$NF}' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | sort -V | tail -1); \
  fi; \
  echo $$tag)
ifeq ($(SANDBOX_TAG),)
$(error Could not resolve the latest sandbox image tag from GitHub. Check your network, or override: make SANDBOX_TAG=vX.Y.Z <target>)
endif
IMAGE            := sandbox-claude:$(SANDBOX_TAG)
GHCR_IMAGE       := ghcr.io/latere-ai/sandbox-claude:$(SANDBOX_TAG)
CODEX_IMAGE      := sandbox-codex:$(SANDBOX_TAG)
GHCR_CODEX_IMAGE := ghcr.io/latere-ai/sandbox-codex:$(SANDBOX_TAG)
NAME             := wallfacer

# Load .env if it exists
-include .env
export

.PHONY: build build-binary pull-images install-wails build-desktop build-desktop-darwin build-desktop-windows build-desktop-linux server run shell clean ui-css ui-ts typecheck-js api-contract fmt fmt-go fmt-js lint lint-go lint-js test test-backend test-frontend e2e-lifecycle e2e-dependency-dag commit-seq push-once release-notes release

# Build the wallfacer binary and pull sandbox images.
build: fmt lint ui-ts build-binary pull-images

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

# Pull sandbox images from GHCR and tag locally.
# Images are maintained in https://github.com/latere-ai/images
pull-images:
	$(PODMAN) pull $(GHCR_IMAGE)
	$(PODMAN) tag $(GHCR_IMAGE) $(IMAGE)
	$(PODMAN) pull $(GHCR_CODEX_IMAGE)
	$(PODMAN) tag $(GHCR_CODEX_IMAGE) $(CODEX_IMAGE)

# Build and run the Go server natively
server:
	go build -o wallfacer . && ./wallfacer run

# Space-separated list of folders to mount under /workspace/<basename>
WORKSPACES ?= $(CURDIR)

# Generate -v flags: /path/to/foo -> -v /path/to/foo:/workspace/foo:z
VOLUME_MOUNTS := $(foreach ws,$(WORKSPACES),-v $(ws):/workspace/$(notdir $(ws)):z)

# Headless one-shot: make run PROMPT="fix the failing tests"
# Mount host gitconfig read-only; set safe.directory via env so the file stays immutable
GITCONFIG_MOUNT := -v $(HOME)/.gitconfig:/home/claude/.gitconfig:ro,z \
	-e "GIT_CONFIG_COUNT=1" \
	-e "GIT_CONFIG_KEY_0=safe.directory" \
	-e "GIT_CONFIG_VALUE_0=*"

run:
ifndef PROMPT
	$(error PROMPT is required. Usage: make run PROMPT="your task here")
endif
	@$(PODMAN) run --rm -it \
		--name $(NAME) \
		--env-file .env \
		$(GITCONFIG_MOUNT) \
		$(VOLUME_MOUNTS) \
		-v claude-config:/home/claude/.claude \
		-w /workspace \
		$(IMAGE) -p "$(PROMPT)" --verbose --output-format stream-json

# Debug shell into a sandbox container
shell:
	$(PODMAN) run --rm -it \
		--name $(NAME)-shell \
		--env-file .env \
		$(GITCONFIG_MOUNT) \
		$(VOLUME_MOUNTS) \
		-v claude-config:/home/claude/.claude \
		-w /workspace \
		--entrypoint /bin/bash \
		$(IMAGE)

# Regenerate derived API artifacts from the contract definition.
# Run this after editing internal/apicontract/routes.go.
# Staleness is enforced automatically by the tests in internal/apicontract/generate_test.go.
# The route registration test verifies every contract route has a handler in BuildMux.
api-contract:
	go run scripts/gen-api-contract.go
	npx --yes prettier@3 --write ui/js/generated/
	go test ./internal/cli/ -run TestContractRoutes_AllRegisteredInMux -count=1

# Transpile TypeScript sources under ui/js/ to sibling .js files so the
# embedded UI keeps working without a bundler. Uses esbuild as a pure
# per-file transpiler (no IIFE wrap, no module shim) to preserve the
# script-tag global-scope model. Re-run after editing any .ts source.
ui-ts:
	cd ui && node scripts/build-ts.mjs

# Run the TypeScript type checker without emitting output. Runs as part
# of `make lint` so pre-commit catches type errors before shipping.
typecheck-js:
	cd ui && npx --no-install tsc --noEmit || \
		(cd ui && npm install --no-audit --no-fund && npx --no-install tsc --noEmit)

# Regenerate the static Tailwind CSS from UI sources (requires Node.js + network).
# Run this after adding new Tailwind utility classes to ui/index.html or ui/js/*.js.
ui-css:
	npx tailwindcss@3 -i ui/tailwind.input.css -o ui/css/tailwind.css \
		--content './ui/**/*.{html,js}' --minify

# Format all source files (Go + frontend)
fmt: fmt-go fmt-js

# Format all Go source files
fmt-go:
	gofmt -w .

# Format frontend JS, HTML, and CSS files
fmt-js:
	npx --yes prettier@3 --write 'ui/**/*.{js,html,css}' '!ui/index.html' '!ui/js/vendor/**' '!ui/css/vendor/**'

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

# Run frontend linter (Biome) over ui/js and ui/partials, plus TypeScript typecheck.
lint-js: typecheck-js
	cd ui && npx --yes @biomejs/biome@1.9.4 lint --max-diagnostics=5000 js partials

# Run all checks (fmt + lint + backend tests + frontend tests)
test: fmt lint test-backend test-frontend

# Run Go unit tests
test-backend:
	go test ./...

# Run frontend JavaScript unit tests. Depends on ui-ts so vm-based
# tests that readFileSync the compiled .js twins see fresh output.
test-frontend: ui-ts
	cd ui && npx --yes vitest@2 run

# End-to-end: task lifecycle (create, run, archive) for both Claude and Codex sandboxes.
# Requires a running wallfacer server with valid credentials.
# Usage: make e2e-lifecycle [SANDBOX=claude|codex]
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

# Remove sandbox images
clean:
	-$(PODMAN) rmi $(IMAGE) $(GHCR_IMAGE) $(CODEX_IMAGE) $(GHCR_CODEX_IMAGE)

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
