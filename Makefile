SHELL            := /bin/bash

# Load .env if it exists
-include .env
export

.PHONY: build build-binary server frontend-build api-contract fmt hooks check test lint lint-js lint-otel lint-truncate test-all lint-all test-frontend e2e-lifecycle e2e-dependency-dag ui-test commit-seq push-once skills-check skills-pull skills-push web-frontend web-run web-dev web-docker

# Full build gate: fmt + frontend assets + every lint + binary.
build: fmt frontend-build lint-all build-binary

# Build the wallfacer Go binary.
# Pass VERSION= to embed a version (e.g., make build-binary VERSION=0.0.6).
VERSION ?=
LDFLAGS := -s -w
ifneq ($(VERSION),)
LDFLAGS += -X latere.ai/x/wallfacer/internal/cli.Version=$(VERSION)
endif

build-binary: frontend-build
	go build -trimpath -ldflags "$(LDFLAGS)" -o wallfacer .

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

# Build the Vue frontend SPA into frontend/dist/ for embedding. The binary
# embeds all:frontend/dist, so the tracked frontend/dist/PLACEHOLDER.txt keeps
# a plain `go build` and `go test` compiling on a clone that has not built it.
frontend-build:
	cd frontend && bun install --frozen-lockfile && bun run build

# Format all Go source files in place.
fmt:
	gofmt -w .

hooks:                                                                   ## Install git hooks (the pre-commit delegates to lateregate)
	git config core.hooksPath .githooks
	@[ -e CLAUDE.md ] || [ -L CLAUDE.md ] || ln -s AGENTS.md CLAUDE.md
	@echo "installed git hooks (core.hooksPath=.githooks)"

# ---- The shared bar -------------------------------------------------------
# Every Go gate lives in lateregate, pinned as a tool in go.mod: formatting,
# modernizers, golangci-lint against the shared config, vet and the suite, the
# race detector, the hermetic and clean-TMPDIR runs, per-package coverage,
# govulncheck, the cgo and outbound-client scans. `make check` is a name for
# `go tool lateregate` and nothing else; `go tool lateregate list` prints the
# plan, and `go tool lateregate <gate>` runs one. Decisions and dated waivers
# live in .lateregate.yaml.
check:
	@go tool lateregate

test:
	@go tool lateregate test

lint:
	@go tool lateregate lint

# lint-otel keeps outbound HTTP instrumented so traces propagate across
# services: an http.Client literal with no Transport, or http.DefaultClient,
# calls out on the stdlib transport and the trace stops at the boundary.
lint-otel:
	@go tool lateregate otel-client

# ---- This repository's own checks ----------------------------------------
# Type-check the Vue frontend (vue-tsc --noEmit).
lint-js:
	cd frontend && bun run typecheck

# Guardrail against byte-index truncation of strings, which can cut inside a
# multi-byte UTF-8 sequence and surface as U+FFFD in API responses and logs.
lint-truncate:
	@./scripts/lint-truncate.sh

# Run Vue SPA unit tests under frontend/.
test-frontend:
	cd frontend && bun run test

# Every lint: the shared Go lint plus the frontend typecheck and the guardrails.
lint-all: lint lint-js lint-otel lint-truncate

# Everything CI runs: the whole shared bar, the frontend suite and typecheck,
# and the repo-specific guardrails.
test-all: check test-frontend lint-js lint-truncate skills-check

# End-to-end: task lifecycle (create, run, archive) for both Claude and Codex.
# Requires a running wallfacer server with valid credentials.
# Usage:
#   make e2e-lifecycle                  # both agents
#   make e2e-lifecycle SANDBOX=claude   # claude only
SANDBOX ?=
e2e-lifecycle:
	sh scripts/e2e-lifecycle.sh $(SANDBOX)

