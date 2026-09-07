# ADR-0010 — A validation report is an accumulation, not one all-or-nothing run

**Status:** Accepted

## Context

A validation run passed every acceptance criterion and delivered nothing — no
push, no pull request — then died four hours later on `redispatch-budget` having
spent $7.78 (issue #701).

The agent did not stop because it thought it was finished. Its final suite run
hit the 600s Bash ceiling: `durationMs: 597462`, `ok: true`, no output. Past the
timeout the harness detaches the command and answers `code 0` with empty stdout,
which is indistinguishable from a suite that completed. That destroyed
`tests/e2e/test-results/results.json`, which makes report generation impossible,
and `SKILL.md` is unconditional — *"Never open the PR without the report."* It
complied and stopped.

The trigger is arithmetic: 24 specs, subsets measured at 86–158s each, one
sequential pass over the deployed system. The suite had outgrown the window, and
criteria accumulate every milestone, so it was going to sever on every future
run of that project.

ADR-0009 decision 5 made this unrecoverable. It let only a complete, unnarrowed
run write the report's input, so a suite that could not be run completely had no
path to a report at all — sharding wrote nothing by design. The reasoning it was
defending is sound: before it, a shard silently overwrote the whole suite's
results with one batch's, and the report claimed most criteria were never
checked. The mistake was **banning the partial run instead of learning to detect
it.**

The detector was already there. `generate-report.mjs` iterates the criteria
**oracle** and looks up results, so the oracle is the denominator and Playwright
only supplies numerators. The generator already knew which criteria had no
result; it just could not tell two very different things apart, calling both
`not_run`:

| Criterion has… | Truth |
|---|---|
| no spec file on disk | legitimately never checked |
| a spec file, no result anywhere | a run was missed or severed |

## Decisions

A **batch** below is one results file, written by one test run. The directory
is `runs/` because a run is what produces one; the code and the report say
`batch` because by the time the generator sees them they are inputs to a merge,
and "run" is already this platform's word for something else.

1. **Every run writes its own results file; the report is their merge.**
   `test-results/runs/<ISO-stamp>.json`, the stamp computed once at config module
   scope. Filename order is chronological, so `readdir().sort()` is the merge
   order and the newest result for a criterion is simply the last one seen. No
   single command has to cover the whole suite, which retires the ceiling as a
   concern rather than moving it.

2. **Coverage is verified, not assumed.** Every spec file on disk must APPEAR
   in the merged runs, or the generator exits 2 naming the ones missing.
   Appearing is the check, deliberately: a spec whose tests all skipped appears
   and reports `not_run` honestly, which is the same thing a criterion with no
   spec file reports. What cannot happen any more is a spec silently absent
   because the run covering it never wrote anything. This is the compensating invariant for letting partial runs write,
   and it is strictly stronger than what it replaces: ADR-0009 *assumed* one run
   had covered everything and had no way to check.

   A severed batch has written nothing *yet*, so its specs surface in exactly
   that list — named and re-runnable. The agent never has to size a batch
   correctly; getting it wrong costs an iteration instead of the cycle.

   "Yet" is load-bearing: a call that blows its timeout is **detached, not
   killed**, so it keeps running and its results may land minutes later. The
   coverage list is therefore a snapshot, not a verdict on the batch, and both
   the skill and the generator's message say to look again before re-running.

   Coverage is matched by spec PATH, not by criterion id alone. Results
   accumulate for the life of the run, so a batch recorded before a spec moved
   would otherwise vouch for a file that has never been executed.

3. **Merge per batch, then fold — never by concatenating `spec.tests[]`.**
   `specOutcome` is fail-dominant and has no notion of time, so folding every
   batch's tests into one array would let a failure that has since been healed
   permanently outvote the pass that healed it. Two specs claiming one criterion
   stays a hard error, but only **within** a batch — whether they sit in one
   file or two, which the message distinguishes; across batches it is the same
   spec run twice, which is the point.

4. **The whole-suite run becomes best-effort, not required.** Attempt it: if it
   lands it supersedes the per-spec results with one sequenced pass, which is the
   only thing that catches a spec depending on state another spec left behind. If
   it severs it costs nothing, because the per-spec results already cover every
   authored spec — and being detached rather than killed, it may still deliver
   its own results afterwards. Guessing its size wrong wastes minutes
   instead of the report.

5. **The authoring runs are the results.** `authoring.md` already requires every
   spec to pass **alone, twice consecutively**, so those runs happen today and
   their results were being thrown away. Keeping them adds no execution — it
   stops a discard, and it is why decision 4 costs nothing.

6. **A call that did not complete settles nothing.** `timedOutAfterMs` on the
   SDK's Bash result is the flag for a severed call. The translator now withholds
   the outcome for one, and renders it as severed rather than `ok`.

   This was a live false-green: `ok: !isError` made a severed call look
   successful, and `validationRunOutcome` painted `Passed` on every criterion it
   was running — reachable on `healing.md`'s focused re-runs, i.e. on criteria
   that had *just failed*. The feed had the same lie, contradicting the warning
   `SKILL.md` prints in prose two paragraphs away.

7. **The Bash ceiling is deliberately not raised.** It is configurable
   (`BASH_MAX_TIMEOUT_MS`, unset in this repo, so 600s is a default rather than a
   limit), and raising it was the obvious fix. It is also a treadmill: any value
   is exceeded as criteria accumulate, so it only schedules the same incident
   later. A number that has to be correct is the thing being removed.

## Rejected

- **Playwright's `blob` reporter plus `merge-reports`.** Both exist in the pinned
  1.61.1, and neither survives contact with overlapping re-runs. The blob
  reporter wipes its output directory on every invocation, so N sequential
  batches into the default `blob-report/` leave one. The merger sets
  `mergeTestCases: false` and compares an already-relative spec path against
  `path.relative(rootDir, …)`, so the match always fails and overlapping specs
  duplicate — which trips the duplicate-criterion check unconditionally. Its
  contract is *disjoint shards*; re-running a spec is outside it. Merging the
  JSON ourselves is less code, has no version coupling, and puts the tiebreaker
  where it can be reasoned about.

- **Cross-spec interference detection of its own.** Rotating spec order across
  batches would surface state dependencies without any complete pass, at the cost
  of multiplying runs — and a criterion that alternates pass/fail is what
  `authoring.md` already calls a flake and refuses to ship. Interference rests on
  the independence discipline that document already mandates (no ordering
  dependencies, no state left behind, unique test data suffixed with a run
  marker), plus decision 4's opportunistic pass. The full-suite run was only ever
  an incidental check on that discipline.

## Consequences

- **A severed command is now visible as one.** It reports `ok: false` and says it
  kept running in the background, so the feed stops agreeing with a lie the skill
  warns about.
- **The report carries provenance.** `batches[]` and a per-criterion `batch`,
  because a green merged out of eleven runs and a green one run proved are
  different claims and the report's whole value is being the authoritative record.
- **The gates got wider coverage, not narrower.** The `// spec:` header check and
  the locator lint are driven off the specs on disk rather than off whatever
  appeared in the results — so a severed run can no longer make its own specs
  skip a check, which is what happened before when they vanished from the tree.
- **`generate-report.mjs` has tests.** `skills/` is not a pnpm workspace and
  deliberately is not one, so they run from the root `Makefile`. The merge is the
  one part of this script that can produce a wrong *verdict* rather than a loud
  failure, and it was previously untested.

- **`test-results/runs/` grows for the life of a run and is never pruned.** Safe
  because `test-results/` is gitignored and a validation pod is one-shot, so the
  directory begins empty and dies with the pod. A re-validation on a fresh clone
  starts empty again. Nothing depends on that beyond this paragraph, which is
  why it is written down.
- **Backgrounding remains available but is not prescribed.** `run_in_background`
  on Bash is un-hooked and a polling loop is permitted (the guard refuses only a
  bare `sleep` of 25s or more as a command's first segment — `SKILL.md`'s claim
  that sleep is blocked was wrong and is corrected). The skill does not offer it
  as an alternative to covering the suite in pieces, because that would be both
  halves of a choice the step already makes. It is the escape hatch if a single
  spec ever outgrows the window, which batching cannot help with.

## Deferred

The corrected diagnosis on #701 asks for two changes this does not make, and
they are deferred rather than rejected:

- **Push and open the pull request before the repair loop.** *"the change that
  prevents this run, not a defence-in-depth extra"* — had the PR existed before
  the suite loop, a severed final run would have cost a stale report rather than
  the whole cycle. `SKILL.md` step 10 still holds the only `git push`.
- **Commit as the repair loop goes.** The failing run had one commit, of the
  scaffold and test plan; every edit that made criteria pass was uncommitted, so
  even recovering the pod's filesystem would not have recovered a green suite.

Both are about *recovering* a run that ends early. This change is about a run
being able to produce a report at all, and the two are separable — but neither
is addressed here, and a reader should not infer otherwise from the fact that
#701 is referenced.

Related: ADR-0009 introduced the per-item progress this builds on; its decision 5
is superseded here. #701 carries the diagnosis, and remains open for the runner
post-condition it actually asks for — a run that exits 0 having delivered nothing
should fail loudly, which nothing here addresses.
