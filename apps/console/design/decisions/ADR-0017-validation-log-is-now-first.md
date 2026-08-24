# ADR-0017: The validation log is now-first, and the line between attempts is drawn at the run boundary

Status: Accepted. Amends ADR-0013 (version-run-surface) §9, which fixed *which* cycle
section opens but never where it sits, and extends ADR-0015's now-first principle to the
validation surface. Both remain in force.

## Context

The validation logs view renders one accordion section per validation cycle. ADR-0013 §9
said "the newest expanded", and the implementation duly expanded it — at the **bottom**
of the stack, because cycles arrive oldest-first from the wire (`created_at ASC`, and the
contract documents them as "Oldest first") and the page rendered them in arrival order.
On a version that failed, was repaired and re-validated, the box a reader opened the page
for sat below collapsed history. `RunFeed`'s own comment said "the newest cycle is what
the user came to watch" while placing it last.

The console already had a settled two-tier rule for ordering, and this surface was in the
wrong tier:

- **Ledgers and histories** — separate past events — read newest-first with everything
  collapsed, and the current item is its own expanded card above them. The Builds page:
  `deliveryRuns[0]` leads as `RunStory`, `.slice(1)` becomes `RunHistoryList`.
- **Chronologies inside one thing** — log lines, chat messages — read oldest-first.

Validation attempts are the first kind, rendered as the second.

## Decisions

1. **Sections read newest-first; the lines inside them still read oldest-first.** These
   are different tiers, not two competing orderings of one thing: a section is a ledger
   entry, its lines are a transcript, and a log read upwards is unreadable. The Builds
   page already ships this combination — its NOW panel leads the page while the agent log
   inside it grows downward. ADR-0014 §4b's objection to "two orderings on one screen" was
   about two competing *numberings* of the same stages, which decision 2 keeps to one.

2. **A section's number is counted from the OLDEST cycle, never from its render
   position.** `RunFeed` numbered by array index, so reversing the stack alone would have
   renamed the newest box "Cycle 1" — the numbers would have followed the eye instead of
   the run. The ordinal is now `cycles.length - i`, so the boxes count *down* the page and
   a box keeps its name whichever end it is drawn at. This is what makes presentation
   order a free choice rather than a rename.

3. **The wire order does not move.** `created_at ASC` in the cycle repository, "Oldest
   first" in the contract, and a database test asserting it all stand; the reversal is
   render-only and the array the ordinals are counted from stays ascending. A future
   reader should not "fix" the backend to match the screen.

4. **Exactly one section is open per PAGE, not per feed — and expansion is CONTROLLED.**
   The page mounts one feed per validating run (the progress endpoint is run-keyed), and
   each would otherwise open its own newest box, so `expandNewest` is true only for the
   newest run's feed; historical feeds open nothing, matching `RunHistoryList`'s
   all-collapsed rows.

   `defaultExpanded` cannot express the rest of it. It is read once at mount, so a cycle
   arriving mid-stream opened *alongside* the box already open — two logs expanded, and
   MUI warning that an uncontrolled Accordion had changed its default. Measured in mock
   mode, that is what the surface actually did, both before this change and after the
   reorder alone. One state carries three meanings instead: `undefined` follows the newest
   cycle as the stream moves it, `null` is the reader having closed it and wanting nothing
   open, and a cycle id is the reader's own pick. The reader's choice outranks the stream —
   the same rule the task log follows when it releases its bottom-pin once the reader
   scrolls up — and a naive "re-derive the newest" default would instead re-open a box the
   reader had just shut.

5. **The line between attempts is drawn at the RUN boundary.** A caption — `EARLIER
   VALIDATION RUNS` — sits between the newest run's feed and the rest, mirroring
   `EARLIER RUNS OF V1` on the Builds page, and appears only when more than one run
   validated the version. *Rejected: a divider between the newest attempt and every older
   one.* With one run holding several attempts that divider falls between two sections of
   the same feed, so `RunFeed` would have to place a caption whose need it cannot see;
   drawing at the run boundary keeps the line on a boundary the page already owns.

6. **A section is `Run N · Cycle M`, and both numbers count only what this page shows.**
   `M` counts the run's **validation** cycles, not its cycles — which is the house
   convention, not a local fiction: `BUILD_CYCLE_KINDS = ["coding", "fix", "conflict"]`
   (`runView.ts:41`) excludes validation, and `buildSessionLabel` numbers over that
   filtered array, so the Builds page's "Build session 2" is not the run's cycle 2 either.
   `N` counts the **validating** runs, oldest first. `·` is the separator every compound
   label in the app already uses (`EarlierSessions.tsx:88`, `BuildsPage.tsx:122`,
   `RunHistoryList.tsx:173`).

   The run number is what makes a section self-describing: each feed numbers its own
   cycles from 1, so a revalidated version otherwise shows two boxes both reading
   "Cycle 1" in one flat stack, separated only by which side of decision 5's caption they
   fall on. Unlike that caption, the prefix is **unconditional** — one that appeared only
   once a second run existed would rename a box mid-session, and this page polls while a
   version is live.

   *Rejected: "Attempt N."* The word already means something else in the same row —
   `cycle.attempts` is a per-cycle re-dispatch count, rendered as "2 attempts".

   *Rejected: the run's absolute cycle position*, which `openapi.yaml:4776`'s `cycleIndex`
   supplies ("1-based position of that cycle in the run"). It is the truthful absolute
   number and that is precisely the problem: it would make validation the only surface
   using absolute positions, printing `Cycle 4` beside a Builds page that calls the same
   run's work `Build session 2`. Two schemes disagreeing about one run is worse than one
   scheme scoped per phase on both.

   *Rejected: numbering runs over the milestone's whole run list.* It would make `N`
   cross-referenceable in principle, but a run that never validated has no box here, so
   `[dev, task, validation]` would print `Run 3` and `Run 1` with no
   `Run 2` anywhere. There is nothing to cross-reference against anyway: no other surface
   numbers runs — `RunHistoryList` and `RunStory` identify one by its kind chip and
   timestamp, and `runView.ts`'s `runKindLabel` states that the kind "is the only thing
   that tells them apart".

## Consequences

- **The Builds page is untouched.** `RunFeed`'s only production consumer is the Validation
  page; the Builds page renders cycles through `RunNowPanel` / `EarlierSessions` /
  `RunHistoryList` and filters validation cycles out via `buildCycles`. That `RunFeed`
  lives under `features/builds/` while only validation mounts it is a misplacement worth
  its own decision, not resolved here.
- The caption's markup is now a **third** byte-identical copy of the one in
  `EarlierSessions` and `RunHistoryList`. It was kept local deliberately, to avoid
  dragging two Builds-page files into a validation change; a shared component is the right
  move at a fourth caller.
- Controlling expansion (decision 4) also retires a long-standing console warning about an
  uncontrolled Accordion changing its default. It was not planned work: the reorder was
  verified in mock mode, the page showed two open logs against a decision that said one,
  and the state was the only way to make the decision true rather than aspirational.
- **The multi-run caption is not reachable in mock mode.** No fixture has two runs that
  both validate, and the progress handler streams `runs[0]` regardless of the `runId` it
  was asked for, so every feed on the page would show the same run. Unit tests cover the
  arrangement; making it drivable by hand means fixing that handler and adding a
  two-validating-run scenario.
