PODMAN  := /opt/podman/bin/podman
IMAGE   := wallfacer:latest
NAME    := wallfacer

# Space-separated list of folders to mount under /workspace/<basename>
WORKSPACES ?= $(CURDIR)

# Load .env if it exists
-include container/.env
export

# Generate -v flags: /path/to/foo -> -v /path/to/foo:/workspace/foo:z
VOLUME_MOUNTS := $(foreach ws,$(WORKSPACES),-v $(ws):/workspace/$(notdir $(ws)):z)

.PHONY: build run interactive shell stop clean

# Build the container image
build:
	$(PODMAN) build -t $(IMAGE) -f container/Dockerfile container/

# Headless mode: make run PROMPT="fix the failing tests"
run:
ifndef PROMPT
	$(error PROMPT is required. Usage: make run PROMPT="your task here")
endif
	$(PODMAN) run --rm -it \
		--name $(NAME) \
		-e CLAUDE_CODE_OAUTH_TOKEN \
		$(VOLUME_MOUNTS) \
		-v claude-config:/home/claude/.claude \
		-w /workspace \
		$(IMAGE) -p "$(PROMPT)" --verbose --output-format stream-json

# Interactive TUI mode
interactive:
	$(PODMAN) run --rm -it \
		--name $(NAME) \
		-e CLAUDE_CODE_OAUTH_TOKEN \
		$(VOLUME_MOUNTS) \
		-v claude-config:/home/claude/.claude \
		-w /workspace \
		$(IMAGE)

# Debug shell
shell:
	$(PODMAN) run --rm -it \
		--name $(NAME) \
		-e CLAUDE_CODE_OAUTH_TOKEN \
		$(VOLUME_MOUNTS) \
		-v claude-config:/home/claude/.claude \
		-w /workspace \
		--entrypoint /bin/bash \
		$(IMAGE)

stop:
	-$(PODMAN) stop $(NAME)

clean:
	-$(PODMAN) stop $(NAME)
	-$(PODMAN) rm $(NAME)
	-$(PODMAN) volume rm claude-config
	-$(PODMAN) rmi $(IMAGE)
