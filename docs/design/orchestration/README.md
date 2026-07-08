# Workflow orchestration (Temporal)

> Status: **implemented** on `aep-rewrite` (R0–R5). This replaces the design docs
> that were ported from the `rewrite` branch and the R3/R4 handover notes written
> mid-implementation — this is what actually shipped, as one reference.

The development lifecycle (requirement → design → implement → deploy) and the
per-task lifecycle (dispatch → PR → build → deploy) are driven by two Temporal
workflows running in a standalone `services/orchestrator` worker. `aep-api`
never runs a worker — it only starts, signals, and queries workflows, and
answers S2S calls the workflows' activities make back into it.

This replaced an earlier reactive engine (a webhook-fed projector table +
imperative gate checks in a "Funnel" + a 60s "Sweep" poll + two watcher
goroutines) that had grown organically on this branch before the Temporal work
started. That engine is now deleted, except for two poll loops kept
deliberately in place — see [What's not done](#whats-not-done-r42) below.

---

## The two workflows

### `DevelopmentFlowWorkflow` — one per change cycle

`services/orchestrator/internal/workflows/development_flow.go`

One instance per **cycle** (a requirement version going through
requirements → design → implement → merge → complete), workflow ID
`devflow:<org>:<project>:<cycleN>`. A new requirement or GitHub issue mints a
new `cycleN` and a new workflow instance — cycles for different requirements,
or different projects, run fully concurrently.

```
REQUIREMENTS ⇄ (Revise)
     │ Approve (human signal, or auto gate-check)
     ▼
DESIGN ⇄ (Revise) ── BackToRequirements ──▶ REQUIREMENTS
     │ Approve
     ▼
IMPLEMENT ── BackToRequirements / BackToDesign ──▶ (loop back)
     │ all tasks reach `deployed`
     ▼
MERGE ──▶ COMPLETE (MarkComplete, human or auto)
```

- Each stage gate is `human` (waits on a signal) or `auto` (runs a gate-check
  activity, then advances on its own) — set per cycle via a `GatePolicy`
  (`SetGatePolicy` signal). Autonomous mode is just every gate set to `auto`;
  it is the same workflow and the same states, only who satisfies the gate
  differs.
- IMPLEMENT spawns one `TaskLifecycleWorkflow` child per task
  (`workflow.ExecuteChildWorkflow`), DAG-ordered by the design's
  `DependsOnComponents` — a task starts only once every dependency has reached
  `deployed`. Independent tasks run in parallel; there is no separate
  scheduler or DB lock serializing this.
- Read-only position is exposed via the `GetCycleState` query
  (`GET /projects/{p}/flow-state` → `QueryWorkflow`).

### `TaskLifecycleWorkflow` — one per task

`services/orchestrator/internal/workflows/task_lifecycle.go`

One child instance per task, workflow ID `task:<org>:<project>:<taskID>`.
State is driven entirely by signals — GitHub webhooks on one side, an
`AutoMerge` gate-check on the other:

```
in_progress ── PRReady ──▶ ready_for_review ── PRMerged ──▶ merged ── BuildStarted ──▶ building
    │                            │                                        │
    │ CodingAgentFailed          │ PRRejected / OrgDisconnected           │ BuildSucceeded
    ▼                            ▼                                        ▼
  failed                  rejected / abandoned                        deployed
    ▲
    │ VerificationFailed (from verification_failed, via Retry back to in_progress)
```

`deployed` is a terminal state for the task and is what
`DevelopmentFlowWorkflow`'s DAG scheduler waits on to unblock dependents.
Read-only position is exposed via `GetTaskState`.

---

## How the two services plug together

```
console ──HTTP──▶ aep-api ──(dial-only client)──▶ Temporal Frontend :7233 ──▶ services/orchestrator (worker)
GitHub  ──webhook──▶ aep-api ──(map event → SignalWorkflow)───────────────────▶        │
                        ▲                                                             ▼
                        └─────────── activities call back /internal/v1/orchestration/* ┘
                                     (dispatch, deploy, auto-merge, gate-check, design-components)
```

- **`packages/contracts/orchestration`** is the single source of truth for
  everything crossing the boundary: the task queue name (`aep-orchestrator`),
  the two workflow type-name strings, every signal/query name, and workflow-ID
  builders. Both services import it; neither hand-types a signal name.
  `aep-api` never imports `services/orchestrator`'s Go packages — workflows
  are started **by name string**, and the worker registers them under the
  same names (`workflow.RegisterOptions{Name: ...}`).
- **`services/aep-api/internal/feature/orchestration/client.go`** is aep-api's
  entire Temporal footprint: `StartCycle`, `Signal`, `QueryCycle`, `QueryTask`.
  A dial failure only logs a warning — aep-api boots with orchestration
  disabled rather than failing startup.
- **Starting a cycle**: approving/saving requirements
  (`requirements_huma.go`'s `SaveAndProceed`) or a GitHub issue opened against
  an issue-first project (`cycle.Service.OnIssueTaskOpened`, bootstraps
  straight into `PhaseImplement` — no requirements/design gate for that path)
  both call `StartCycle`/`SignalWithStart`.
- **Signaling from webhooks**: `internal/feature/execution/events.go`
  (`Events.WithTaskSignaler`) turns GitHub PR/push events into
  `TaskSignaler.Signal(workflowID, signalName, ...)` calls — `PRReady` on PR
  opened, `PRMerged` + `BuildStarted` on merge, `PRRejected` on close-without-merge.
  This is the "webhook → signal" hop: aep-api's webhook router never touches
  the old Funnel anymore, it signals the exact task workflow by ID.
- **Activities call back into aep-api**, they don't do I/O themselves. Five
  authenticated (`orchestration.bearerAuth`, a shared secret —
  `AEP_API_INTERNAL_BEARER`, byte-identical on both services) S2S endpoints
  under `/internal/v1/orchestration/*`:
  - `POST tasks/dispatch` — wraps the preserved `CodingExecutor.Run` (namespace
    ensure, SM-API `ExternalSecret`s, cluster-gateway-proxy Job apply,
    credential minting) **minus** the old Funnel's gating, which now lives in
    the workflow itself.
  - `POST tasks/deploy` — same shape, for the build→deploy leg.
  - `POST tasks/auto-merge` — real GitHub auto-merge
    (`gitrepo.IssueOps.MergePullRequest`, idempotent on an already-merged PR)
    for `CodeReview: auto` cycles.
  - `POST gate-check` — the `auto`-gate hook (tests/lint/self-review). Today
    this is an explicit, authenticated pass-through with no check logic behind
    it yet — greenfield, out of scope for this pass by explicit decision.
  - `POST design/components` — reads the design graph so the workflow can
    derive its task DAG (`DependsOnComponents`) without re-implementing design
    parsing.
  - Every dispatch/deploy call also UPSERTs an `executions` row keyed on
    `(workflow_id, version)` — the **read-model** the console's task list
    still reads. `executions` is no longer a queue or an admission gate, just
    a projection the activities write after the fact.
- **Concurrency cap**: dispatch ensures a `ResourceQuota`/`LimitRange` on the
  org's `wc-<org>-...-remote-worker` namespace; a quota-exceeded response is
  retriable, so Temporal backs off and retries automatically. This replaces
  the old admission-mutex partial unique index — the cap is enforced by
  Kubernetes, not application-level locking.
- **`ProjectStatus.CyclePhase`** surfaces the live workflow position
  (`cycle.Service.GetFlowState`) alongside the pre-existing artifact-derived
  `Project.Phase` (has-spec / has-design) — the two are deliberately kept
  separate rather than merged into one field.

---

## What's not done: R4.2

The reactive engine's **dispatch/admission** path (Funnel, Sweep, the
admission-mutex index) is fully deleted. Two poll loops were deliberately
**kept**, because deleting them requires more than this pass could safely
verify:

- **`ExecWatcher`** — polls OpenChoreo `WorkflowRun`/`ReleaseBinding` status
  for coding and build runs, handles the git-clone-auth build-retry loop,
  captures final logs, cleans up per-run `ExternalSecret`s, and (the one
  behavior this pass changed) polls the component's real `ReleaseBinding`
  Ready condition before firing `SignalDeploySucceeded` — it used to fire that
  signal synthetically in the same tick as `BuildSucceeded`, before any deploy
  had actually happened. That was a genuine bug, now fixed, independent of
  whether the watcher itself survives.
- **`JobWatcher`** — polls proxy-dispatched coding-agent Jobs and fires
  `SignalCodingAgentFailed`.

The original plan called for porting both into Temporal **heartbeating
activities** called directly from `TaskLifecycleWorkflow`, removing the
aep-api-side pollers entirely. Blocked on:

1. `TaskLifecycleWorkflow` has a `WorkflowReplayer` determinism test
   (`internal/workflows/replay_test.go` + `testdata/task_lifecycle_happy.json`)
   that replays a *recorded* Temporal event history against the current
   workflow code. Adding a new `workflow.ExecuteActivity` call for a
   heartbeating wait — exactly what this change requires — changes the
   command sequence and fails the replay test.
2. The fixture can only be regenerated from a **live Temporal server**
   running the old workflow and capturing its real execution history — not
   something to hand-author, and no live cluster was available when this was
   attempted.
3. The alternative that avoids the replay problem — an independent sibling
   workflow, started from an *already-scheduled* activity via the Temporal
   client directly (not `ExecuteChildWorkflow`), signaling
   `TaskLifecycleWorkflow` from the outside via `SignalExternalWorkflow` — is
   real, valuable follow-up work, but needs a live Temporal + OpenChoreo stack
   to tune heartbeat timeouts and verify signal semantics before it's safe to
   land.

**Next steps, in order:** get a live Temporal dev environment capable of
capturing/replaying history → build the sibling-workflow architecture for
build/deploy completion (replay-safe by construction) → extend it to
coding-run failure detection → only then delete `ExecWatcher`/`JobWatcher` and
their `app.go` wiring (verify with
`grep -rn "NewTicker" internal/feature/codingagent` that nothing is left
polling).

---

## Local configuration

Both services must agree on four env vars — `docker-compose.yml` sets them
identically:

| Var | aep-api | orchestrator | Notes |
|---|---|---|---|
| Temporal frontend address | `TEMPORAL_HOST_PORT` | `TEMPORAL_HOSTPORT` | **Different spelling on purpose** — don't "fix" one to match the other without checking both `config.go` files; each is an existing, independent env-var contract. |
| `TEMPORAL_NAMESPACE` | same name | same name | Temporal namespace, `default` locally. |
| `TEMPORAL_TASK_QUEUE` | same name | same name | Must match `orchestration.TaskQueue` (`aep-orchestrator`). |
| `AEP_API_INTERNAL_BEARER` | same name | same name | Shared secret for `/internal/v1/orchestration/*`. Must be byte-identical on both services. |

Local dev stack: `temporal` (dev server, frontend :7233) + `temporal-ui`
(:8233) + `orchestrator` (the worker) run as their own `docker-compose`
services alongside `aep-api`/`console`. Watch a cycle advance at
`http://localhost:8233`.

For multi-org / production topology, R5 ports this to an in-cluster Helm
install (Postgres-backed Temporal, orchestrator as a Deployment, `org`
registered as a Search Attribute) — see `deployments/helm-charts/` for the
current chart.
