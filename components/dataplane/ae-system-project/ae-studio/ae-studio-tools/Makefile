.PHONY: build test deadcode deadcode-check

build:
	go build ./...

# -short matches services/aep-api's fast lane.
test:
	go test -short ./...

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
