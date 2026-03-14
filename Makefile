SHELL            := /bin/bash
PODMAN           := /opt/podman/bin/podman
IMAGE            := wallfacer:latest
GHCR_IMAGE       := ghcr.io/changkun/wallfacer:latest
CODEX_IMAGE      := wallfacer-codex:latest
GHCR_CODEX_IMAGE := ghcr.io/changkun/wallfacer-codex:latest
NAME             := wallfacer

# Load .env if it exists
-include .env
export

.PHONY: build build-binary build-claude build-codex server run shell clean ui-css api-contract lint test test-backend test-frontend commit-seq push-once

# Build the wallfacer binary and both sandbox images.
build: build-binary build-claude build-codex

# Build the wallfacer Go binary.
build-binary:
	go build -o wallfacer .

# Build the Claude Code sandbox image and tag it with both the local name and the ghcr.io
# name so that 'wallfacer run' finds it under the default image reference.
build-claude:
	$(PODMAN) build -t $(IMAGE) -t $(GHCR_IMAGE) -f sandbox/claude/Dockerfile sandbox/claude/

# Build the OpenAI Codex sandbox image.
build-codex:
	$(PODMAN) build -t $(CODEX_IMAGE) -t $(GHCR_CODEX_IMAGE) -f sandbox/codex/Dockerfile sandbox/codex/

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
api-contract:
	go run scripts/gen-api-contract.go

# Regenerate the static Tailwind CSS from UI sources (requires Node.js + network).
# Run this after adding new Tailwind utility classes to ui/index.html or ui/js/*.js.
ui-css:
	npx tailwindcss@3 -i tailwind.input.css -o ui/css/tailwind.css \
		--content './ui/**/*.{html,js}' --minify

# Run golangci-lint
lint:
	golangci-lint run ./...

# Run all tests (backend + frontend)
test: test-backend test-frontend

# Run Go unit tests
test-backend:
	go test ./...

# Run frontend JavaScript unit tests
test-frontend:
	npx --yes vitest@2 run

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
