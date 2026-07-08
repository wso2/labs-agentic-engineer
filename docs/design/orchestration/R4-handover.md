# R4 Handover: Remove the reactive engine

Context for whoever picks up the rest of R4. This documents what the R2/R3
gap-closure pass (see `~/.claude/plans/can-you-fix-missing-frolicking-owl.md`)
landed for R3/R4, and — more importantly — *why* the literal R4 ask (delete
`ExecWatcher`/`JobWatcher`, replace their polling with orchestrator
heartbeating activities called from `TaskLifecycleWorkflow`) could not be
completed in that pass, so the next person doesn't re-discover the same
blocker from scratch.

## What landed

- **R3.1 (security):** `/internal/v1/orchestration/*` now authenticates by
  construction (`orchestration.bearerAuth`, a shared secret verified against
  `AEP_API_INTERNAL_BEARER` — set on both aep-api and the orchestrator). It was
  previously wide open.
- **R3.3 (read-model):** `executions` gained `workflow_id`/`version` columns +
  a disjoint partial unique index (`ux_executions_readmodel`, on `workflow_id
  <> ''`) alongside the existing admission-mutex index. `UpsertReadModel`/
  `GetByWorkflowID`/`ListReadModelByStatus` are real repository methods, wired
  into `orchestrationTaskDriver.DispatchTask`/`DeployTask`.
- **R3.5 (auto-merge):** `AutoMerge` is real now —
  `gitrepo.IssueOps.MergePullRequest` (PUT `.../pulls/{n}/merge`, idempotent on
  an already-merged PR), the PR number recovered from the task's latest coding
  Execution's `pr#<N>` reason stamp (`execution.OpenPRNumber`, exported for
  this).
- **R2.1 (issue fast-path):** `cycle.Service.OnIssueTaskOpened` bootstraps a
  cycle at `PhaseImplement` for an issue-first project (no prior cycle row);
  wired via `task.WebhookEvents.WithCycle` on `issues.opened`.
- **R2.2 (Project.Phase):** `ProjectStatus.CyclePhase` (nil-safe, split
  responsibility — the artifact-derived `Phase` is unchanged) surfaces the
  live workflow position from `cycle.Service.GetFlowState`.
- **R3.2 fix (real deploy completion) — the R4-relevant piece:**
  `ExecWatcher.reconcile`'s build-success branch used to fire
  `SignalBuildSucceeded` + `SignalDeployStarted` + `SignalDeploySucceeded` **in
  the same tick**, before any deploy had actually happened. It now fires only
  the first two, records a `"deploying"` read-model checkpoint, and a new
  `ExecWatcher.pollDeploys` (called at the top of every `Sweep`) polls the
  component's real `ReleaseBinding` Ready condition
  (`openchoreo.ComponentClient.IsComponentReady`, new) on each subsequent
  tick — firing `SignalDeploySucceeded` only once actually Ready, or
  `SignalDeployFailed` after `deployPollStaleAfter` (30 min) with no Ready
  condition. This is a genuine bug fix, independent of whether the watcher
  itself survives R4.2.

## What did NOT land: the heartbeating-activity architecture, and why

The plan's R4 ask was: port `ExecWatcher`/`JobWatcher`'s poll loops into
Temporal **heartbeating activities** called by `TaskLifecycleWorkflow`,
eliminating the aep-api-side tickers entirely. This turns out to require
either regenerating a replay fixture this environment cannot produce, or a
non-trivial new sibling-workflow architecture that needs a live cluster to
verify. Concretely:

1. **`TaskLifecycleWorkflow` has a determinism guard you cannot edit around.**
   `internal/workflows/replay_test.go` replays a *recorded* event history
   (`testdata/task_lifecycle_happy.json`) against the current workflow code.
   Any change to the workflow's activity/signal call sequence — including
   adding a *new* `workflow.ExecuteActivity` call for a heartbeating
   build/deploy-wait activity, which is exactly what "call a heartbeating
   activity instead of receiving a signal" requires — changes the command
   sequence the replayer checks, and **fails this test**. (R3-handover.md
   already flagged this for the `DevelopmentFlowWorkflow` DAG-scheduling
   change; the same constraint applies here, just for a workflow that already
   has a captured fixture.)
