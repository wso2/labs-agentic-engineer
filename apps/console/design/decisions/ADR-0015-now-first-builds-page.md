# ADR-0015: The Builds page is now-first — a glance strip, one narrated stage, and the milestone beside the run

Status: **Superseded by
[ADR-0021](./ADR-0021-builds-is-a-version-ledger.md)** — Builds is a
version ledger and the run story moved to a build detail page. §4 ("in progress"
is derived, in two strengths) survives and still governs task-row copy; §1, §3,
§5 and §6 do not. ADR-0021 also records what happens to this ADR's amendments to
ADR-0013 and ADR-0014: they do **not** revert.

Originally: Accepted. Amends ADR-0013 (version-run-surface) and ADR-0014
(build-session-spine); both remain in force except where named below.

## Context

ADR-0014's rail rendered every stage of every build session expanded — each
note, its issues, and a 420px log — numbered straight through the run. That is
the right surface for reading a finished run end to end, and the wrong one for
the question a reader actually arrives with while a run is live: what is
happening right now, and how much is left. Six expanded stages made the reader
do the scanning.

## Decisions

1. **The run card leads with a one-line glance strip.** Every stage of the
   current build session on one line, the current stage badged `now`; each
   stage keeps its note as a tooltip. Only the current stage gets words — the
   NOW panel narrates it, with the session's issues and the agent log
   collapsed to its newest line (the full log one click down; a LIVE run still
   attaches its stream unprompted, per ADR-0014 §9's fetch policy). Everything
   ahead collapses to one "Then:" sentence naming each actor.
   *Amends ADR-0014 §9 ("nothing collapses") — the fetch-on-demand half of §9
   stands; the render-everything half does not.*

2. **The strip numbers within the session, not across the run.** Run-wide
   numbering on a strip showing one session read as "step 7 of 5". Which
   session it is comes from the history below; earlier build sessions of the
   current run list under the card, earlier runs of the milestone under those,
   each collapsed to its outcome and expanding to its sessions.
   *Amends ADR-0014 §4b ("numbered straight through").*

3. **The milestone renders beside the run** — progress, count pills, issue
   groups (in progress highlighted, closed collapsed behind a count), the
   connections with `gateRows`' own who-is-acting labels, and the Ledger
   (ADR-0013 §7, unchanged in meaning, new in home). "What is happening" and
   "how much is left" are one glance apart. The flat `Issues` table below the
   fold is gone: two lists of the same issues on one page said everything
   twice.
   *Amends ADR-0014 §10 (the flat register); preserves ADR-0013 §5 (rows are
   not clickable — the one link a row carries is its GitHub issue) and §7.*

4. **"In progress" is derived, in two strengths.** Closed = `merged` (the
   two-value row vocabulary). A recorded claim — the open session's
   `resolves` — is a fact: "Claimed by build session N". Before the pull
   request exists, a live unclaimed session is PRESUMED to be working the
   open issues: "Being worked by build session N". A claim outranks the
   presumption; everything else is open.

5. **A finished flow is a result, not a rail.** When every stage is done AND
   the milestone's issues are all closed, the card shows the delivered banner
   (with the way out: deployments, validation) — keyed on the flow and the
   issue plane, never on `run.state`, which lags through validation. An open
   validation cycle is named on the banner as what keeps the run open.

6. **Progress is not a hold.** Planning and the pre-dispatch wait render as a
   quiet spinner block; `RunHoldNotice`'s leading-edge rule is reserved for
   states that need a reader. An unresolved connection mounts the
   provisioning section on the card (the surface `runHold` defers to), with
   its way out.

## Consequences

- `RunSpine` and its satellite components remain in the tree untouched as the
  reference implementation of the full rail; nothing mounts them from the
  Builds page. If no surface readopts them, they should be pruned.
- Vocabulary is unchanged from ADR-0014 §1 and the glossary: a dispatch is a
  **build session** (copy only; the model stays `cycle`), the wrapper is a
  **run**, and the bare word "session" is never used for either.
