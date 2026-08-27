# AEP root Makefile — the single entry point for the uniform verbs.
#
# Fans out to Turborepo (TypeScript) and a `go` loop over the go.work members.
# This is how Go packages get the same verb names without a package.json.
#
#   make install      install JS deps (pnpm) and sync the Go workspace
#   make gen          regenerate contracts (TS codegen); the aep-api OpenAPI
#                     spec is hand-authored source of truth, NOT generated here
#   make build        build everything (runs gen first)
#   make dev          start dev servers (TS)
#   make test         run tests
#   make lint         lint TS (eslint) + Go (golangci-lint)
#   make typecheck    typecheck TS (tsc) + Go (go vet)
#   make license      add license headers to all in-scope sources
#   make license-check  fail if any in-scope source is missing a header
#   make tools        install pinned Go tools (golangci-lint)
#   make clean        remove build output and caches

SHELL := /bin/bash
ROOT := $(CURDIR)
GOBIN := $(shell go env GOPATH)/bin

# Workspace Go modules = modules whose dir lives under the repo root.
# Discovered dynamically from go.work, so adding a `use` line is the only edit
# needed to adopt a new Go module.
GO_MODULE_DIRS := $(shell go list -m -f '{{.Dir}}' 2>/dev/null | grep -F '$(ROOT)')

PNPM := pnpm
TURBO := $(PNPM) turbo
GOLANGCI := $(GOBIN)/golangci-lint

# golangci-lint must be built with a Go toolchain >= the modules' go directive
# (it refuses to analyze a newer-targeted module). `make tools` forces the
# project toolchain so the installed binary matches.
GOLANGCI_VERSION := v2.12.2
GO_TOOLCHAIN := go1.26.0

# addlicense applies the WSO2 Apache-2.0 header, picking the comment style per
# file type. Idempotent. Generated Go and vendored/build output are excluded.
ADDLICENSE := go run github.com/google/addlicense@v1.2.0
LICENSE_HEADER := .github/license-header.txt
# Filter git-tracked files to the in-scope source types. Kept as a pipeline (not
# a $(shell)-expanded arg list) so filenames with shell metacharacters — e.g.
# TanStack route files like projects.$projectName.tsx — reach addlicense verbatim
# via NUL-delimited xargs instead of being word-split / $-expanded by the shell.
# No `/vendor/` exclusion: the runner's vendored bal library distribution was the
# only one in the repo, and ADR-0008 replaced it with a build stage.
LICENSE_MATCH = grep -E '\.(go|ts|tsx|sh)$$|(^|/)Dockerfile$$' | \
	grep -vE '\.gen\.(go|ts)$$|_mock\.go$$|/mocks/|/node_modules/|/dist/|/generated/|(^|/)\.(agents|claude)/'

.PHONY: install gen build dev test lint eval-ui typecheck license license-check tools clean eval cover build-runner workflow-skill deadcode-ts deadcode-ts-check setup-local dev-cluster deploy-local bal-library-tool

install:
	$(PNPM) install
	go work sync

gen:
	$(TURBO) run gen
	@for d in $(GO_MODULE_DIRS); do echo ">> go generate $$d"; ( cd "$$d" && go generate ./... ); done

build: gen
	$(TURBO) run build
	@for d in $(GO_MODULE_DIRS); do echo ">> go build $$d"; ( cd "$$d" && go build ./... ); done

dev:
	$(TURBO) run dev

test: gen
	$(TURBO) run test
	@for d in $(GO_MODULE_DIRS); do echo ">> go test $$d"; ( cd "$$d" && go test ./... ); done

# Local coverage summary — coverage is not gated in CI. Go: the aep-api module's fast-lane
# cover target (-short, no Docker). TS: @aep/agents via node:test's
# --experimental-test-coverage. Report-only — the TS side never fails the verb,
# and spends no tokens. Extend module-by-module as other packages grow tests.
cover:
	@echo ">> Go coverage — services/aep-api (fast lane, -short)"
	@$(MAKE) -C services/aep-api cover || true
	@echo ""
	@echo ">> TS coverage — @aep/agents (node:test --experimental-test-coverage)"
	@$(PNPM) --filter @aep/agents exec node --experimental-test-coverage --import tsx --test "test/**/*.test.ts" 2>/dev/null \
		| grep -E '^# (tests|pass|fail|all files)' || echo "  (TS coverage unavailable — run 'pnpm --filter @aep/agents test' to debug)"

# Spec-agent evals (evals/spec-agents). On-demand only — never wired into CI.
#   make eval                 run every eval (real model calls, costs money)
#   make eval EVAL=<file>     one eval file, e.g. EVAL=evals/requirements.eval.ts
#   make eval-ui              run once + serve the local results UI
eval:
	$(PNPM) --filter @aep/spec-agent-evals eval $(if $(EVAL),-- $(EVAL),)

eval-ui:
	$(PNPM) --filter @aep/spec-agent-evals eval:ui

