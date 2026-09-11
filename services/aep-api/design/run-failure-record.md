# The run failure record — why a run failed, on the run row

A run's outcome is two columns on `milestone_runs`: `state` and
`terminal_reason`. Both are coarse by design — `plan-failed` names a phase, not
a cause — and for the planning phase everything finer used to be dropped at the
settle: `workflow_dev.go` logged the activity's error and wrote the slug. The
console rendered the slug. This note describes the record that now carries the
cause, who writes it, and what reads it.

## The record

`milestone_runs.failure` is one nullable `jsonb` column, `delivery.RunFailure`:

| field | meaning |
|---|---|
| `code` | closed set — `dependency-unprovisionable`, `dependency-provision-failed`, `plan-turn-failed`, `repository-unavailable` |
| `phase` | the run phase the fault was met in |
| `component`, `dependency` | the subject, when the fault has one |
| `permanent` | the PRODUCER's classification — the same answer that decided the retry policy |
| `attempts`, `maxAttempts` | attempts that hit this fault; the activity's bound (0 = unbounded) |
| `firstAt`, `lastAt` | the attempt window; the repository keeps `firstAt` across overwrites of the same fault |
| `detail` | the platform's own error text, per dependency, without sentinel wraps — `ScrubFailureDetail` redacts credential shapes and caps at 2 KiB |

It is a **record, not a history**: written by the activity that met the fault
on every attempt (so a `planning` run reads "attempt 2 of 3" live), cleared by
`ClearFailure` when a later attempt succeeds, left in place by settle.
`terminal_reason` is unchanged and remains what every predicate reads.

The column encodes itself (`Value`/`Scan`), because the run repository writes
through map updates, the same reason `DependencyNames` does.

## Who writes it

```
ProvisionGates ──► gates.ProvisionForBuild ──► *delivery.ProvisionFailedError{Faults}
                    │                             (component, dependency, reason, permanent, cause)
                    ├─ recordPlanningFault(runID, provisionFailure(err, attempt))
                    └─ return provisionErr(err)          ← unchanged classification
PlanMilestone  ──► planner.PlanIntoMilestone
                    ├─ recordPlanningFault(runID, planFailure(err, attempt))
                    └─ return planErr(err)
```

- `app.aggregateProvisionFailures` returns the typed error instead of a joined
  string. Its `Error()` is the old text; `Is(ErrProvisionPermanent)` is true
  when any fault is; `Unwrap()` exposes each fault's cause so the dependencies
  domain's sentinels stay reachable.
- `PlanMilestoneInput` carries `RunID` (empty on a pre-record history → records
  nothing). Recording is best-effort inside the activity: a failed write is
  logged and never masks the activity's own error.
- `RunStore` (run/ports.go) gained `RecordFailure` / `ClearFailure`; the
  composition root adapts them onto `MilestoneRunRepository`.

**Classification fix that rode along:** `BuildExternalResourceType`'s refusal
(no config key, empty key) is wrapped `ErrProvisionPermanent` by
`dependencies.ExternalResourceProvisioner`, and the external path of
`ProvisionForBuild` now keeps `Err` on its `ProvisionFailure` so the
classification survives aggregation (`authorExternalPrepared` wraps with `%w`
twice). A design stub the provisioner cannot author fails on attempt one, not
three, and the record says `permanent: true`.

## Who reads it

| read | field | consumer |
|---|---|---|
| `MilestoneRunView.failure` (`runread.failureView`) | the whole record + `workflowId` derived from `MilestoneRunWorkflowID` | the build page's failure card |
| `BuildSummary.failureCode` (`build.failureCodeFor`) | the code, only on a FAILED run | the Builds ledger chip |
| `ProjectStatus.build.failureCode` (`projects/status_stages.go`) | the code, only on a failed newest dev run | the overview's Build leg |
| `activity_events` `run_failed` (`app.runFailedActivityRecorder`, from `Activities.SettleRun`) | `tag`, `component`, `reason` = code or terminal reason | the project feed |

The console owns every sentence (console ADR-0031, `features/builds/lib/failure.ts`).
Codes cross the wire; prose does not — the `RunEvent.notice` rule.

## Not covered

Cycle-phase producers (an agent that died, a budget spent) leave no record yet;
the console renders those from `terminalReason` and the newest cycle's
`agentReason`. Adding one is a producer call in the stage that settles on it.
