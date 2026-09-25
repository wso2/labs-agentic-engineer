# ADR-0033: Validation is a version ledger, and a section on it is an attempt

- **Status:** Accepted
- **Date:** 2026-09-23 ([#823](https://github.com/wso2/labs-agentic-engineer/issues/823))
- **Amends:** [ADR-0017](./ADR-0017-validation-log-is-now-first.md) — decision 5
  (the caption's wording), decision 6 (`Run N · Cycle M`), and the scope of
  decision 4 (one open section per *page*). Decisions 1–3 stand unchanged:
  sections read newest-first, a section's number counts from the oldest, and
  the wire order does not move.
- **Builds on:** [ADR-0013](./ADR-0013-version-run-surface.md) (a version is one
  supervised run's story), [ADR-0015](./ADR-0015-now-first-builds-page.md)
  (now-first), [ADR-0016](./ADR-0016-validation-verdict-vocabulary.md) (the
  verdict vocabulary, including §7's two lifecycle states),
  [ADR-0021](./ADR-0021-builds-is-a-version-ledger.md) (the shape this borrows).

## Context

The Validation page showed one version — the newest milestone — and derived
which version that was from whatever the project status said. A version
validated last week was unreachable: no list, no page, no way to ask its
criteria again.

Two things then changed underneath it. The platform split judging out of the
delivery loop: a dev run delivers a version and mints its validation task, and
a **validation run** judges it. And a version can be judged repeatedly — after
a repair, or because somebody asked again — so "the verdict" is a property of
*an attempt*, of which a version has a history.

ADR-0017 designed the log for the surface as it was then: one page, one run's
cycles, the word "cycle" for a section. Every one of those premises has moved.

## Decisions

1. **The surface is two levels: a ledger, and a page per version.** The ledger
   is one row per version the platform has worked, including versions never
   validated — the row a reader most needs to find. This is ADR-0021's shape,
   deliberately: a reader who has learned Builds already knows this page.

2. **The server says which run answers for a version.** `ValidationStageFromRun`
   / `ValidationStageWithCycle` live beside the verdict vocabulary in
   `delivery`, and the project-status aggregate and the validation read model
   both call them. Two copies were two ideas of when a version counts as
   judged, which is how the page and the deployments board came to disagree
   about one run ([#423](https://github.com/wso2/labs-agentic-engineer/issues/423)).
   The console derives none of it.

3. **A section is an ATTEMPT, headed `Attempt N`, counted from the oldest across
   the whole version.** *This replaces ADR-0017 §6.* That decision rejected
   "Attempt N" on one ground — `cycle.attempts`, a per-cycle re-dispatch count,
   rendered as "2 attempts" in the same row — and that collision is removed by
   rewording the count to **`started N times`**, which is what it always meant.

   The positive case is that "cycle" and "run" are now both wrong for a reader.
   Post-split an attempt *is* a run, so `Run N · Cycle M` numbered two things
   where one exists; and a run holding two cycles means only that the agent
   merged without a report and the platform dispatched again — a remedy, not a
   distinction to put in a heading. The number is version-wide because it is
   what a reader refers to, and it must not restart at 1 because the platform
   opened a new run.

4. **The caption is `EARLIER ATTEMPTS OF <tag>`.** *This replaces ADR-0017 §5's
   `EARLIER VALIDATION RUNS`.* One wording on both cards, naming the unit the
   page thinks in. §5's placement rule survives in a stronger form: the line is
   drawn after the newest ATTEMPT on both cards, not after the newest run,
   because the report card's sections and the log card's feeds now number the
   same way.

5. **One open section per CARD, not per page.** *This narrows ADR-0017 §4.*
   That rule was written when the log stack was the page's only accordion list.
   With a report list above it, per-page means opening a report collapses the
   log you opened it to read — and reading the two together is the normal act.
   The three-valued state (`undefined` follows the newest, `null` is the reader
   having closed everything, a string is their pick) is unchanged.

6. **The newest attempt sits apart; the history is one fitted block.** MUI
   rounds an accordion's corners by `:first-of-type` / `:last-of-type` among its
   siblings, so the newest attempt gets its own parent (all four corners) and
   the older ones share one (rounded only at the ends, under the caption). A
   flat list with the caption spliced in gave the newest a square bottom and the
   first older one a square top, with a label through the seam.

7. **An expanded attempt leads with its own numbers**, one line:
   `6 passed — All 6 scenarios were settled and passed.` The counts come from
   the report the section already fetched. Not on collapsed headers, which would
   need every older report fetched up front.

8. **A running attempt shows no stale counts.** The card reads the attempt's own
   verdict, which is empty while it runs, so the previous attempt's numbers
   cannot appear marked `(last attempt)`. That marking is still right on the
   deployments rail, which has no way to show past attempts; this is a change of
   mind for one surface, not for `lib/verdict.ts`.

9. **Only the newest attempt's evidence is fetched with the page.** The rest load
   on expand, gated by `enabled`. A snapshot carries a whole report plus every
   feature file at that commit.

## Consequences

- **`RunFeed` grew a `label` seam.** It is shared with Builds, whose unit is
  still the cycle, so the default is unchanged and the validation page passes
  its own heading. The kind chip is dropped when a feed shows a single kind,
  which is derived inside the feed rather than asked for by a caller.
- **The page's vocabulary is in the lexicon** — attempt, the card titles, the
  ledger's columns and filters, `Revalidate`. The page never says *cycle* or
  *run*.
- **Builds still numbers `Run N · Cycle M`** and rounds each older run's feed
  separately. Bringing it in line is deliberately not done here.
- **ADR-0017 stands otherwise**, and its decisions 1–3 are the reason this page
  reads the way it does at all.
