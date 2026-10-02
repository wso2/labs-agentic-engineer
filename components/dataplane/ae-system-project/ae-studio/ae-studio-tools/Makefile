.PHONY: build test gen-api gen-api-check deadcode deadcode-check

build:
	go build ./...

# -short matches services/aep-api's fast lane.
test:
	go test -short ./...

# ── Contract-first API codegen ───────────────────────────────────────────────
# The committed contracts under packages/contracts are the SOURCE OF TRUTH.
# gen-api regenerates, each into its own sub-package of internal/gen:
#   gen/             /internal/v1  models + strict server + embedded spec (aep-api caller)
#   gen/v1           /v1           models + strict server + embedded spec (browser)
#   gen/filessock    Files socket  models + strict server + embedded spec (ae-collab caller)
#   gen/aepapi       aep-api internal spec, the two ae-studio ops: client + models
# The embedded specs (GetSpec) feed the request validators. Generated files are
# committed; gen-api-check is the CI freshness gate.
OAPI_CODEGEN          ?= go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.0
CONTRACTS_DIR          = ../../../../../packages/contracts
INTERNAL_CONTRACT_DIR  = $(CONTRACTS_DIR)/api/ae-studio-tools/internal/v1

gen-api:
	@$(OAPI_CODEGEN) -config internal/gen/oapi-codegen-internal.yaml $(INTERNAL_CONTRACT_DIR)/openapi.yaml
	@$(OAPI_CODEGEN) -config internal/gen/v1/oapi-codegen.yaml $(CONTRACTS_DIR)/api/ae-studio-tools/v1/openapi.yaml
	@$(OAPI_CODEGEN) -config internal/gen/filessock/oapi-codegen.yaml $(CONTRACTS_DIR)/sockets/ae-studio/files/openapi.yaml
	@$(OAPI_CODEGEN) -config internal/gen/aepapi/oapi-codegen.yaml $(CONTRACTS_DIR)/api/internal/v1/openapi.yaml
	@gofmt -w internal/gen/internal_gen.go internal/gen/v1/v1_gen.go internal/gen/filessock/filessock_gen.go internal/gen/aepapi/aepapi_gen.go
	@echo "internal, /v1, Files socket (strict servers + embedded specs) and aep-api client regenerated"

# CI freshness gate: regenerate, then fail if anything tracked drifted (a
# contract edit without `make gen-api`, or a hand-edit of generated output).
# --others catches a generated file that is not committed yet.
gen-api-check: gen-api
	@if ! git diff --exit-code --quiet -- internal/gen/ || [ -n "$$(git ls-files --others --exclude-standard -- internal/gen/)" ]; then \
		git status --short -- internal/gen/ | cat; \
		echo ""; \
		echo "Generated API code is stale w.r.t. its contracts under packages/contracts."; \
		echo "Run 'make gen-api' in this module and commit the changes."; \
		exit 1; \
	fi

# ── Dead-code gate ───────────────────────────────────────────────────────────
# Whole-program reachability from the cmd/ae-studio-tools main via
# golang.org/x/tools/cmd/deadcode (RTA). Tests do NOT count as consumers. The
# script header carries the rationale and the //deadcode:keep marker policy.
#
#   make deadcode        human audit (prints findings, never fails)
#   make deadcode-check  CI gate (fails on any surviving finding)
deadcode:
	@bash scripts/deadcode.sh report

deadcode-check:
	@bash scripts/deadcode.sh check
