---
name: amend
description: Use for a scoped change to an existing PRD — adding a feature, adding an actor, going deeper on a feature, or resolving open questions. The instruction names the scope; touch nothing outside it.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Amend

A scoped edit to `specs/requirements/prd.md`. The instruction names the scope —
a feature to add, an actor to add, a feature to deepen, or the open questions
to resolve. **Scope is the contract**: sections the scope doesn't touch stay
byte-identical, and story numbers only ever append (they are permanent —
designs, criteria, and tasks cite them).

The PRD's shape is defined by the `prd-contract` skill — follow it when writing.
The `grilling` skill owns the question mechanics for every branch below.

## Add a feature

Run a short scoped interview (one form: what the feature does, for whom, and
any policy it implies — skip what the instruction already says). Then:

- append the feature's stories with fresh numbers,
- write or extend `specs/requirements/features/<slug>.md` with the depth —
  a feature that should not ship yet is an Out of Scope line instead,
- record any new product decisions (org defaults answer silently, as ever).

Done when every new story has a number and an actor the Actors section
defines.

## Add an actor

Define the actor in **Actors** (product-level: name + what they can broadly
see/do). Add or amend only the stories the instruction implies for them.

## Go deeper on a feature

Expand `specs/requirements/features/<slug>.md` — interview for the missing
depth, then write it there. The PRD body gains at most new story lines the
depth surfaced; everything else lands in the feature file.

## Resolve open questions

Walk the **Open Questions** list, one `ask_question` each. Two exits per
question:

- **Answered** — the answer moves to the section it belongs in (a decision, a
  story) and the question is removed.
- **Deferred** — the user says later: mark it "deferred — does not block
  design" and leave it in place.

Done when no question is left in the undecided state.

## Close

Summarize exactly what changed — new story numbers, sections touched, questions
resolved or deferred — in a few lines. The rest of the flow (design, build)
stays untouched by this skill.
