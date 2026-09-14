# ADR-0031: A failed run explains itself — one record, one sentence source

- **Status:** Accepted
- **Date:** 2026-09-11
- **Context:** A build that failed while the platform was preparing the version
  read `Failed · plan-failed` on the ledger and on the build page, and nothing
  else: no cycle, so no agent log; no gate comment, because an external
  dependency mints no gate; the reason lived in one aep-api log line. Two
  projects hit it on consecutive days (`allocation-app`, `testdsxc1`: an
  external dependency written as a `source: org` stub the provisioner refuses).
  ADR-0014 had already stated the rule — *a stage that went wrong says so in the
  platform's own recorded words* — and ADR-0021 retired the rail that was to
  carry them without giving the words a new home. `ledger.ts` rendered
  `BuildSummary.reason` verbatim, so the reader met the slug.

## Decisions

1. **The platform sends codes; the console owns the words, in one module.**
   `MilestoneRunView.failure` (`RunFailure`: `code`, `phase`, `component`,
   `dependency`, `permanent`, `attempts`/`maxAttempts`, `firstAt`/`lastAt`,
   `detail`, `workflowId`) is the record; `BuildSummary.failureCode` and
   `ProjectStatus.build.failureCode` carry the code where the cheap reads are.
   `features/builds/lib/failure.ts` is the single mapper — `failureLabel` for
   the chip qualifier, `failureCopy` for the card — the way `verdict.ts` is for
   validation and `@aep/progress-view` is for notice codes. The ledger chip, the
   build page header, its card and the overview's Build leg all read from it,
   so one outcome is said one way. *Amends the "Builds" section of the lexicon.*

2. **One card under the build page header, not a rail and not a section.**
   Drawn only when there is something to explain: a failed run, or a run still
   `planning`/`running` with a recorded fault (a fault being retried — amber,
   "attempt 2 of 3"). It says what happened, what did NOT happen (*Nothing was
   coded or deployed*), and whether trying again can help — in that order —
   with a next step when the platform knows where the fix lives (*Open sendgrid
   in the design* → `/spec?file=specs/design/dependencies/<name>/dependency.json`).
   A cancelled run draws nothing (a person stopping an increment is not a
   fault); a blocked run keeps its existing message. Considered and not taken:
   a "Run log" section listing the platform's phase narration (the right shape
   for a platform log, more than was asked for), and card-less "section only"
   (the reader would open a section to learn why).

3. **Details are one click down, and copyable.** *Show details* opens the
   facts a bug report needs — `code` with *permanent*/*retryable*, `attempts`,
   `window`, `recorded` (the platform's own error text, scrubbed and capped at
   the producer), `run`, `workflow` — and *Copy details* puts them on the
   clipboard as one block. The support conversation starts from the card.

4. **Every terminal reason has a sentence, and an empty record says so.** The
   producer records faults for the planning phase (the two activities that
   discarded everything); every other `terminalReason` gets a fallback
   sentence keyed on the slug, with the newest cycle's `agentReason` when it
   has one, and a run failed before the record existed reads *The platform
   recorded no further details for this run.* — ADR-0027's `recording: none`
   stance. An unknown code renders as itself rather than hiding.

5. **The Coding agent log stops promising a future a failed run has none of.**
   When every run of the version ended without dispatching a cycle, the empty
   section reads *Did not start — the run ended while the platform was
   preparing the version* instead of *Nothing has been dispatched yet*.

## Consequences

- Copy for every failure state lives in `failure.ts` and is recorded in the
  lexicon (*A failed run explains itself*). A new code needs a sentence there
  and nowhere else.
- The "not looking" surface is the overview's Build leg (`Build failed ·
  <what>`); the backend also emits a `run_failed` activity event so a feed
  panel, if one returns, is fed. The bell stays RCA-only (ADR-0008).
- Live-verified 2026-09-11 on the local plane with the `sendgrid` stub: one
  attempt (not three — the schema refusal is now classified permanent),
  `Failed · Dependency could not be provisioned` on the ledger and header, the
  card with its design link and details, the overview leg — see the backend
  note `services/aep-api/design/run-failure-record.md`.
