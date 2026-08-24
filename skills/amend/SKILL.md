---
name: amend
description: Use for a scoped change to an existing PRD — adding a feature, adding an actor, or going deeper on a feature. The instruction names the scope; touch nothing outside it.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Amend

A scoped edit to `specs/requirements/prd.md`. The instruction names the scope —
a feature to add, an actor to add, or a feature to deepen. **Scope is the
contract**: sections the scope doesn't touch stay byte-identical, and story
numbers only ever append (they are permanent — designs, criteria, and tasks
cite them).

Revising a point the document already carries is the `settle` skill's job, not
this one: that edit propagates by design, and propagation is the opposite of
the promise made above.

The PRD's shape is defined by the `prd-contract` skill — follow it when writing.
The `grilling` skill owns the question mechanics for every branch below.

**The document is already there to ask against**, so amend starts where `start`
has to arrive: every question can name the line it would change. Write each
answer in as it settles rather than banking them to the end, and keep the edit
inside the scope — rounds are cheap, a widened edit is not.

## Add a feature

Interview for what the feature does, for whom, and any policy it implies —
skipping what the instruction already says. Then:

- append the feature's stories with fresh numbers,
- write or extend `specs/requirements/features/<slug>.md` with the depth, and
  link it from the story per the contract — a feature that should not ship yet
  is an Out of Scope line instead,
- record any new product decisions (org defaults answer silently, as ever).

Done when every new story has a number and an actor the Actors section
defines.

## Add an actor

Define the actor in **Actors** (product-level: name + what they can broadly
see/do), then add or amend **only the stories their arrival implies**.

Two entrances reach this branch, and the second is the common one:

- **The user asked for the actor.** The instruction is the brief, and the
  stories it implies are the ones it names.
- **You noticed one mid-conversation.** *"Managers approve them"* names a
  Manager the Actors section has never defined, and their stories are already
  under discussion. Define the actor in the same turn those stories are
  written, so every story names an actor the document defines.

## Go deeper on a feature

Expand `specs/requirements/features/<slug>.md` — interview for the missing
depth, then write it there. The PRD body gains at most new story lines the
depth surfaced, plus the contract's link to the file if the story does not
carry one yet; everything else lands in the feature file.

## Close

Summarize exactly what changed — new story numbers, sections touched — in a few
lines. The rest of the flow (design, build) stays untouched by this skill.
