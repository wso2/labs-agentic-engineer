# ADR-0005: Statistics, baselines and sweeps

**Status:** Accepted (2026-10-05)

## Context

A codegen attempt takes up to an hour and costs real money, so a sweep is
small: one or two cases, a few repeats. Its numbers are noisy, and failures
come from two places: the generated app, and everything around it (docker, the
runner, the model provider, the browser, the harness). The report has to say
what moved without overstating it, and the sweep has to spend nothing on work
that cannot finish.

## Decision

**Harness errors are excluded; hard fails count.** An attempt whose failure is
the environment's (`classify.ts`) is a `harness-error`: counted and listed, never
averaged in, because it says nothing about the code. A `hard-fail` (the coding
run failed, `wire` never came up) is the code's failure, scores 0 and is in the
median. A failure nobody classified is the environment's: it is no evidence
against the app.

**Rewalks are excluded from statistics.** A rewalk re-scores code an attempt
already produced, possibly against a changed checklist. Counting it would weight
one coding run twice and mix two rubrics in one median.

**The baseline is chosen per row.** For each case × config, the baseline is the
most recent earlier sweep that ran that row with no harness errors. This differs
from `evals/ballerina`, which picks a baseline sweep by coverage of the whole
case set: codegen sweeps rarely share a whole case set, and that rule would leave
the column empty.

**Never one number.** A row reports median and spread. A delta is
`inconclusive` when it sits inside the wider of the two spreads, and always when
either side has n=1, because one attempt has no spread.

**Cost is recorded as reported.** `total_cost_usd` comes from the coding run's
own transcript, null when absent. It is never recomputed from a price table that
the harness would have to keep current.

**The exit code is never set by a score.** The CLI exits nonzero only when
something could not be done (a refused credential, a case that will not load,
docker down). A bad score is the tool's output; a harness that exits 1 on it
cannot be scripted.

**`report.ts` is copied from `evals/ballerina`, not imported.** The shape is the
same (stat, compare, baseline), but that module's types are its own sweep's, and
the two harnesses should be free to diverge.

**Sweep execution:**

- A fixed pool of N workers pulls from one queue, as in
  `evals/ballerina/src/sweep.ts`. Attempts differ in length by an order of
  magnitude (a hard fail at minute 3, a full walk at minute 70), and batching
  would idle lanes behind the slowest member.
- SIGINT/SIGTERM abort one shared signal. An interrupted sweep starts nothing
  new, and each attempt in flight tears down through its normal path, never
  through a second emergency cleanup that would have to know what each attempt
  had running.
- Every case is loaded and its roles checked before anything is spent: a
  checklist naming a role `wire` would refuse is an hour of coding wasted.
- Provenance (`provenance.ts`) records the commit, the uncommitted paths and the
  exact `skills.diff`, since a sweep on a branch with uncommitted skill edits
  measures those edits and the commit alone hides them.

**Per-agent usage comes from the transcript.** The progress feed cannot give
it: `task_settled` carries no usage, and `run_settled` only the run total.
`log --usage` reads `.logs/runtime.log`, where the SDK tags each subagent
message with its fan-out call.

## Consequences

- An unstable environment shows up as a harness-error count, not as a lower
  score.
- Small sweeps often report `inconclusive`. That is the intended answer when
  the noise is wider than the move.
- A row with no clean earlier run has no baseline.
- Changes to `evals/ballerina/src/report.ts` do not reach this harness, and the
  reverse.
