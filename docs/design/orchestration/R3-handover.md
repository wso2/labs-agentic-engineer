# R3 Handover: Swap the lower tier to TaskLifecycleWorkflow

Context for whoever picks up R3. Read this, then the full plan at
`~/.claude/plans/users-gayanka-downloads-ai-first-toolin-warm-floyd.md`
(§ "R3 — Swap the lower tier to TaskLifecycleWorkflow behind preserved dispatch").
That file has the target topology, phased plan, and file anchors. This document
fills the gaps the plan doesn't cover: *why* things were built the way they
were in R0–R2, and the traps to avoid.

## Where things stand (branch `aep-rewrite`)

Commits, oldest first:
- `f0dd536` — R0: ported `services/orchestrator` + `packages/contracts/orchestration`
  from the `rewrite` branch. Promoted the shared DTOs into the contract module.
  Renumbered the orchestration ADRs to **0005/0006** (this branch already had
  0003/0004 for unrelated dependency-resolution topics).
- `fab8a66` — R1: aep-api's dial-only Temporal client
  (`internal/feature/orchestration/client.go`) + `TemporalConfig` + the
  `development_cycles` read-model table/repo.
- `384a368` — R2: `internal/feature/cycle` (Service + Huma REST surface) wired
  to the real requirements/design approval hooks. **Upper tier (the cycle) is
  live.** Lower tier (tasks) is **still on the old Funnel/Execution engine** —
  R3 is what replaces it.
- `8641b88` — regenerated OpenAPI spec (separate commit; folded in a
  pre-existing stale-spec drift unrelated to Temporal — ignore that noise).

Also done after R2 (uncommitted-at-time-of-writing, check `git log` /
`git status` to confirm current state): added an `orchestrator` service +
`Dockerfile` to `deployments/docker-compose.yml` so the worker actually runs
alongside `aep-api`/`console`/`temporal` under `scripts/start.sh`. **Verify
this landed** — if `services/orchestrator/Dockerfile` and the `orchestrator:`
block in `docker-compose.yml` are missing, the console will hang on any save
(workflow starts but nothing polls the task queue to advance it).

## The architectural decision R3 executes

`aep-rewrite` had **already built** a complete GitHub-native reactive task
engine before this Temporal work started:

| Concern | Reactive engine (to be replaced) | Anchor |
|---|---|---|
| Task state | `DerivedTaskStatus`, computed live from GitHub + `executions` rows | `internal/contracts/taskmeta/derive.go` |
| Dispatch | **Funnel** — imperative gate checks + executor registry | `internal/feature/execution/funnel.go:53` (`OnExecuteIntent`) |
| Concurrency cap | Partial unique index `ux_executions_admission` (admission mutex) | `internal/database/migrations/executions.go:37` |
| Reconciliation | Sweep (60s poll) + ExecWatcher + JobWatcher | `execution/sweep.go`, `codingagent/{exec_watcher,watcher}.go` |
| Actual dispatch work | `CodingExecutor.Run` → cluster-gateway-proxy Job apply (namespace ensure, ExternalSecrets from SM-API, credential minting) | `internal/feature/codingagent/coding_executor.go:164` |

The user chose **strategy B**: Temporal owns *both* tiers, replacing Funnel/
Sweep/watchers/admission-mutex entirely — not a dual-write, not a thin mirror.
R3 is the phase that does this for the **task** tier (R2 already did the
**cycle** tier).

**Non-negotiable design constraint**: `CodingExecutor.Run` and everything it
touches (namespace ensure, SM-API ExternalSecrets, cluster-gateway-proxy Job
apply, credential minting) is deeply wired into aep-api's credential store,
OC clients, and git host. **Do not re-port this into the orchestrator.**
Instead: orchestrator activities are **thin HTTP clients** of new aep-api
`/internal/v1/*` S2S endpoints that wrap the *preserved* dispatch code. aep-api
stays the one integration owner; the orchestrator stays pure flow-state. This
was confirmed with the user explicitly — don't re-litigate it.

## What R3 actually needs to do (see plan for full detail)

1. IMPLEMENT phase spawns `TaskLifecycleWorkflow` children, DAG-ordered by the
   design's `DependsOnComponents` (dependent starts only once deps reach
   `deployed`).
