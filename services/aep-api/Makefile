.PHONY: build test test-db cover check-no-legacy-creds gen-api gen-api-check gen-oc-client gen-oc-client-check gen-mocks deadcode deadcode-check

build:
	go build ./...

# Fast lane: -short skips the DB tier (dbtest.New self-skips under -short), so
# plain unit/component runs need no Docker.
test: check-no-legacy-creds
	go test -short ./...

# ── Dead-code gate ───────────────────────────────────────────────────────────
# Whole-program reachability from the cmd/aep-api main via
# golang.org/x/tools/cmd/deadcode (RTA). Run WITHOUT -test: tests do NOT count
# as consumers, so a function only a test calls is dead. Generated files are
# excluded by deadcode; test-support packages and //deadcode:keep-marked seams
# are excluded by the script, whose header carries the rationale and the
# marker policy (scripts/deadcode.sh).
#
#   make deadcode        human audit (prints findings, never fails)
#   make deadcode-check  CI gate (fails the build on any surviving finding)
deadcode:
	@bash scripts/deadcode.sh report

deadcode-check:
	@bash scripts/deadcode.sh check

# ── Coverage (local convenience; no CI exists) ───────────────────────────────
# Fast lane (-short skips the DB tier, so no Docker needed). Writes coverage.out
# and prints the total statement coverage. The generated OpenChoreo client and
# its mocks are excluded so the number reflects hand-written code. Run `test-db`
# separately for the DB tier — this target intentionally stays hermetic.
COVER_PKGS = $(shell go list ./... | grep -vE '/internal/clients/openchoreo/(gen|mocks)$$')
cover:
	@go test -short -coverprofile=coverage.out $(COVER_PKGS); status=$$?; \
		echo ""; \
		echo "── total ──────────────────────────────────────────────"; \
		go tool cover -func=coverage.out | tail -1; \
		exit $$status

# Full run including the DB tier. Requires Docker (Colima etc.): dbtest.New
# starts a throwaway postgres:17 via testcontainers, migrates a template once
# with the real migrator, and clones an isolated database per test (pgtestdb).
# No build tag, no shared :5433 — isolation is the template-clone. Tests skip
# under -short and where Docker is unavailable, so this degrades gracefully.
#
# One container per package process, each terminated by that package's
# `TestMain -> dbtest.Main` once its tests finish, so a run leaves no Postgres
# on the host. Testcontainers' Ryuk sidecar is only the backstop for binaries
# that die before that (panic, signal) — it is not what a normal run relies on.
#
# DOCKER_HOST and the Ryuk socket-mount override are derived from the active
# docker context when unset, so testcontainers finds a non-default daemon (e.g.
# Colima, whose socket lives under ~/.colima) out of the box; an explicit
# DOCKER_HOST is always respected.
test-db:
	@DOCKER_HOST="$${DOCKER_HOST:-$$(docker context inspect -f '{{.Endpoints.docker.Host}}' 2>/dev/null)}" \
	  TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE="$${TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE:-/var/run/docker.sock}" \
	  go test ./...

# ── Contract-first public API codegen ────────────────────────────────────────
# The committed contract (packages/contracts/api/v1/openapi.yaml — schemas and
# paths in ONE document) is the SOURCE OF TRUTH; gen-api regenerates what serves
# it into internal/gen (package gen): the schema types + operation Params
# (models_gen.go) and the strict server that references them, plus the contract
# baked in as GetSpec() for the request validator (server_gen.go, embedded-spec
# — no vendored YAML copy needed). Generated files are committed; gen-api-check
# is the CI freshness gate.
CONTRACT_DIR          = ../../packages/contracts/api/v1
INTERNAL_CONTRACT_DIR = ../../packages/contracts/api/internal/v1
gen-api:
	@$(OAPI_CODEGEN) -config internal/gen/oapi-codegen-models.yaml $(CONTRACT_DIR)/openapi.yaml
	@$(OAPI_CODEGEN) -config internal/gen/oapi-codegen-server.yaml $(CONTRACT_DIR)/openapi.yaml
	@$(OAPI_CODEGEN) -config internal/igen/oapi-codegen.yaml $(INTERNAL_CONTRACT_DIR)/openapi.yaml
	@gofmt -w internal/gen/models_gen.go internal/gen/server_gen.go internal/igen/igen_gen.go
	@echo "API types + strict server (with embedded spec) regenerated"

# CI freshness gate: regenerate, then fail if anything tracked drifted — a
# contract edit without `make gen-api`, or a hand-edit of generated output.
gen-api-check: gen-api
	@if ! git diff --exit-code --quiet -- internal/gen/ internal/igen/; then \
		git diff --stat -- internal/gen/ internal/igen/ | cat; \
		echo ""; \
		echo "Generated API code is stale w.r.t. packages/contracts/api/v1."; \
		echo "Run 'make gen-api' in services/aep-api and commit the changes."; \
		exit 1; \
	fi

