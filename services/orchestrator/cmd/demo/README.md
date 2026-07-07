# Orchestrator demo

A self-contained end-to-end driver for the development-flow orchestration. It
starts an **in-process Temporal worker** (wired with in-memory fakes — a design
of `api` + `web` that depends on `api`) and drives two real cycles against the
local dev server:

- **`devflow:acme:web:demo-human`** — human-gated: approvals advance requirements
  and design; PRs are merged by hand.
- **`devflow:acme:web:demo-auto`** — autonomous: every gate is `auto`, so the
  cycle advances with no approvals (the fake checks pass) and PRs auto-merge.

Each cycle spawns two task children (`task:acme:web:api`, then `task:acme:web:web`
once `api` is deployed — the dependency DAG) and drives each through the full
task lifecycle: `PR → build → deploy → deployed`.

## Prerequisites
- Docker (for the Temporal dev server) and Go 1.26.

## Start
```bash
# 1. Temporal dev server (gRPC 7233, Web UI 8233)
cd deployments && docker compose up -d temporal

# 2. run the demo (from the orchestrator module)
cd ../services/orchestrator && go run ./cmd/demo
```
The program prints a narrated trace and exits when both cycles complete. Open the
**Web UI → http://localhost:8233** (namespace `default`) to browse the 6 resulting
workflows: the two `devflow:…` parents and their `task:…` children. Click a parent
→ its history/graph shows the phase loop, signals, activities, and child spawns.

## Manual mode — approve the gates yourself
```bash
cd services/orchestrator && go run ./cmd/demo -manual
```
Runs a single human-gated cycle (`devflow:acme:web:manual-1`) and **pauses at each
human gate** until *you* approve it. The program never sends the approvals itself —
it polls the cycle's phase, so it advances no matter how you approve:

- **Terminal:** press **[Enter]** at the `⏸ phase=…` prompt → it sends the signal.
- **Temporal Web UI:** open the workflow at http://localhost:8233 → **"Send a
  Signal"** → enter the signal name (no payload):
  - `requirements` → `ApproveRequirements`
  - `design` → `ApproveDesign`
  - `merge` → `MarkComplete`

You can mix the two (e.g. approve requirements from the UI, design from the
terminal). The build/deploy task events are **not** human gates, so they are still
driven automatically once the design gate is approved.

## Stop
```bash
cd deployments && docker compose down
```
The dev server is in-memory (no persistence configured), so this also clears all
demo workflow executions.

## Reset to a clean set of 6
```bash
cd deployments && docker compose down && docker compose up -d temporal
cd ../services/orchestrator && go run ./cmd/demo
```

## Notes
- The demo runs its **own** worker. Don't run `cmd/worker` against the same dev
  server at the same time — with no real adapters wired it has nil (no-op)
  dependencies and would compete for tasks on the `aep-orchestrator` queue.
- This is a developer tool, not part of the deployed system. The real worker is
  `cmd/worker`; its activity adapters are wired in `internal/deps` (O4-real).