2. **The fixture can only be regenerated from a real Temporal execution
   history** (per the test's own error message: "regenerate the fixture only
   on an intentional, versioned change... see the capture tool referenced in
   the design docs"). That requires a live Temporal server to run the
   *old* workflow, capture its history, then re-run it forward — not
   something synthesizable by hand, and no live cluster was available in the
   environment this pass ran in.
3. **The alternative — a genuinely independent sibling workflow** (started as
   a side effect of an *existing*, already-scheduled activity like
   `DispatchDeploy`, using the Temporal client directly rather than
   `workflow.ExecuteChildWorkflow`, and signaling `TaskLifecycleWorkflow` via
   `workflow.SignalExternalWorkflow` from the outside) avoids the replay
   problem in principle, since it never becomes a command in
   `TaskLifecycleWorkflow`'s own history. But it needs: a Temporal client
   wired into the orchestrator's `Activities` (new dependency), a new
   workflow + heartbeating activity + worker registration, and — critically —
   integration testing against a live Temporal + OpenChoreo stack before it's
   safe to land (heartbeat timeout tuning, `SignalExternalWorkflow` semantics
   for a workflow ID that may not exist yet or may already be terminal, etc.).
   That's real, valuable follow-up work — just not something to build blind
   in one pass without a cluster to verify against.

Given that, this pass made the **narrowest safe fix**: keep `ExecWatcher` as
the mechanism, but make its deploy-completion signal *true* instead of
synthetic (item 5 above). `ExecWatcher`/`JobWatcher` are **not deleted** —
they still own: OC `WorkflowRun` polling for coding/build runs, the
git-clone-auth build-retry loop, final-log capture, per-run `ExternalSecret`
cleanup, and (`JobWatcher`) proxy-dispatched coding-agent Job polling +
`SignalCodingAgentFailed`. None of that was touched or replicated elsewhere,
so deleting either watcher today would silently stop those signals/cleanups.

## Suggested next steps

1. Get a real Temporal dev server + a way to capture/replay history (the
   "capture tool" the replay test's error message references — find or build
   it) before attempting anything that changes `TaskLifecycleWorkflow`'s
   command sequence.
2. If (1) isn't available soon, build the sibling-workflow architecture
   (§3 above) for build/deploy completion first — it's replay-safe by
   construction. Extend it to coding-run failure detection
   (`JobWatcher`'s `SignalCodingAgentFailed`) once proven.
3. Only after the sibling workflow(s) cover every signal `ExecWatcher`/
   `JobWatcher` currently fire — AND the build-retry / log-capture / secret-
   cleanup responsibilities have an explicit new home — delete the watchers
   and their `app.go` wiring (`execWatcher`/`jobWatcher` construction, the
   `watchers` slice entries). Confirm via `grep -rn "NewTicker"
   internal/feature/codingagent` that no polling loop remains.
4. Update `docs/design/orchestration/00-overview.md` / `04-sequence-full-flow.md`
   if they diagram `ExecWatcher`/`JobWatcher` as part of the completion path.

## Other deferred-by-design items from this pass

- **Real gate-checks** (tests/lint/self-review behind `GateChecker`): still an
  explicit, now-authenticated pass-through (`InternalService.RunChecks`
  comment updated to say so plainly). Greenfield — no check logic exists
  anywhere in the repo today. Out of scope for this pass by explicit user
  decision.
- **`plan-tasks` / `build` as orchestrator-initiated S2S endpoints**: the
  workflow already derives its task DAG from the design graph directly
  (`PlanTasks` activity → `DesignReader.Components`), and build stays
  webhook-driven (PR-merged → `spawnBuild`). Not revisited — explicit user
  decision to keep this pass scoped to bug fixes, not a re-architecture.
- **Concurrency cap (ResourceQuota/LimitRange)**: see the plan's R3.4 section
  for the design and its cross-service dependency (the external
  cluster-gateway-proxy's verb allow-list needs `resourcequotas`/
  `limitranges` added before aep-api can apply them).