# Pinned spec version — bump this and re-run `make gen-oc-client` when the
# upstream OC API changes. Keep aligned with the OC build deployed by the
# deployments-v2/wso2cloud-deployment submodule. Matches agent-manager.
#
# NOTE: the deployed OpenChoreo is 1.1.1 (deployments/scripts/env.sh), but this
# generated-client spec pin stays at v1.0.1-hotfix.1 DELIBERATELY: the Resource
# model types (ResourceType / Resource / ResourceReleaseBinding) are hand-rolled
# in internal/clients/openchoreo/resource_client.go because the pinned spec
# predates them. Bump this pin only when upstream publishes a spec that COVERS
# those types (so gen can replace the hand-rolled client), not merely to track
# the deployed OC version.
OC_SPEC_VERSION ?= v1.0.1-hotfix.1
OC_SPEC_URL     ?= https://raw.githubusercontent.com/openchoreo/openchoreo/$(OC_SPEC_VERSION)/openapi/openchoreo-api.yaml

# The generator is pinned by version AND by the toolchain that builds it.
#
# server_gen.go embeds the contract as a gzipped, base64-encoded blob
# (GetSpec(), ~850 lines). gzip output is not stable across Go releases —
# compress/flate's choices change — so the same spec, generated by the same
# oapi-codegen, produces a DIFFERENT blob under a different toolchain. Left
# unpinned, `go run …@v2.7.0` builds with whatever Go the contributor has, and
# gen-api-check then fails in CI for someone whose only mistake was having a
# newer Go installed. The generated code itself is identical; only the
# compressed bytes move, which makes the failure read as a stale contract when
# nothing is stale.
#
# Derived from go.mod rather than hardcoded so it cannot drift from the module,
# and matching what CI resolves from go.work. Same reasoning, same remedy as
# scripts/deadcode.sh and the root Makefile's GO_TOOLCHAIN.
GO_DIRECTIVE  := $(shell awk '$$1 == "go" { print $$2; exit }' go.mod)
GEN_TOOLCHAIN := go$(GO_DIRECTIVE)$(if $(word 3,$(subst ., ,$(GO_DIRECTIVE))),,.0)
OAPI_CODEGEN  ?= GOTOOLCHAIN=$(GEN_TOOLCHAIN) go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.0

# Regenerate the OpenChoreo client from the pinned upstream spec.
# Generated files in internal/clients/openchoreo/gen/ are checked in; this target
# overwrites them and is the only supported way to modify them.
gen-oc-client:
	@echo "Fetching OpenChoreo spec $(OC_SPEC_VERSION)"
	@curl -fsSL "$(OC_SPEC_URL)" -o /tmp/openchoreo-api.yaml
	@echo "Generating types"
	@$(OAPI_CODEGEN) -config internal/clients/openchoreo/gen/oapi-codegen.yaml /tmp/openchoreo-api.yaml
	@echo "Generating client"
	@$(OAPI_CODEGEN) -config internal/clients/openchoreo/gen/oapi-codegen-client.yaml /tmp/openchoreo-api.yaml
	@rm /tmp/openchoreo-api.yaml
	@gofmt -w internal/clients/openchoreo/gen
	@echo "OpenChoreo client generated successfully"

# Regenerate moq fakes for the OC client interfaces. Drops files into
# internal/clients/openchoreo/mocks/ — only commit them once a test actually uses
# them, otherwise they're dead code (the `//go:generate` directives mean
# `go generate ./internal/clients/openchoreo/...` always reproduces them).
gen-mocks:
	@echo "Regenerating moq fakes"
	@go generate ./internal/clients/openchoreo/...
	@echo "Mocks regenerated under internal/clients/openchoreo/mocks/"

# CI sync check: regenerate, then fail if any tracked *.gen.go drifted.
# Uses `git diff --exit-code` so untracked files (e.g. a freshly-cut branch
# that hasn't committed gen/ yet) don't trip the check — only changes to
# already-tracked output do.
gen-oc-client-check: gen-oc-client
	@if ! git diff --exit-code --quiet -- 'internal/clients/openchoreo/gen/*.gen.go'; then \
		git diff --color -- 'internal/clients/openchoreo/gen/*.gen.go' | cat; \
		echo ""; \
		echo "internal/clients/openchoreo/gen/*.gen.go is out of sync with $(OC_SPEC_VERSION)."; \
		echo "Run 'make gen-oc-client' locally and commit the changes."; \
		exit 1; \
	fi

# Phase 2 PR C — fail the build if the legacy git-service GetCredentials bridge
# resurfaces, or if any code references the retired 'platform-pat' kind. Skips
# test files and comment-only references (lines whose first non-whitespace char
# after the path:line: is `//`).
#
# NOTE: `CreateGitSecret` was dropped from this guard — the legacy git-service
# that made it a concern is fully removed, and CreateGitSecret now names the
# sanctioned OpenChoreo GitSecret-client method (internal/clients/openchoreo +
# orgcreds/build_credentials_service.go). Re-adding it false-positives on
# architecture-endorsed code.
#
# Phase 2 PR D — also asserts EnvSecretProvider and tier_gate stay
# deleted (per phase2.md §15.4 PR D cleanup checklist). Both were
# removed in PR B / PR A and must not resurface.
check-no-legacy-creds:
	@matches=$$(grep -rEn 'gitservice\.GetCredentials|"platform-pat"|\bEnvSecretProvider\b|\btier_gate\b' --include='*.go' . 2>/dev/null \
	  | grep -v '_test\.go' \
	  | grep -v '/internal/clients/openchoreo/gen/' \
	  | grep -vE ':[[:space:]]*//' \
	  || true); \
	if [ -n "$$matches" ]; then \
		echo "legacy credential / cleanup surface detected (see phase2.md §1.10 / §15.4):"; \
		echo "$$matches"; \
		exit 1; \
	fi
