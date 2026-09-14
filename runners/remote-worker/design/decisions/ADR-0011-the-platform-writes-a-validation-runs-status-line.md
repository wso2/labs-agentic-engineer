# ADR-0011 — The platform writes a validation run's status line, not the agent

**Status:** Accepted

## Context

ADR-0010 made an issue's newest comment its status line and put the actor
working the issue in charge of it. On a validation run that did not happen.

`p42-todo-list-bare`, measured:

| | |
|---|---|
| issue #7's comments | **two**, 3h07m apart |
| the first | 08:29:56, the agent's step-1 opener |
| the second | 11:36:30, the platform's own closing line |
| the cycle | 08:28:57 → 08:58:30; the run was cancelled at 11:36:31 |

Between them the run drove the deployed app with `playwright-cli` — what
`aep-validation/SKILL.md` itself calls "the longest stretch of this phase and the
only one nothing else can see into" — and nothing said so anywhere.

The cause was structural and this repo had already written it down. The
obligation for the middle of the run lived in a `## The status line` section
sitting *beside* the numbered workflow, and only steps 1 and 10 carried an actual
imperative. Those two are exactly the two comments the run produced. ADR-0010's
own consequences name the shape:

> "a rule stated only in a section *beside* a numbered sequence gets skipped, so
> the obligation is named in the sentence the loop's steps hang off"

The `aep` skill got that treatment. `aep-validation` did not. And `f863d0d6`,
which shipped the feature, said as much at the time: *"the skill half is unproven
until an agent keeps the line on a live validation."*

What made the silence expensive rather than cosmetic is that nothing else is
durable. ADR-0009 measured the progress feed as a sliding window — at most 200
events from the last 64 KiB of pod stdout — so *"a page opened 90 minutes into a
7200s validation cycle shows `Pending` for criteria that already passed, and
neither a refresh nor a reconnect recovers them."* The issue's comments are the
only record of a run that survives the run.

## Decisions

1. **For a VALIDATION run the status line is inferred from tool calls, not
   declared by the agent.** This extends ADR-0009's decision 3 — "an instruction
   to report progress is one the agent can skip with nobody noticing for a whole
   run" — from `progress_item` to the issue comment.

   ADR-0010 declined that extension, on the grounds that "nothing can infer prose
   about intent from a `Write` call". That reasoning holds for a coding run and
   does not hold here, and the difference is what bounds this decision. A coding
   run's beats are judgements — "todo-api builds clean" is a claim about work
   nobody watched. A validation run is one agent, on one issue, walking a fixed
   ten-step procedure whose every beat is a call it must make. ADR-0010 decision
   2 stands unchanged for every other run kind.

2. **Five rungs, three of them borrowed.** `exploring`, `authoring` and `running`
   ARE `ProgressItemStatus` values, read through the same derivation the
   console's per-criterion rows use (`validation_progress.ts`), sharing one
   state. One vocabulary over two surfaces: a row and the line above it cannot
   disagree about what the run just did. `harness` and `reporting` bracket them
   and match a command each, because neither is about a criterion.

3. **Each line names the EVIDENCE, not the step.** This is what answers
   ADR-0009's rejection of phase markers, whose objection was that the workflow's
   steps interleave: `authoring.md` has the run testing all through step 6, so
   the first `npm test` fires while it is still authoring. A line claiming "step
   7 has begun" would be wrong for an hour; "Running automated tests against the
   deployed system" is true when posted and never false in hindsight.

4. **A one-way ratchet, plus a repair MODE for the loop.** The middle of a run
   oscillates by design, and the first rule shipped did not survive contact with
   it: posting whenever the rung changed turned twelve criteria — each walking
   exploring → authoring → running — into thirty-six lines, which exhausts the
   cap around the fourth criterion and leaves the rest of a two-hour run in the
   silence this ADR exists to end. Healing walks the last two rungs again for
   every repair, so it compounds.

   So rungs are one-way: forward is news, behind is not. Criterion churn and
   healing cost nothing, and the console already draws both per criterion.

   The one thing genuinely worth reporting behind the mark is step 9's exit 2 —
   "the ordinary loop, not a defect", where the generator names specs with no
   result and the run covers them and regenerates, possibly several times. That
   is NOT a rung. It is a mode the run is in, entered on the generator's own
   FAILED outcome (via the translator's tool-outcome seam, the same one the rows
   settle on) and sticky until one succeeds. While it holds, every rung is
   silent, because re-running a spec and generating again ARE the repair.

   Ranking it as a sixth rung was the obvious alternative and is wrong: it would
   make the repair a place to fall from and climb back to, which is precisely the
   oscillation the mode absorbs.

   The result is at most six lines per cycle however many times the generator
   refuses. `MAX_POSTS` survives as a backstop for the failure nobody predicted —
   the last one was a rung matching a `cp` — and it now WARNS on the run's feed
   when reached, because a ladder that stops looks exactly like a run that
   finished.

