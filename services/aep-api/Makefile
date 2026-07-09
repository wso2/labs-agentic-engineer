.PHONY: build test test-db cover check-no-legacy-creds gen-oc-client gen-oc-client-check gen-mocks openapi openapi-check vendor-skills vendor-skills-check

build:
	go build ./...

# The committed spec (packages/contracts/api/v1/openapi.yaml) is the SOURCE OF
# TRUTH; the Go code follows it. This target exports what the code currently
# serves — a comparison aid only, deliberately NOT wired into `make gen`
# (contract-first inversion; server stubs generated from the spec are tracked
# in the repo issue referenced by openapi-check below).
#   public   → ../../packages/contracts/api/v1/openapi.yaml
#   internal → build/internal-openapi.yaml  (gitignored tooling artifact)
openapi:
	go run ./cmd/openapigen

# Drift check: fail if what the code serves differs from the committed
# contract. The fix direction is code → spec, never the reverse.
openapi-check: openapi
	@if ! git diff --exit-code --quiet -- ../../packages/contracts/api/v1/openapi.yaml; then \
		git diff --color -- ../../packages/contracts/api/v1/openapi.yaml | cat; \
		echo ""; \
		echo "aep-api serves an API that drifted from the committed contract."; \
		echo "The spec is the source of truth: update the Go handlers to match,"; \
		echo "then 'git checkout -- packages/contracts/api/v1/openapi.yaml'."; \
		exit 1; \
	fi

# Fast lane: -short skips the DB tier (dbtest.New self-skips under -short), so
# plain unit/component runs need no Docker.
test: check-no-legacy-creds
	go test -short ./...

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
# DOCKER_HOST and the Ryuk socket-mount override are derived from the active
# docker context when unset, so testcontainers finds a non-default daemon (e.g.
# Colima, whose socket lives under ~/.colima) out of the box; an explicit
# DOCKER_HOST is always respected.
test-db:
	@DOCKER_HOST="$${DOCKER_HOST:-$$(docker context inspect -f '{{.Endpoints.docker.Host}}' 2>/dev/null)}" \
	  TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE="$${TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE:-/var/run/docker.sock}" \
	  go test ./...

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
OAPI_CODEGEN    ?= go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.0

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

# The skill library embedded into the binary (skills/embedded/) is a
# `go generate` VENDOR of the repo-root skills/ directory — the single source of
# truth, which go:embed cannot reach across the module boundary. Author skills
# ONLY in repo-root skills/; re-vendor with this target.
vendor-skills:
	@go generate ./skills/
	@echo "Vendored repo-root skills/ into skills/embedded/"

# CI sync check: re-vendor, then fail if skills/embedded/ drifted from the
# repo-root skills/ source. Same posture as gen-oc-client-check (fix direction:
# source → vendor) but uses `git status --porcelain` rather than `git diff` so
# it also catches ADDED-but-not-vendored skills — the exact drift that had
# shipped a stale high-level-architecture and dropped task-breakdown from every
# org.
vendor-skills-check: vendor-skills
	@if [ -n "$$(git status --porcelain -- skills/embedded)" ]; then \
		git status --short -- skills/embedded; \
		git diff --color -- skills/embedded | cat; \
		echo ""; \
		echo "services/aep-api/skills/embedded/ drifted from the repo-root skills/ source"; \
		echo "(a skill was edited or added but not re-vendored + committed)."; \
		echo "Run 'make vendor-skills' and commit skills/embedded/."; \
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