2. New aep-api `/internal/v1/*` endpoints the orchestrator's activities call:
   `dispatch` (→ `CodingExecutor.Run`, **minus** the Funnel's gating — gating
   moves into the workflow), `plan-tasks` (→ `task.PlanService`), `read-design`,
   `gate-check` (auto-mode tests/lint/self-review), `build`, `deploy`,
   `readmodel-upsert`. Auth: reuse `auth.ExecutionScopedInput`
   (Task-JWT/publisher-cc) — see `internal/feature/execution/skills_s2s.go:125`
   and `internal/feature/orgcreds/credentials_internal_huma.go:40` for the
   existing pattern of an `/internal/v1/executions/{executionId}/*` route.
3. Completion signals: port `JobWatcher`/`ExecWatcher` poll logic into
   heartbeating activities, or rewire the webhook router's task handlers to
   `SignalWorkflow(TaskWorkflowID(...))` (PRReady/PRMerged/BuildSucceeded/
   DeploySucceeded/...) instead of calling into the Funnel.
4. Concurrency cap: the `dispatch` activity ensures a k8s `ResourceQuota` (+
   `LimitRange`) on the `wc-<org>-...-remote-worker` namespace; treats
   quota-exceeded as **retriable** so Temporal backs off naturally — this
   replaces the admission-mutex partial unique index. Platform-touching:
   per repo convention, route ResourceQuota provisioning through
   `platform-design-expert` + a cluster-health pre-flight (do not just apply
   it blind).
5. `executions` table becomes a **read-model** (UPSERT via the
   `readmodel-upsert` activity, `ON CONFLICT (workflow_id, version)`) — still
   serves the task-list UI; tasks stay GitHub issues (no new task table).
6. **Do not delete the Funnel/Sweep/watchers yet** — that's R4. R3 swaps what
   the workflow *drives*; R4 removes the now-dead reactive engine.

## Patterns established in R0–R2 — reuse these, don't reinvent

- **Consumer-side ports, not concrete imports.** `internal/feature/cycle`
  depends on a narrow `WorkflowClient` interface (satisfied by the concrete
  orchestration client at the composition root in `app.go`), not the concrete
  type. This keeps `internal/arch/arch_test.go`'s `featureEdgeAllowlist` clean
  and makes the service unit-testable with a fake — see
  `internal/feature/cycle/service_test.go`. Do the same for whatever service
  ends up calling into the new `/internal/v1/*` task-dispatch endpoints.
- **`featureEdgeAllowlist` is enforced by a test.** Any new cross-feature Go
  import needs a row (with rationale in the comment) in
  `internal/arch/arch_test.go`, or `TestFeatureEdgeAllowlist` fails the build.
  Check this early, not at the end.