# Ballerina coding evals: host mode, your own `claude login`, on demand.
# Needs an installed `bal library` — packages/bal-library-tool/install-local.sh.
eval-bal:
	$(PNPM) --filter @aep/ballerina-evals eval $(if $(ARGS),-- $(ARGS),)

lint:
	$(TURBO) run lint
	@for d in $(GO_MODULE_DIRS); do echo ">> golangci-lint $$d"; ( cd "$$d" && $(GOLANGCI) run ./... ); done

typecheck: gen
	$(TURBO) run typecheck
	@for d in $(GO_MODULE_DIRS); do echo ">> go vet $$d"; ( cd "$$d" && go vet ./... ); done

license:
	@git ls-files | $(LICENSE_MATCH) | tr '\n' '\0' | xargs -0 $(ADDLICENSE) -f $(LICENSE_HEADER)

license-check:
	@git ls-files | $(LICENSE_MATCH) | tr '\n' '\0' | xargs -0 $(ADDLICENSE) -check -f $(LICENSE_HEADER)

tools:
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

# TS dead-code gate (knip) — the counterpart of services/aep-api's Go
# `deadcode-check`. Whole-program unused-export/file/dependency analysis over the
# agents runtime + the playground that consumes it, run with --production so
# *.test.ts never count as consumers. Config + rationale live in knip.jsonc.
#   make deadcode-ts        human report (never fails)
#   make deadcode-ts-check  CI gate (fails on any finding)
deadcode-ts:
	$(PNPM) run deadcode-ts

deadcode-ts-check:
	$(PNPM) run deadcode-ts:check

# Local-dev helper (not a uniform verb): build + k3d-import the runner image
# (one image, both task kinds). setup-aep.sh runs this automatically at setup;
# use it to force a rebuild after changing runners/remote-worker/Dockerfile or
# the runner's TS — `make build-runner FORCE=1`.
build-runner:
	FORCE=$(FORCE) bash deployments/scripts/build-runner.sh

# Build the `bal library` tool jar into its working tree, which is what the
# playground bind-mounts over the image's installed copy — so this is the whole
# edit-run loop for the tool, with no image rebuild (ADR-0008). Needs JDK 21 and
# a token with `read:packages` (see the tool's README).
# The runner image builds its own copy; nothing here feeds it.
# NOT the loop for host runs (`make eval-bal`, `pnpm play <dir> code --host`): those resolve
# `bal library` out of your own ~/.ballerina, which only install-local.sh writes.
# The evals read this jar too, but only to compare mtimes and refuse a stale sweep.
bal-library-tool:
	cd packages/bal-library-tool && ./gradlew :native:jar

# Print the `aep` workflow skill exactly as a coding session reads it. Local
# mode's text is DERIVED (the authored SKILL.md + skills/aep/overlays/local.md),
# so it exists in no file; this runs the same composer a run runs, which is why
# there is no second copy to drift.
#   make workflow-skill             # the platform's dispatched run, verbatim
#   MODE=local make workflow-skill  # what a playground run reads
workflow-skill:
	@cd runners/remote-worker && npx tsx src/compose_workflow.ts

# ── Local in-cluster dev (Skaffold + k3d) ────────────────────────────────────
# An alternative to the default docker-compose flow (deployments/scripts/start.sh),
# which runs the AEP services in-cluster instead of as host containers.
#
# Run once per cluster after setup-k3d.sh. Creates K8s Secrets and registers
# AEP OAuth clients in Thunder. Idempotent. No Anthropic key needed — orgs
# connect their own from the console and there is no platform fallback.
setup-local:
	bash deployments/scripts/setup-local.sh

# values.local.dev.yaml holds per-developer chart overrides (git-ignored; copy
# from values.local.dev.yaml.example). Ensure an empty stub exists so skaffold
# never fails when a developer hasn't created one or on a fresh checkout.
LOCAL_DEV_VALUES := deployments/helm-charts/platform/values.local.dev.yaml
define ensure_local_dev_values
	@test -f $(LOCAL_DEV_VALUES) || printf '# Per-developer overrides (git-ignored). See values.local.dev.yaml.example.\n{}\n' > $(LOCAL_DEV_VALUES)
endef

# Inner dev loop: build images, load into k3d, deploy via Helm, watch for changes.
# Console: http://console.openchoreo.localhost:8080
# aep-api: http://localhost:9090 (port-forwarded by Skaffold)
dev-cluster:
	$(ensure_local_dev_values)
	skaffold dev --kube-context k3d-openchoreo -f skaffold.yaml

# One-shot build + deploy (no watch). Useful for CI smoke tests or resetting state.
deploy-local:
	$(ensure_local_dev_values)
	skaffold run --kube-context k3d-openchoreo -f skaffold.yaml

clean:
	$(TURBO) run build --force >/dev/null 2>&1 || true
	rm -rf .turbo
	find . -type d -name dist -prune -not -path './node_modules/*' -exec rm -rf {} + 2>/dev/null || true
	find . -type d -name .turbo -prune -not -path './node_modules/*' -exec rm -rf {} + 2>/dev/null || true
	@for d in $(GO_MODULE_DIRS); do ( cd "$$d" && go clean ./... ); done
