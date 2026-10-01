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

.PHONY: install gen build dev test lint eval-ui typecheck license license-check tools clean eval cover build-runner workflow-skill deadcode-ts deadcode-ts-check manifests-check dev-env dev-images dev-update dev-runner obs-park obs-unpark obs-status bal-library-tool

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

# `skills/` is not a pnpm workspace and deliberately isn't one — nothing in this
# repo imports it (see knip.jsonc). Its payload still carries logic worth
# pinning: the report generator the validation agent invokes decides a run's
# verdict. Its tests run from here rather than nowhere, guarded on a match
# because `node --test` with no arguments discovers the whole tree instead.
# Every Go loop below records the first failure and exits non-zero at the end.
# A bare `for` in make keeps only the LAST iteration's status, so a failure in any
# module but the last was reported as a green verb — which is exactly how a red
# `go vet` in aep-api passed as a clean `make typecheck`. The loops still run every
# module, because knowing about one broken module should not hide the next.
# manifests-check is a PREREQUISITE, not a recipe line: the `exit $$rc` below
# ends the recipe, so anything appended after it never runs.
test: gen manifests-check
	$(TURBO) run test
	@rc=0; for d in $(GO_MODULE_DIRS); do echo ">> go test $$d"; ( cd "$$d" && go test ./... ) || rc=1; done; exit $$rc
	@files=$$(find $(ROOT)/skills $(ROOT)/deployments/scripts -name '*.test.mjs' | sort); \
	  if [ -n "$$files" ]; then echo ">> node --test skills + deployments/scripts"; node --test $$files; fi

# Platform manifests that exist twice: once under deployments/manifests (applied
# by the setup scripts with kubectl) and once as a verbatim copy in the platform
# Helm chart (what a real installation renders). Nothing derives one from the
# other, so only a check keeps them honest. Runs as part of `make test`, which
# is what CI runs.
manifests-check:
	@bash $(ROOT)/deployments/scripts/check-trait-copies.sh

# Local coverage summary — coverage is not gated in CI. Go: the aep-api module's fast-lane
# cover target (-short, no Docker). TS: @aep/ae-design-agent via node:test's
# --experimental-test-coverage. Report-only — the TS side never fails the verb,
# and spends no tokens. Extend module-by-module as other packages grow tests.
cover:
	@echo ">> Go coverage — services/aep-api (fast lane, -short)"
	@$(MAKE) -C services/aep-api cover || true
	@echo ""
	@echo ">> TS coverage — @aep/ae-design-agent (node:test --experimental-test-coverage)"
	@$(PNPM) --filter @aep/ae-design-agent exec node --experimental-test-coverage --import tsx --test "test/**/*.test.ts" 2>/dev/null \
		| grep -E '^# (tests|pass|fail|all files)' || echo "  (TS coverage unavailable — run 'pnpm --filter @aep/ae-design-agent test' to debug)"

# Spec-agent evals (evals/spec-agents). On-demand only — never wired into CI.
#   make eval                 run every eval (real model calls, costs money)
#   make eval EVAL=<file>     one eval file, e.g. EVAL=evals/requirements.eval.ts
#   make eval-ui              run once + serve the local results UI
eval:
	$(PNPM) --filter @aep/spec-agent-evals eval $(if $(EVAL),$(EVAL),)

eval-ui:
	$(PNPM) --filter @aep/spec-agent-evals eval:ui

# Ballerina coding evals: host mode, your own `claude login`, on demand.
# Needs an installed `bal library` — packages/bal-library-tool/install-local.sh.
eval-bal:
	$(PNPM) --filter @aep/ballerina-evals eval $(if $(ARGS),-- $(ARGS),)

lint:
	$(TURBO) run lint
	@rc=0; for d in $(GO_MODULE_DIRS); do echo ">> golangci-lint $$d"; ( cd "$$d" && $(GOLANGCI) run ./... ) || rc=1; done; exit $$rc

typecheck: gen
	$(TURBO) run typecheck
	@rc=0; for d in $(GO_MODULE_DIRS); do echo ">> go vet $$d"; ( cd "$$d" && go vet ./... ) || rc=1; done; exit $$rc

license:
	@git ls-files | $(LICENSE_MATCH) | tr '\n' '\0' | xargs -0 $(ADDLICENSE) -f $(LICENSE_HEADER)

