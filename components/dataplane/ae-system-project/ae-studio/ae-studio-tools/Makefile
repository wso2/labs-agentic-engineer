.PHONY: build test gen-api gen-api-check deadcode deadcode-check

build:
	go build ./...

# -short matches services/aep-api's fast lane.
test:
	go test -short ./...

# ── Contract-first internal API codegen ──────────────────────────────────────
# The committed contract (packages/contracts/api/ae-studio-tools/internal/v1) is
# the SOURCE OF TRUTH; gen-api regenerates the models, the strict server and the
# embedded spec (GetSpec, read by the request validator) into internal/gen.
# Generated files are committed; gen-api-check is the CI freshness gate. The
# /v1 contract has no operations yet, so nothing is generated from it.
OAPI_CODEGEN          ?= go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.0
INTERNAL_CONTRACT_DIR  = ../../../../../packages/contracts/api/ae-studio-tools/internal/v1

gen-api:
	@$(OAPI_CODEGEN) -config internal/gen/oapi-codegen-internal.yaml $(INTERNAL_CONTRACT_DIR)/openapi.yaml
	@gofmt -w internal/gen/internal_gen.go
	@echo "internal API types + strict server (with embedded spec) regenerated"

# CI freshness gate: regenerate, then fail if anything tracked drifted (a
# contract edit without `make gen-api`, or a hand-edit of generated output).
# --others catches a generated file that is not committed yet.
gen-api-check: gen-api
	@if ! git diff --exit-code --quiet -- internal/gen/ || [ -n "$$(git ls-files --others --exclude-standard -- internal/gen/)" ]; then \
		git status --short -- internal/gen/ | cat; \
		echo ""; \
		echo "Generated API code is stale w.r.t. packages/contracts/api/ae-studio-tools/internal/v1."; \
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