5. **A third comment class, `<!-- aep:observed -->`.** `MachineCommentMarker`
   could not carry this: the surfaces built for people DROP machine comments,
   because those are the platform writing for the agent. This is the platform
   writing for a person. Posting unbranded was the alternative and would have put
   platform prose in the channel documented as "what a person wrote or an agent
   said" — with no way to ever measure whether the agent's own half works.

   `aep:status` was the first candidate and is taken: `aep:status/*` is the issue
   LABEL namespace. "Observed" is the honest word anyway.

6. **Newest wins, whoever wrote it; the class labels, it does not re-rank.**
   Preferring the agent's line is worse than it sounds — its first act is the
   step-1 opener, so an agent-first rule would pin "Starting validation…" over
   the whole run. The console labels the agent's line rather than the platform's:
   the platform's is the common case and the pulse beside it already says a
   machine is working, while the agent's carries a judgement and is the one a
   reader must not mistake for mechanism.

7. **The skill's status-line section is DELETED, and nothing replaces it.**

   It shipped first as an override — `alwaysOnSkills("validation")` is
   `["aep", "aep-validation"]`, both bodies ride the system prompt, so removing
   the section leaves `aep`'s "keep an issue's line current" in force with
   nothing saying the platform already does it. The override existed to stop the
   agent narrating on top of the ladder.

   That was 11 lines of prompt on every run to prevent a behaviour never once
   observed. The agent SKIPPING its middle lines is the entire premise of this
   ADR; over-posting them has no evidence behind it at all. And if it ever
   happened, newest-wins means a judgement supersedes a mechanical line, which is
   the precedence you would choose anyway.

   The section's own words were "Everything between them is this line" — the
   middle was its whole job, and the platform now has it. What a validation run
   still says is what it always said, from the two places it always came from:
   step 1's opening comment and step 10's closing summary, both step-anchored,
   which is exactly why those two are the pair that reliably happen.

## Rejected

- **Editing one comment in place** rather than posting up to five. Tidier for the
  read window (`CommentsPerIssue` = 10) and would hold the count at three. It
  loses the timestamps a person reading the issue actually wants, and an edited
  comment stops being the newest the moment anything else is posted — which is
  the whole mechanism. Revisit if the count becomes a problem.
- **Posting through the BFF.** It would keep marker-stamping inside
  `issueService`, the single writer every platform comment passes through. It
  costs a new internal endpoint and a round trip on the agent's critical path,
  and the runner needs the trigger either way. The marker literal is duplicated
  across the language boundary instead, pinned by a test on each side — the same
  bargain `runread`'s `validationReportPath` and `validation_progress.ts`'s
  `AC_ID` already make.
- **A `healing` rung.** It is an existing `ProgressItemStatus` and would cost one
  line. Left out because the rows say it per criterion, which is better than one
  run-wide line, and because every rung spends a slot in the read window.
- **Doing this for coding runs.** Their beats are judgements. That is precisely
  what ADR-0010 says cannot be inferred, and nothing measured here contradicts
  it.

## Consequences

- **A failed post costs the line, never the run.** `gh` is awaited so the line
  lands before the silence it explains, but a failure is reported on the feed and
  swallowed. A two-hour validation must not die because it could not be watched.
- **A pod that cannot resolve `gh`, or a dispatch carrying no issue number, keeps
  no line at all** — and both are ordinary, not faults. The run behaves exactly
  as it did before this ADR.
- **The console's derived line gained a third window.** It used to say nothing
  through the long middle, on the reasoning that the rows carry it. They do, per
  criterion; the run-wide count is the thing no row can show, and a blank tile
  over a live run reads the same as a run that stopped.
- **The agent's opening comment is not guaranteed, and was not before either.**
  p44 posted none. That first looked like fallout from the override, and the
  record says otherwise: p37 missed it under the ORIGINAL skill, while p40, p41
  and p42 posted it. One miss in four before, one in one after — no attribution
  either way, and the pod log that would settle it had already rotated. What is
  lost when it is missed is now only the run's stated PLAN ("9 need new specs"),
  for the minute before the first scaffold write; the console's own derived line
  covers that window from the rows, so nothing on screen goes blank.
- **The last rung stands stale at the end.** After `reporting` the run pushes and
  opens its pull request, and nothing posts again until the agent's closing
  summary. That is the same guarantee ADR-0010 already gave, on more lines.

Related: ADR-0009 is what a run records to be watched per criterion; ADR-0010 is
what an issue's newest comment means. This is what happens when the actor those
two relied on does not write one.