- **Nil-safe optional hooks, wired post-construction.** R2 added
  `SetCycleHook` on `requirementsService`/`designService`, mirroring the
  existing `SetTaskService`/`SetProvisionIssueMinter` pattern (see
  `design_service.go:181` for the precedent). If R3 needs to hook a save/build
  path into the workflow, follow this pattern — never make the hook a
  constructor-required dependency, and never let a hook failure fail the
  underlying operation (`slog.Warn`, don't `return err`).
- **The typed-nil-interface trap.** In `app.go`, the Temporal client variable
  is declared as the **interface type** (`cycle.WorkflowClient`), not the
  concrete `*orchestration.Client`, specifically so that "dial failed" leaves
  it as a **true nil interface** — a nil-check on a nil *concrete pointer*
  stored in an interface would NOT compare equal to `nil`. Keep this pattern
  for any new orchestration-dependent service.
- **Non-fatal Temporal dial.** `app.go`'s `orchestration.New(cfg.Temporal)`
  failure only logs a warning; the BFF still boots with orchestration
  disabled. Don't make R3's dispatch path fail aep-api startup either.
- **Idempotent repository `Ensure`.** `DevelopmentCycleRepository.Ensure` uses
  `ON CONFLICT DO NOTHING` keyed on the unique `workflow_id`, then re-fetches
  on the losing race — see `repositories/development_cycle_repository.go`.
  Follow this for any new read-model upsert in R3 (the `readmodel-upsert`
  activity target).
- **`StartCycle` swallows `WorkflowExecutionAlreadyStarted`.** The client
  treats a retried start as a no-op success (returns the deterministic ID, no
  error) rather than a typed sentinel error — simpler for callers. Consider
  the same for any new "ensure this task workflow is running" call.
- **Workflow-name drift guard.** Workflows are started **by name string**
  (`contract.WorkflowDevelopmentFlow` / `WorkflowTaskLifecycle` constants in
  `packages/contracts/orchestration/orchestration.go`), and the worker
  registers them under the same names via
  `workflow.RegisterOptions{Name: ...}` in
  `services/orchestrator/cmd/worker/main.go`. aep-api never imports the
  orchestrator's `internal/workflows` package. Any new signal/query/workflow
  name needs a constant in the contract module, not a hand-typed string.
- **OpenAPI spec is generated, never hand-edited.** After adding
  `/internal/v1/*` endpoints (well — internal endpoints likely aren't in the
  *public* spec; check `internal/api/internal.go` vs `surfaces.go` for which
  surface they belong to), run `make -C services/aep-api openapi` and commit
  the regenerated spec **in a separate commit** from the code change (this is
  what R2 did — keeps the diff reviewable). `make -C services/aep-api
  openapi-check` is the drift guard; it must be green before you're done.

## Things that will bite you if you skip them

- **Env var spelling mismatch (real, already present):** the orchestrator
  reads `TEMPORAL_HOSTPORT` (no underscore between HOST and PORT); aep-api
  reads `TEMPORAL_HOST_PORT`. Different services, different spelling. Already
  documented inline in `docker-compose.yml` — don't "fix" one to match the
  other without checking both `services/orchestrator/internal/config/config.go`
  and `services/aep-api/internal/config/config.go` first (changing either
  breaks the other's existing env var contract).
- **Module prefix divergence:** `packages/contracts/orchestration` and
  `services/orchestrator` use `github.com/wso2/labs-agentic-engineer/...`
  (per ADR-0001), but `services/aep-api`'s module is
  `github.com/wso2/aep/aep-api` (diverged before this work started — not
  something to "fix" as part of R3). aep-api's `go.mod` has a `replace`
  directive pointing the orchestration module at its local path — required
  because `go.work` covers workspace *builds* but not full module-graph
  resolution (`go mod tidy`) for an unpublished module.
  See ADR-0001's "no replace" rule — this is the documented exception.
  Docker builds of `services/orchestrator` need **repo-root build context**
  (`context: ..` in compose) precisely because of this relative `replace` —
  mirror `services/orchestrator/Dockerfile` and its compose block for any new
  Go service with the same cross-module dependency.
- **`go.temporal.io/api/serviceerror`** is the package with
  `WorkflowExecutionAlreadyStarted` — used via `errors.As`, not a sentinel
  `errors.Is` comparison (Temporal's SDK wraps it as a typed error, not a
  singleton).
- **Determinism boundary is real and tested.** `internal/workflows` in
  `services/orchestrator` must never import HTTP/DB/`time.Now`/`os`/`uuid`.
  There's a `WorkflowReplayer` test
  (`internal/workflows/replay_test.go` + `testdata/task_lifecycle_happy.json`)
  that will fail if a workflow code change isn't backward-compatible with
  the fixture's recorded history — see ADR-0006 (workflow versioning) for the
  `workflow.GetVersion` pattern to use when changing `TaskLifecycleWorkflow`.
  R3's DAG-scheduling change inside `DevelopmentFlowWorkflow.runImplement`
  is exactly this kind of edit — expect to regenerate the replay fixture and
  read ADR-0006 before touching it.
- **Test lane discipline.** `go test -short` skips real-Postgres dbtests
  (`internal/platform/dbtest`); Docker must be running for the real dbtest
  lane. `make -C services/aep-api cover` / `go test ./...` (no `-short`) runs
  them. R1/R2 used both lanes — do the same for any new repository code.

## Suggested first steps for the R3 agent

1. `git log --oneline -10` and read `f0dd536`, `fab8a66`, `384a368` in full
   (`git show <sha>`) to see the actual code, not just this summary.
2. Confirm the `orchestrator` docker-compose service exists and
   `bash deployments/scripts/start.sh` brings up a working console + BFF +
   Temporal + orchestrator stack; do one manual "save requirements → approve
   design" click-through and watch the cycle advance in the Temporal UI
   (`:8233`) before touching any task-tier code. This proves R0–R2 are still
   healthy before you build on them.
3. Re-read `internal/feature/execution/funnel.go` and
   `internal/feature/codingagent/coding_executor.go` in full — R3 depends on
   precisely preserving their side-effect logic while removing their
   gating/orchestration role.
4. Start with the `/internal/v1/*` dispatch endpoints (aep-api side) before
   the orchestrator's `TaskLifecycleWorkflow` DAG wiring — the plan's R3
   ordering front-loads the endpoints because the workflow activities need
   them to exist first to test against.