license-check:
	@git ls-files | $(LICENSE_MATCH) | tr '\n' '\0' | xargs -0 $(ADDLICENSE) -check -f $(LICENSE_HEADER)

tools:
	GOTOOLCHAIN=$(GO_TOOLCHAIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

# TS dead-code gate (knip) — the counterpart of services/aep-api's Go
# `deadcode-check`. Whole-program unused-export/file/dependency analysis over the
# agents runtime, the collab server + the playground that consumes the agents,
# run with --production so *.test.ts never count as consumers. Config + rationale live in knip.jsonc.
#   make deadcode-ts        human report (never fails)
#   make deadcode-ts-check  CI gate (fails on any finding)
deadcode-ts:
	$(PNPM) run deadcode-ts

deadcode-ts-check:
	$(PNPM) run deadcode-ts:check

# Local-dev helper (not a uniform verb): build + k3d-import the runner images
# only — `make build-runner FORCE=1` to rebuild over an existing tag. The
# release keeps its images until `make dev-runner` points it at them.
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

# ── Local in-cluster dev (aectl + k3d) ───────────────────────────────────────
# Installs the platform the same way a real user does: via the `aectl` CLI
# (tools/aectl), against a bare upstream OpenChoreo + ThunderID cluster — no
# dependency on this repo's own legacy setup.sh chain.
#
# Built as aectl-skaffold, not aectl: this binary is this local-dev flow's own
# copy (git-ignored, tools/aectl/.gitignore), kept distinct by name from a
# developer's own `aectl` build in the same directory.
#
# One-time bootstrap: the bare cluster (deployments/scripts/setup-env-for-aectl.sh),
# aectl's own Thunder admin client (WITH_SKAFFOLD_CLIENT=1 — see that script's
# step 3c for why this can't be registered by aectl itself), then
# `aectl platform config import` + `aectl platform install` against the local chart,
# then Agent Manager beside it.
#
# Agent Manager runs LAST and from here rather than being left to the operator,
# because the two products share one identity provider and one environment tier:
# a cluster with only AEP on it cannot exercise either sharing decision, so the
# convergence is only actually tested when both are installed. Its step 4 also
# hands two OpenChoreo objects to Helm, which has to happen after
# `platform install` has finished patching them. Its last step provisions the
# environment's AI gateway, so an agent deployed here reaches its model through
# Agent Manager rather than calling Anthropic directly.
#
# WITH_AGENT_MANAGER=0 skips it, for when the AEP half is all that is being
# worked on — it is the slowest step here by a wide margin. WITH_AI_GATEWAY=0
# keeps Agent Manager but skips the gateway alone.
#
# The SRE agent (deployments/scripts/setup-sre.sh) then wires the OpenChoreo
# SRE agent's alert → RCA → AE issue handoff onto the observability plane
# (OpenSearch, Fluent Bit, the logs adapter) the cluster bring-up installed.
# It uses the org's model connection key (an Anthropic one) as saved in the
# Console, so its pod waits until that key is saved; re-run setup-sre.sh after
# saving it.
#
#   WITH_OBSERVABILITY=0  skips the observability plane, and with it the SRE
#                         agent (the agent has nothing to read alerts from)
#   WITH_SRE=0            keeps the plane but skips the SRE agent, and parks
#                         the plane's heavy half last (park-observability.sh)
#                         since nothing then reads it
#
# An SRE-only profile that saves the most memory:
#   WITH_AGENT_MANAGER=0 make dev-env
#
# `platform install` otherwise prompts interactively for two secrets — set as
# env vars here so it doesn't:
#   ANTHROPIC_API_KEY               platform.go treats an EMPTY value the same
#                                    as unset (still prompts, then refuses) —
#                                    "none" is a non-empty placeholder, not a
#                                    real key. No platform fallback reads it;
#                                    orgs connect their own in Settings.
#   AEP_THUNDER_ADMIN_CLIENT_SECRET the secret for ae-install-client, the same
#                                    client WITH_SKAFFOLD_CLIENT=1 bootstraps
#                                    above (skaffold/defaults.yaml's
#                                    thunder.admin_client_id)
#
# The install runs on images built from THIS checkout (dev-images below, then
# `--image-tag=dev-local`), never the released ones the chart defaults to.
# A local chart with released images is a cluster running two different
# versions of the platform: the Deployment templates come from your branch
# while the containers come from whatever main last published, and the two
# disagree the moment a branch changes the contract between them. Templates
# pass env vars an older image's entrypoint drops on the floor, and the
# failure surfaces somewhere else entirely — the ae:* resource-indicator
# work (ADR-0039) showed up as a wrong-audience 401 in an aep-api log, three
# systems away from the stale console image that caused it.
#
# This is why the build sits between cluster bring-up and `platform install`
# rather than after it: skaffold needs the k3d cluster to import into, and
# the install needs the images to already be there, so the pods come up on
# them first time instead of pulling `:latest` and being swapped afterwards.
# The DNS suffix every hostname on the cluster is composed onto. This flow is
# localhost and nothing else, and dev-env enforces that below rather than
# accepting an override it cannot honour: the config it imports two lines
# later, skaffold/defaults.yaml, is pinned to localhost URLs. An override here
# would re-domain the CLUSTER while the PLATFORM installed onto it kept
# console.ae.localhost, thunder.openchoreo.localhost and an environment tier
# to match — a split-brain install that comes up green and fails at the first
# sign-in. A cluster on any other suffix runs the scripts directly, with a
# config file carrying the same suffix (deployments/README.md).
AE_DOMAIN ?= localhost

dev-env:
	@if [ "$(AE_DOMAIN)" != "localhost" ]; then \
		echo "❌ dev-env is the localhost flow: it imports skaffold/defaults.yaml, whose URLs are localhost." >&2; \
		echo "   AE_DOMAIN=$(AE_DOMAIN) would re-domain the cluster and leave the platform on localhost." >&2; \
		echo "   For another suffix, run the scripts directly with a matching config — see deployments/README.md." >&2; \
		exit 1; \
	fi
	cd tools/aectl && go build -o aectl-skaffold .
	AE_DOMAIN=$(AE_DOMAIN) WITH_SKAFFOLD_CLIENT=1 bash deployments/scripts/setup-env-for-aectl.sh
	$(MAKE) dev-images
	./tools/aectl/aectl-skaffold platform config import --config skaffold/defaults.yaml
	ANTHROPIC_API_KEY=none AEP_THUNDER_ADMIN_CLIENT_SECRET=ae-install-client-secret \
		./tools/aectl/aectl-skaffold platform install --addons=all --platform-version=latest --platform-chart=deployments/helm-charts/platform --image-tag=dev-local
	@if [ "$${WITH_AGENT_MANAGER:-1}" != "1" ]; then \
		echo "⏭️  Skipping Agent Manager (WITH_AGENT_MANAGER=0)"; \
	elif [ "$${WITH_OBSERVABILITY:-1}" != "1" ]; then \
		echo "⏭️  Skipping Agent Manager: it installs against the observability plane (WITH_OBSERVABILITY=0)"; \
	else \
		AE_DOMAIN=$(AE_DOMAIN) bash deployments/scripts/setup-agent-manager.sh; \
	fi
	@if [ "$${WITH_OBSERVABILITY:-1}" != "1" ]; then \
		echo "⏭️  Skipping the SRE agent (WITH_OBSERVABILITY=0)"; \
	elif [ "$${WITH_SRE:-1}" = "1" ]; then \
		bash deployments/scripts/setup-sre.sh; \
	else \
		echo "⏭️  Skipping the SRE agent (WITH_SRE=0)"; \
	fi
	@if [ "$${WITH_OBSERVABILITY:-1}" = "1" ] && [ "$${WITH_SRE:-1}" != "1" ]; then \
		bash deployments/scripts/park-observability.sh down; \
	fi

# Builds the six platform service images from this checkout and loads them
# into the k3d cluster, tagged dev-local (skaffold.yaml — build-only). Shared
# by `dev-env`, which installs onto them, and `dev-update`, which re-points an
# already-installed release at them; the list of images lives here once so the
# two cannot drift apart.
#
# Needs the cluster to exist: skaffold is given its kube-context, and the
# import targets it by name.
dev-images:
	skaffold build --kube-context k3d-openchoreo -f skaffold.yaml
	# skaffold's own build cache lives in the HOST docker daemon, not the k3d
	# cluster's containerd — a cache hit ("Found Locally") skips its internal
	# k3d-import too, so a recreated/fresh cluster silently never receives an
	# image skaffold thinks is already cached. Import explicitly every run,
	# cache hit or not; re-importing an image the cluster already has is cheap.
	k3d image import \
		ghcr.io/wso2/aep/aep-api:dev-local \
		ghcr.io/wso2/aep/agents:dev-local \
		ghcr.io/wso2/aep/collab:dev-local \
		ghcr.io/wso2/aep/aep-mcp-server:dev-local \
		ghcr.io/wso2/aep/console:dev-local \
		ghcr.io/wso2/aep/tryit:dev-local \
		--cluster openchoreo

# The observability plane's heavy half (OpenSearch, Prometheus, collectors,
# adapters): `make dev-env` installs it running and parks it last, unless the
# SRE agent runs (the default), which needs OpenSearch, Fluent Bit and the logs
# adapter up to evaluate alerts. Unpark to read traces, metrics or archived
# logs, between builds on an 8 GiB VM.
obs-park:
	bash deployments/scripts/park-observability.sh down
obs-unpark:
	bash deployments/scripts/park-observability.sh up
obs-status:
	bash deployments/scripts/park-observability.sh status

# Edit source, then run this: rebuilds only the images whose dependencies
# changed (dev-images above), then re-points the aep-platform release at them
# directly via `helm upgrade --reuse-values` — a plain CLI flag skaffold's own v4beta11
# HelmRelease schema has no field for (see skaffold.yaml's header), and
# load-bearing: without it this would reset every value `aectl platform
# install` set (Thunder/OpenBao/webhook URLs, etc.) back to chart defaults.
#
# The tag is always the same literal string (dev-local), so a repeat
# `helm upgrade --set image.tag=dev-local` is byte-identical to the Deployment
# spec already running — Helm sees no diff and never recreates the pod, even
# though `k3d image import` just overwrote what dev-local points to in
# containerd. The explicit `kubectl rollout restart` below is what actually
# picks up the new content; without it every dev-update after the first is a
# no-op as far as the running pods are concerned.
#
# One-shot, not a watch loop. Run after `make dev-env`.
# Console: http://console.ae.localhost:8080
#
# Named dev-update, not dev: `make dev` is the uniform verb (turbo run dev,
# host-side TS dev servers per package) and already means something else.
dev-update:
	$(MAKE) dev-images
	helm upgrade aep-platform deployments/helm-charts/platform -n wso2-aep --reuse-values \
		--set aepApi.image.repository=ghcr.io/wso2/aep/aep-api --set aepApi.image.tag=dev-local \
		--set aepAgents.image.repository=ghcr.io/wso2/aep/agents --set aepAgents.image.tag=dev-local \
		--set collab.image.repository=ghcr.io/wso2/aep/collab --set collab.image.tag=dev-local \
		--set aepMcpServer.image.repository=ghcr.io/wso2/aep/aep-mcp-server --set aepMcpServer.image.tag=dev-local \
		--set console.image.repository=ghcr.io/wso2/aep/console --set console.image.tag=dev-local \
		--set tryIt.image.repository=ghcr.io/wso2/aep/tryit --set tryIt.image.tag=dev-local
	kubectl -n wso2-aep rollout restart deployment/aep-api deployment/aep-agents deployment/collab-server deployment/aep-mcp-server deployment/aep-console deployment/aep-tryit

# Builds the coding-agent runner images from this checkout (Claude Code and
# OpenCode, deployments/scripts/build-runner.sh), imports them into k3d, and
# points the aep-platform release at them. The chart's defaults are the
# released ghcr image and no OpenCode image, which takes OpenCode off the
# runtime menu. Run after `make dev-env`, and again after changing
# runners/remote-worker or a package it bakes in (agent-eval, web-search,
# skills, bal-library-tool). Like dev-update, --reuse-values keeps aectl's
# settings; the image values change, so Helm rolls aep-api by itself.
dev-runner:
	FORCE=1 bash deployments/scripts/build-runner.sh
	helm upgrade aep-platform deployments/helm-charts/platform -n wso2-aep --reuse-values \
		--set codingAgentRunner.image=aep-runner:dev \
		--set codingAgentRunner.opencodeImage=aep-runner-opencode:dev

clean:
	$(TURBO) run build --force >/dev/null 2>&1 || true
	rm -rf .turbo
	find . -type d -name dist -prune -not -path './node_modules/*' -exec rm -rf {} + 2>/dev/null || true
	find . -type d -name .turbo -prune -not -path './node_modules/*' -exec rm -rf {} + 2>/dev/null || true
	@for d in $(GO_MODULE_DIRS); do ( cd "$$d" && go clean ./... ); done
