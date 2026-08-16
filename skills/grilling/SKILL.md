---
name: grilling
description: Use whenever questions go to the user — an instruction or skill that says to interview, grill or clarify before generating, and any user message asking you to grill them, go deeper, dig in or pin down the details on something (a bare "yes" to a standing "grill me on…" offer included); owns ask_question / ask_questions and picks one-form vs multi-round session mode.
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
  together. Up to 8 per form — a ceiling, not a target, and one-form mode's
  ceiling alone: a session round takes **1–4** (see Modes).
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

Every interview runs in one of two modes, and **the mode is settled before the
first question**. Read the trigger below before you write a single question:
the two modes have different budgets, different question counts and different
tool input, so picking the mode after drafting the form means drafting it
twice.

### Session mode

A **grilling session** interrogates a named scope across as many rounds as it
takes. Run one when **either** trigger fires:

**1. A flow opens one.** `amend`'s "go deeper on a feature", a deep dive on an
`*assumed*` decision, any instruction that says to grill.

**2. The user asks for depth on a scope.** Their message pairs

- a **depth ask** — "grill me", "grill me properly", "go deeper", "dig in",
  "drill into", "interrogate", "pin down the details", "properly", "in
  detail", "really nail" — with
- a **scope** — a named area ("the voting and nomination rules"), several
  areas, or "all" / "everything".

Accepting a standing "grill me on: …" offer is this trigger, however short the
reply: "yes", "all", "do it", "sure", or just naming an area from the offer.

A depth ask carrying no scope still opens a session — you name the scope:
the areas holding `*assumed*` decisions, plus the thinnest sections of the
document. Say which you picked in the opening line so the user can redirect
you. **Never answer a depth ask in one-form mode** — that is the failure this
mode exists to prevent. (A session that converges after one round is fine: it
carried the checklist and it closes with the summary.)

A request to *change* something, or an ordinary question about the document, is
not a depth ask. Do the work; do not open a session.

Running a session:

- **Open with the scope, in the turn that asks round 1.** One short line of
  prose naming the session and its areas ("Grilling voting & nominations:
  eligibility · quorum · nominee limits"), then the round-1 `ask_questions`
  call in the same turn. A turn that announces a plan and asks nothing spends a
  round trip on nothing. Pick **2–6 areas**, named as the user would name them,
  from the request plus what the document shows is unsettled.
- **Rounds are 1–4 questions.** One `ask_questions` form per round:
  independent questions ride together, genuinely dependent chains split across
  rounds. **Four is a ceiling, not a target** — a fifth question is not a
  judgement call, it belongs to the next round. The tool's 8-question limit is
  one-form mode's budget and a session never spends it.
- **Every round carries the checklist**, the first one included. Pass the
  `ask_questions` `session` field:

  ```
  session: {
    title: "Voting & nominations",
    areas: [
      { name: "Eligibility",    state: "now"  },
      { name: "Quorum",         state: "todo" },
      { name: "Nominee limits", state: "todo" }
    ]
  }
  ```

  `areas` is the FULL list on every round — names stay byte-identical for the
  session's life and only `state` moves (`done` = settled by an earlier round,
  `now` = this round asks into it, `todo` = still ahead). At least one area is
  `now`. **A round sent without `session` is not a session round**: the user
  sees a plain one-form interview and loses the scope entirely.
- **Write as you go**: the turn that asks round N+1 FIRST applies round N's
  settled decisions to the document, then asks. Writing and asking share a
  turn — the live document is the progress indicator. A free-text override of
  an earlier answer is reconciled in the same write. The document's own
  contract skill governs those writes (`prd-contract` for the PRD) — load it
  if you are not already holding it.
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

### One-form mode (the default)

Everything the session triggers do not catch. Some flows allow exactly **one**
form — `/start`'s coverage walk, `amend`'s add-a-feature and add-an-actor
branches. Obey it: converging early is always allowed, asking a second form
never is. Everything a single form cannot carry goes through the finish valve
as an `*assumed*` recommendation.

A depth ask that arrives *after* such a form is not a second form of that
interview — it is a session the user asked for, and the one-form rule does not
reach it.

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
