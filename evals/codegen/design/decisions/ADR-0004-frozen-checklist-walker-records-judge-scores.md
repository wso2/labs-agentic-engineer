# ADR-0004: The checklist is frozen; the walker records, the judge scores

**Status:** Accepted (2026-10-05)

## Context

A codegen attempt is scored by walking the running app against a checklist of
user-visible behaviours. Three agents take part: the planner writes the
checklist, the walker drives the browser, and the judge reads the walk. If the
checklist moved between attempts, or if the agent that made the walk also gave
the number, two attempts would not be comparable and the score would not be
trusted.

## Decision

**The planner runs once, at save.** `pnpm eval save` derives `checklist.yaml`
from the case's specs and commits it. Only an explicit `pnpm eval replan`
derives it again (hand-added extras are kept). A rubric derived per attempt
would move with the model, and two attempts would be scored against two
different lists. The planner has read-only tools and never sees generated
code, so the list describes what the spec promises, not what one build did.

**The checklist is hand-editable and strictly parsed.** Every schema in
`case.ts` refuses an unknown key, so a misspelled `wieght:` or `mustNto:`
fails at load instead of scoring every attempt against a list nobody wrote.
Roles are checked against the case's roles at load, before anything is spent.

**The walker is a black box that records.** It sees only the running app,
never fixes or works around what it finds, and a mutation counts only when a
request leaves the page: a row that changes on screen with no request behind
it is a fail.

**The judge is a separate call from the walker.** The walker is invested in
the run it just made, and the number must not be its own. The judge has no
tools: it sees exactly the evidence it is handed. Per item it says pass or
fail and the user-visible symptom, never a cause. Root cause is a later stage
with the whole archive in front of it; a cause guessed from a walk transcript
would put a confident wrong answer where the next reader starts.

**The score is computed in code** (`score.ts`), never by a model:

- weighted pass ratio × 100 over the checklist items plus the `mustCover` extras;
- an item the walker reported `blocked` is a fail, even if the judge passed it:
  a screen a user cannot reach does not work, and a judge that passes an item
  nobody could attempt is wrong;
- an item the judge returned no verdict for is a fail, named "not judged",
  rather than dropped from the denominator (which would raise the score);
- a violated mustNot caps the band at `review`, whatever the ratio, the same
  rule as `evals/spec-agents`.

## Consequences

- A change to the specs does not change a saved case's checklist until someone
  runs `replan`. A rewalk scores old code against the current checklist.
- Hand edits are safe to make and fail loudly when malformed.
- The report carries symptoms only. Explaining a failure is a separate pass
  (the `analyzing-evals` skill) over the archive.
- A judge mistake in the walker's favour cannot raise the score past what the
  walk showed.
