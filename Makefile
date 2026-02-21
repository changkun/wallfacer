SHELL      := /bin/bash
PODMAN     := /opt/podman/bin/podman
IMAGE      := wallfacer:latest
GHCR_IMAGE := ghcr.io/changkun/wallfacer:latest
NAME       := wallfacer

# Load .env if it exists
-include .env
export

.PHONY: build server run shell clean ui-css

# Build the sandbox image and tag it with both the local name and the ghcr.io
# name so that 'wallfacer run' finds it under the default image reference.
build:
	$(PODMAN) build -t $(IMAGE) -t $(GHCR_IMAGE) -f sandbox/Dockerfile sandbox/

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

# Regenerate the static Tailwind CSS from UI sources (requires Node.js + network).
# Run this after adding new Tailwind utility classes to ui/index.html or ui/js/*.js.
ui-css:
	npx tailwindcss@3 -i tailwind.input.css -o ui/css/tailwind.css \
		--content './ui/**/*.{html,js}' --minify

# Remove the sandbox image
clean:
	-$(PODMAN) rmi $(IMAGE) $(GHCR_IMAGE)
