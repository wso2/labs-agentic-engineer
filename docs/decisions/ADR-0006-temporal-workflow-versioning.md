# ADR-0006: Temporal workflow versioning & determinism policy

- **Status:** Accepted
- **Date:** 2026-06-18
- **Context:** Temporal replays a workflow's event history against the current
  workflow code. Nondeterminism, or an incompatible edit to running workflow
  code, breaks replay and can fail in-flight executions. We adopt the discipline
  up front so it is never bolted on. Companion: ADR-0005, `docs/design/orchestration/`.

## Decision

**Determinism (enforced by package boundary).** `internal/workflows` uses only
the Temporal workflow API (`ExecuteActivity`, `ExecuteChildWorkflow`,
`GetSignalChannel`, `SetQueryHandler`, `Now`, `NewTimer`, `Selector`). It must
**not** import `net/http`, `database/sql`, `os`, `time.Now`, `math/rand`, or
`uuid` — all such I/O lives in `internal/activities`. Task transitions are a pure
`(state, signal) → state` helper, unit-testable outside Temporal. A
`golangci-lint depguard` rule may enforce the import ban.

**Activity idempotency.** Activities are retried by Temporal, so each must be
idempotent: "ensure X / set Y to value", never "create X / increment Y".
Read-model writes are `UPSERT` keyed by `(workflowID, version)`; dispatch checks
before creating; signals from activities/webhooks carry a dedupe key.

**Versioning.**
- In-place behavioral changes to workflow code are guarded with
  `workflow.GetVersion(ctx, changeID, min, max)`; each change gets a unique
  `changeID`; the old branch is kept until no in-flight runs remain.
- Prefer additive, backward-compatible changes (new signal/state) over
  reordering/removing existing steps.
- Larger jumps use **Worker Build IDs / Worker Versioning** (drain old, start new).
- A `worker.WorkflowReplayer` test replays committed event-history fixtures
  (`internal/workflows/testdata/`) against current code; it runs in `make test`
  and catches breaking edits before they ship.
- Each versioned change is recorded (changelog/ADR) with its `changeID` and
  retirement.

## Consequences
- Workflow edits are safe-by-process: replay test + `GetVersion` guard or a new
  Build ID for anything non-additive.
- The determinism boundary is structural (a package), not just a guideline.