# End-to-end: dependency DAG (8 tasks with fan-out/fan-in, conflict resolution, autoimplement).
# Requires a running wallfacer server. Pass WORKSPACE= pointing at a fresh git repo.
# Usage:
#   WORKSPACE=$$(mktemp -d) && git -C $$WORKSPACE init -b main && git -C $$WORKSPACE commit --allow-empty -m init
#   make e2e-dependency-dag WORKSPACE=$$WORKSPACE
e2e-dependency-dag:
ifndef WORKSPACE
	$(error WORKSPACE is required. Create a fresh git repo and pass its path.)
endif
	sh scripts/e2e-dependency-dag.sh $(WORKSPACE)

# UI regression checks: boot wallfacer against seeded demo data and assert UI
# invariants (render-crash + broken-layout) in a real browser. Catches the class
# of bug jsdom unit tests cannot. SKIP_BUILD=1 reuses an existing ./wallfacer.
#   make ui-test
#   SKIP_BUILD=1 make ui-test
ui-test:
	sh frontend/scripts/ui-shots/ui-test.sh

# The wf-spec-* skills in .claude/skills/ are a mirror of the `spec` plugin in
# latere-ai/agent-skills. They stay committed because the Claude harness runs
# `claude -p`, which auto-discovers .claude/skills/ but does not install
# plugins. Upstream is canonical; edits are legal in either place.
#   make skills-check   fail on any difference (CI gate)
#   make skills-pull    adopt upstream changes here
#   make skills-push    promote local edits into ../agent-skills
skills-check:
	./scripts/skills.sh check

skills-pull:
	./scripts/skills.sh pull

skills-push:
	./scripts/skills.sh push

# ---- wallfacerd (wf.latere.ai) ----

web-frontend: frontend-build                                             ## Build wallfacerd frontend for embedding

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

# Releases are automated. Push a version tag and the Release workflow
# (.github/workflows/release.yml) builds the binaries, pushes the image,
# deploys to k8s, and publishes the GitHub release with generated notes:
#   git tag v0.0.7 && git push origin v0.0.7

# ── Local production release ────────────────────────────────────────────────
# Build natively (avoids the qemu bun/go crash when building Dockerfile.wallfacerd
# on an arm64 host), push the image, and roll it out to the live cluster via
# kubectl. The automated release.yml pipeline is the canonical path; this is the
# local fallback while that is unavailable.
#
#   make release-prod                 # tag = short git sha
#   make release-prod VERSION=0.0.7-alpha.24
CONTAINER          ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)
RELEASE_IMAGE      ?= ghcr.io/changkun/wallfacerd
RELEASE_NS         ?= latere
RELEASE_DEPLOYMENT ?= wallfacerd
RELEASE_URL        ?= https://wf.latere.ai

.PHONY: release-prod
release-prod: REL_VER := $(if $(VERSION),$(VERSION),$(shell git rev-parse --short HEAD))
release-prod:
	@test -n "$(CONTAINER)" || { echo "release-prod: no podman/docker found"; exit 1; }
	@echo ">> [1/5] building frontend (native)"
	$(MAKE) frontend-build
	@echo ">> [2/5] cross-compiling linux/amd64 binary ($(REL_VER))"
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
		-ldflags "-s -w -X latere.ai/x/wallfacer/internal/cli.Version=$(REL_VER)" \
		-o deploy/prebuilt/wallfacer .
	@echo ">> [3/5] building + pushing $(RELEASE_IMAGE):$(REL_VER)"
	cd deploy/prebuilt && $(CONTAINER) build --platform linux/amd64 -t $(RELEASE_IMAGE):$(REL_VER) .
	$(CONTAINER) push $(RELEASE_IMAGE):$(REL_VER)
	@echo ">> [4/5] rolling out to $(RELEASE_NS)/$(RELEASE_DEPLOYMENT) (watch)"
	kubectl set image deployment/$(RELEASE_DEPLOYMENT) $(RELEASE_DEPLOYMENT)=$(RELEASE_IMAGE):$(REL_VER) -n $(RELEASE_NS)
	kubectl rollout status deployment/$(RELEASE_DEPLOYMENT) -n $(RELEASE_NS) --timeout=200s
	@echo ">> [5/5] smoke" && curl -fsS $(RELEASE_URL)/healthz && echo " <- healthz ok ($(REL_VER) live)"
