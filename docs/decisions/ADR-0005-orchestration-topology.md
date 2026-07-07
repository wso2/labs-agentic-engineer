# ADR-0005: Temporal-native development-flow orchestration topology

- **Status:** Accepted
- **Date:** 2026-06-18
- **Context:** the AEP rewrite replaces the old UI-button + webhook-projector +
  polling-watcher orchestration with a durable workflow engine. Because the
  rewrite is a clean slate (no `services/aep-api` ported yet), Temporal can be
  the **primary** orchestrator from day one rather than a dual-write shadow (the
  approach `main` was forced into). Full design: `docs/design/orchestration/`.

## Decision

| Concern | Decision |
|---|---|
| Engine | **Temporal**, primary orchestrator (the workflow *is* the state machine) |
| State ownership | Temporal owns flow position (its own datastore); `database` holds UI read-models written by idempotent activities; git tags own artifacts |
| Worker placement | **standalone `services/orchestrator`** runs the only worker; `aep-api` is a dial-only client (start/signal/query) |
| Topology | two tiers: **`DevelopmentFlowWorkflow`** (one instance per change *cycle*) → **`TaskLifecycleWorkflow`** (one child per task). No standing project coordinator. |
| Unit of orchestration | a **change cycle** (`requirements → design → implement → merge → complete`, with iterate-back), **not** a long-lived per-project workflow. New requirement/issue = new cycle instance (same type). |
| Boundary contract | signal/query names, task-queue, workflow-ID builders in `packages/contracts/orchestration` (imported by both services) |
| Gate modes | per-stage `GatePolicy` (`human` \| `auto`); autonomous = all `auto` (with checks); same workflow |
| DAG scheduling | dependents spawn once deps reach `deployed`; independent tasks run in parallel |
| Per-org cap | k8s Job per task + `ResourceQuota` on `wc-<org>-remote-worker`; dispatch activity retriable on quota-exceeded. **No limiter workflow.** |
| Cascade (CORS / env-config wiring) | **deferred** — workflow does dependency-dispatch ordering only for now |
| Multi-tenancy | org in every workflow ID + an `org` Search Attribute; single namespace; stateless horizontally-scalable workers |

## Consequences
- One fewer moving part than `main`: no DB state-machine table, no polling
  watchers, no advisory locks, no per-org limiter workflow.
- `aep-api` and `orchestrator` must agree on the boundary — enforced by the
  shared `orchestration` Go module (compile-time, not stringly-typed).
- Real activities depend on the `database`/`aep-api` ports; the workflow
  skeletons + tests land first (mocked activities), the real activities later.
