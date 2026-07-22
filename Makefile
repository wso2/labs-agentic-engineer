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
LICENSE_MATCH = grep -E '\.(go|ts|tsx|sh)$$|(^|/)Dockerfile$$' | \
	grep -vE '\.gen\.(go|ts)$$|_mock\.go$$|/mocks/|/node_modules/|/dist/|/generated/|(^|/)\.(agents|claude)/'

.PHONY: install gen build dev test lint typecheck license license-check tools clean cover build-validation-runner deadcode-ts deadcode-ts-check

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

# Local coverage summary (there is no CI). Go: the aep-api module's fast-lane
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

# Local-dev helper (not a uniform verb): build + k3d-import the validation-task
# runner image. setup-aep.sh runs this automatically at setup; use it to force a
# rebuild after changing Dockerfile.validation — `make build-validation-runner FORCE=1`.
build-validation-runner:
	FORCE=$(FORCE) bash deployments/scripts/build-validation-runner.sh

clean:
	$(TURBO) run build --force >/dev/null 2>&1 || true
	rm -rf .turbo
	find . -type d -name dist -prune -not -path './node_modules/*' -exec rm -rf {} + 2>/dev/null || true
	find . -type d -name .turbo -prune -not -path './node_modules/*' -exec rm -rf {} + 2>/dev/null || true
	@for d in $(GO_MODULE_DIRS); do ( cd "$$d" && go clean ./... ); done
