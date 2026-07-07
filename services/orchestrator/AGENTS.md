# AGENTS.md — services/orchestrator

The Temporal **worker**: the only process that registers and executes the
development-flow orchestration. `aep-api` is a thin client (start/signal/query);
it runs no worker.

## Layout
- `cmd/worker/main.go` — single entry point (builds client, registers workflows
  + activities, runs the worker).
- `internal/config` — env parsing in one place (`TEMPORAL_*`, downstream URLs).
- `internal/temporal` — Temporal client factory. `internal/worker` — worker bootstrap.
- `internal/workflows` — **pure, deterministic** workflow code (no I/O imports).
- `internal/activities` — all side effects (dispatch k8s Jobs, build, read-models, git).

## Rules
- Workflow boundary constants come from `packages/contracts/orchestration` — never
  hand-defined here.
- Determinism: `internal/workflows` must not import HTTP/DB/`time.Now`/`os`/`uuid`.
- Activities are idempotent (Temporal retries them).

Module: `github.com/wso2/labs-agentic-engineer/services/orchestrator` (a `go.work` member).
Design: `docs/design/orchestration/`.
