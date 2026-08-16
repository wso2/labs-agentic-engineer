---
name: grilling
description: Use when an instruction or skill asks you to interview, grill, or clarify with the user before generating — the structured-question mechanics for ask_question and ask_questions, in one-form or session mode — or when the user accepts a "grill me on…" offer.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Grilling

Interview mechanics. A sharp question up front is cheaper than a wrong
document; during an interview, asking **is** the job — the usual "only ask when
you cannot proceed safely" restraint does not apply.

## The tools

Each form ends your turn; the user's answers arrive as the next message.

- **`ask_questions`** — a form of several INDEPENDENT questions answered
  together. Up to 8 per form — a ceiling, not a target.
- **`ask_question`** — one question, when the next question depends on this
  answer.

## Writing questions

- Ask only questions whose answers **change the artifact**. Everything else is
  either already answered (the brief, an earlier answer, an org default) or
  yours to assume.
- Give **0–5 concrete options** and mark exactly **one** `recommended: true`.
  Add a `description` only where the label alone is ambiguous, and keep it to
  ONE short clause — the form is generated in full before the user sees any of
  it, so every extra sentence across every option is time they spend waiting on
  a blank screen. `multiSelect: true` only when several options genuinely
  co-apply.
- The form always offers free text, so pass an **empty options list** when the
  answer must be typed — never invent placeholder options like "Other".
- Options are a starting point, not a cage: never smuggle in a constraint the
  user didn't state; ask instead.

The answers are the authoritative brief — treat them as decisions, not
suggestions. Free text in ANY answer may overturn an earlier answer — the
latest statement wins; reconcile the document to it on your next write.

## Modes

How many forms an interview may spend is the **calling flow's rule**, not this
skill's. Two modes exist; the calling skill or the user's request picks one.

### One-form mode (the default)

Some flows allow exactly **one** form — `/start`'s coverage walk, a scoped
amend. Obey it: converging early is always allowed, asking a second form never
is. Everything a single form cannot carry goes through the skip valve as an
`*assumed*` recommendation.

### Session mode (the calling flow opts in)

A **grilling session** interrogates a named scope across several rounds. Run
one when a flow explicitly opens a session — the user accepting a "grill me
on: …" offer, a deep-dive on an `*assumed*` decision, a design clarification
that names it.

- **Open with the scope.** Name the session's scope as an **area checklist**
  ("Grilling Favorites: ownership · limits · privacy") in your prose before
  the first round. Areas come from the request plus what the document shows
  is unsettled.
- **Rounds are adaptive small batches**: one `ask_questions` form of **1–4
  questions** — independent questions ride together, genuinely dependent
  chains split across rounds. Never a full survey.
- **Send the checklist every round**: pass the form's `session` field —
  optional short `title`, plus the FULL `areas` list with per-area `state`
  (`done` = settled, `now` = this round asks into it, `todo` = still ahead).
  Keep area names stable; only states move.
- **Write as you go**: the turn that asks round N+1 FIRST applies round N's
  settled decisions to the document, then asks. Writing and asking share a
  turn — the live document is the progress indicator. A free-text override of
  an earlier answer is reconciled in the same write.
- **Converge — you declare the end.** Stop when the remaining questions would
  no longer change the artifact. There is no round cap and no length guard;
  the only-artifact-changing-questions rule is what bounds the session.
- **Close with a summary.** After the final write, end with a message that
  opens with the line
  `**Session summary** — <N> asked · <M> assumed`
  (N = questions the user answered, M = decisions filled from your
  recommendations and tagged `*assumed*`), followed by the per-area outcomes.
  Name the areas still carrying `*assumed*` decisions — the summary doubles as
  the next deep-dive offer.

## Ending the interview

- **Converged**: the remaining unknowns no longer change the artifact — stop
  and generate (in session mode: write, then the summary above).
- **Finish valve**: every form carries "Finish — use recommendations", and the
  user may equally type "just generate" / "skip". Stop asking immediately.
  Answers the message lists as given are the user's decisions; apply your
  recommended answer to every other listed question AND every remaining
  undecided area, tagging each one `*assumed*` where it lands — per the
  `prd-contract` rules: end of the line it qualifies, one token per line, any
  section. In session mode, still close with the summary.
- **Headless**: the turn states no interview is possible — ask nothing;
  generate on stated assumptions, each marked `*assumed*`.
