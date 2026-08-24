---
name: grilling
description: Use when a flow skill sends you to interview the user — how to write a structured question, how many rounds an interview may run, and how it ends.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Grilling

Interview mechanics. A sharp question up front is cheaper than a wrong
document; during an interview, asking **is** the job — the usual "only ask when
you cannot proceed safely" restraint does not apply.

## A flow skill always owns the document

This skill owns the question tools and nothing else — it has **no artifact
contract**, so fired on its own it produces well-formed questions with nowhere
to put the answers. The document belongs to a flow skill: `start` for
requirements from an idea, `amend` for a scoped change to them, `design` for
the design. Each loads this one for the mechanics.

Reached directly, hand it back: name the flow that covers what the user wants,
and let them fire it.

## The tools

Each form ends your turn; the user's answers arrive as the next message.

- **`ask_questions`** — a form of several INDEPENDENT questions answered
  together. Up to 8 per form: a screenful, not a budget. What does not fit goes
  in the next round.
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
suggestions.

## How many rounds

**As many as the work needs** — no cap on questions, forms, rounds or sessions,
for requirements and for design alike. Revisiting something answered earlier is
legitimate when what you have written since changed what the answer has to
carry; say what changed when you do.

What keeps that from being an interrogation is the **order**, not a budget:

- **Reach the document early.** Ask first what the artifact cannot be written
  without, write it, then refine it in place. The calling skill says what its
  own document needs first.
- **Every round after the first reacts to something concrete** — a line the
  document now carries, an answer that opened a new choice, two statements that
  contradict. A question that could have been asked before the document existed
  belongs in the first round. Without that, more rounds are strictly worse than
  the single form they replace.
- **No denominator.** State where the work stands by what the document says —
  *here is what I have; what is wrong with it?* A convergence-based interview
  has no total, so any count you offer ("question 3 of 5", "one last round") is
  a fiction.

## Ending the interview

- **Converged**: the remaining unknowns no longer change the artifact — stop
  and generate.
- **Recommended answers**: the user says "just generate" / "skip", or takes the
  form's own exit — stop asking, apply your recommended answer to every
  remaining decision, and tag each one `*assumed*` where it lands in the
  artifact. This ends the round, not the conversation: a flagged assumption is
  a decision the user can see and overturn, which is what makes it a deferral
  rather than a loss. A silent one is an invention.
- **Deferred**: the user says they know and will decide later — write "deferred"
  against it and stop raising it. This is the one answer that does close a line
  of questioning for good.
- **Headless**: the turn states no interview is possible — ask nothing;
  generate on stated assumptions, each marked `*assumed*`.

A judgment the agent can make is assumed and flagged. A fact only the user
holds — a URL, a package, which vendor they have a contract with — is never
assumed: it goes to the document's open questions, because an invented API URL
does not fail at review, it fails at build.
