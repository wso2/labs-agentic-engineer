# ADR-0018 — Planning is a phase of the run, not of the click

**Status:** Accepted, partly revised by
[ADR-0024](ADR-0024-cancel-reaches-the-planning-phase.md) ·
**Related:** [ADR-0011](ADR-0011-milestone-is-the-unit-of-execution.md)
(the milestone is the unit of execution), [ADR-0017](ADR-0017-the-platform-owns-deploy.md)

> **ADR-0024 revises two things below.** The planning phase was blind to cancel
> for its whole duration, and `ProvisionGates` inherited an unbounded retry policy
> that turned a permanent provisioning answer into a loop that re-minted the
> version's gates. It also replaces this ADR's reading of the rebuild branch: an
> unchanged spec is not evidence that the milestone was filled, so whether a run
> skips its planning turn is now answered by the milestone rather than by the
> spec-save status.

## Context

Cutting a version ran in two halves. The build click did the fast, ordered work
synchronously — supersede, mint the milestone, admit the run row (which arms the
build mutex) — and then handed the rest to a **detached goroutine**:

```go
detached := context.WithoutCancel(ctx)
go s.fillMilestone(detached, orgID, projectID, res.Tag, run, provInputs)
```

`fillMilestone` minted the version's dependency gates, ran the planning turn (an
LLM call, minutes long, wrapped around git and GitHub), and started the
supervisor. Any error settled the run `plan-failed`.

The shape was deliberate — the click must not hold a request open for an LLM
turn, and the row must be admitted *before* planning so a double-click cannot
land in the gap — but a bare goroutine gave that step three properties nothing
else in the platform has:

1. **Not durable.** An `aep-api` restart mid-turn killed it. Whether the row was
   then settled depended on luck.
2. **No transient/permanent split.** A seven-second TCP connect timeout to
   github.com and "the repository was deleted" were handled identically: settle
   the version. This is what actually happened — a build died `plan-failed`
   because the host VM was CPU-saturated and two connects timed out. At normal
   load the same container reached GitHub 20/20.
3. **Invisible.** No history, no attempt record; diagnosis was log-grep.

Every other I/O in this platform already distinguishes a blip from an answer:
`run/errors.go` `sourceControlErr`, `openchoreo/stale_write.go`
`retryStaleWrite`, and `deployErr` from ADR-0017. The plan path was the one step
outside that discipline, because it was the one step outside Temporal.

## Decision

**The build click starts the run; the run fills its own milestone.**

```text
click:  … → mint milestone → ADMIT ROW (planning) → StartRun → return
                                                       │
run workflow:                                 phase "planning"
                                              ├─ ProvisionGates   (gates first)
                                              ├─ PlanMilestone
                                              └─ the cycle loop
```

- The mutex ordering is unchanged: the row is still admitted synchronously,
  before anything slow, so the version is claimed across the planning turn.
- `Tag` and `ProvisionInputs` ride the **request**, not the run row. Only a
  caller knows whether it is asking for a version to be FILLED (the click) or an
  existing run to be RESUMED (the sweep, an adoption); reading a tag off the row
  would make every re-offer re-plan.
- Planning is idempotent by construction — minting dedupes on the title slug
  against the milestone's own issues — which is what makes it safe under
  Temporal's retries and under a fresh execution started to recover a run.
- A planning failure still settles `plan-failed`. Same terminal reason, now
  written by the supervisor.

Two supporting decisions were forced by it:

**The supervisor becomes the only settle-writer — so a start that never happened
must be reported.** `StartRun` previously returned `nil` from a degraded boot (no
dispatcher, no workflow engine) on the theory that the reconcile sweep would
re-offer. It cannot: a non-terminal row makes `LiveRunForMilestone` answer
forever, so the sweep skips it, and the partial unique indexes then refuse every
later build on that project. `StartRun` now returns the existing
`ErrRunNotStarted` sentinel; callers with a timer swallow it, and the click —
which has none — settles the row it armed and answers 503.

**The reconcile sweep re-offers live rows.** A live ROW is not a live WORKFLOW,
and nothing else notices the difference. Re-offering is idempotent (a running
execution answers `AlreadyStarted`; the row is reused, not re-admitted) and
cheaper than the pass it replaces — one Temporal call instead of a GitHub issue
count. It also closes a wedge that pre-dates this change, already documented at
`migrate/milestone_runs.go:75-85`. It deliberately skips a row still in
`planning`: re-offering that one would start a fresh workflow with no Tag and so
settle an *unplanned* version as delivered.

## Consequences

- A transient GitHub failure during planning no longer kills a version. That is
  the defect this ADR exists for.
- **The zero-cycle park is gone.** A run that planned nothing settles
  `succeeded` rather than waiting forever. That wait existed only because a poll
  could land mid-plan and read "not planned yet" as "nothing to do"; the
  ambiguity does not survive planning moving inside the loop. It is also correct
  for a re-build whose Tasks all already exist and are closed. Such a run does
  **not** validate — validation asserts against what a run landed, and it landed
  nothing. A revalidation keeps its exception.
- `RunStatePlanning` gains an honest meaning: it is now the state the run is in
  *while the workflow is filling the milestone*, not a window between two
  processes. It is still written exactly once, at admission, and the workflow
  only ever leaves it.
- The click's failure surface grows a 503 (the platform is not ready to work
  this version), where before it returned 200 over a run nobody would drive.
- `run` gains two ports (`Gates`, `Planner`) satisfied at the composition root.
  This does not breach `task ⊥ run`: that fence is an import ban in both
  directions, and `build` already reaches the same two capabilities the same way.

## Alternatives rejected

- **Keep the goroutine, add a bounded retry.** Fixes the reported symptom and
  nothing else — still not durable, still invisible, still a second writer able
  to settle a row. It would also be deleted by this change, so building it first
  was waste.
- **A `planned_at` column so recovery can skip a completed plan.** Adds schema
  and a second writer to the row the supervisor otherwise owns, to save one LLM
  turn on a path that is reached only when Temporal loses an execution.
- **`DescribeWorkflowExecution` liveness probe in the sweep.** An extra RPC for
  an answer `ExecuteWorkflow` already gives atomically, plus a TOCTOU window.
