# ADR-0029 — A failed run carries its failure record; the console owns the words

**Status:** Accepted · **Exposed by:** `RunFailure`, `BuildSummary.failureCode`,
`BuildStage.failureCode`, `ActivityEvent.run_failed` in
`packages/contracts/api/v1/openapi.yaml` · **Detail:**
[`services/aep-api/design/run-failure-record.md`](../../services/aep-api/design/run-failure-record.md),
console [ADR-0031](../../apps/console/design/decisions/ADR-0031-a-failed-run-explains-itself.md)

**Why here.** Two codebases have to agree on what a failure IS on the wire: the
platform records a code and facts, the console renders a sentence. That
division is the decision; the package notes describe each half.

## Context

A run settles with `state` and `terminal_reason`, and `plan-failed` names a
phase, not a cause. For the planning phase the cause — which dependency, the
provisioner's own sentence, whether repeating could help, how many attempts —
was logged and dropped at the settle. The console rendered the slug
(`Failed · plan-failed`). An external dependency mints no gate issue, so that
class had no surface at all: two projects hit it on consecutive days and the
diagnosis was a log grep, the defect ADR-0018 had named and only half-fixed.

## Decision

1. **One record on the run row, not a table.** `milestone_runs.failure` (jsonb,
   `delivery.RunFailure`): `code`, `phase`, optional subject (`component`,
   `dependency`), `permanent`, `attempts`/`maxAttempts`, `firstAt`/`lastAt`,
   `detail`. Written per attempt by the activity that met the fault, cleared
   when a later attempt succeeds, left by settle. It explains `terminal_reason`;
   it does not replace it. A run has one failure record, not a history.
2. **Permanent is the producer's word, once.** The same classification that
   makes an error non-retryable (`ErrProvisionPermanent`,
   `sourcecontrol.IsPermanent`) is what the record says, so "retrying cannot fix
   this" and the single attempt it took are two readings of one fact. A design
   the provisioner cannot author (a schema with no key) is permanent — one
   attempt, not three.
3. **Codes cross the wire; prose does not.** `code` is a closed set; `detail`
   is the platform's own error text, producer-scrubbed and capped, never model
   output or a request body. The console owns every sentence in one module —
   the rule `RunEvent.notice` established.
4. **The cheap reads carry the code.** `BuildSummary.failureCode` (ledger) and
   `ProjectStatus.build.failureCode` (overview) come from the row those reads
   already make, so the reader who is not on the build page meets the same
   words. A failed settle also emits `run_failed` on the activity feed.

## Consequences

- A new failure class is a code constant, a producer call in the stage that
  meets it, and a sentence in the console; the record, ports and wire need no
  change. Only the planning phase records today; other terminal reasons render
  from the slug and the cycle's own facts until they get a producer.
- Every run failed before the record existed reads *the platform recorded no
  further details* — honest, like ADR-0027's `recording: none`.
- Rejected: a `run_events` narration table (a platform log, more than asked
  for; the row design does not preclude adding it), a fault-only table, and
  free-text messages on the wire.
