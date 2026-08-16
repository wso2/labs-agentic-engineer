---
name: prd-contract
description: The shape of specs/requirements/prd.md — its sections in order, the story-numbering rules, and what the PRD deliberately excludes. Use whenever writing or amending the PRD.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# The PRD contract — specs/requirements/prd.md

The PRD speaks **product language**: what the system does and for whom, never
how it is built. Engineering altitude begins at `specs/design/`.

## Sections, in order

```markdown
# <project name> — PRD

## Problem Statement
<who hurts, how, and what today's workaround costs — a short paragraph>

## Solution
<what this product is, in one paragraph a stakeholder can repeat>

## Actors
<one bullet per actor: name + what they can broadly see/do, product-level.
Every actor cited by a story is defined here first.>

## User Stories
<a single numbered list, the spine of the document:>
1. As a <actor>, I want <feature>, so that <benefit>.
2. …

## Product Decisions
<policy choices at product altitude: sign-in approach, notification channels,
which external services the product depends on (by capability, e.g.
"transactional email" — binding to a concrete provider happens at design).
Decisions taken from org defaults or the skip valve are ordinary entries;
skip-valve entries carry the *assumed* tag.>

## Out of Scope
<what this project deliberately does not do>

## Open Questions
<numbered; each is something nobody could answer yet — marked, never guessed.
An open question is resolved (answered, and its answer moved to the right
section) or explicitly deferred ("deferred — does not block design") before
/design proceeds.>

## Further Notes
<anything real that fits nowhere above; omit the section when empty>
```

## Rules

- **Story numbers are permanent.** New stories append with fresh numbers;
  numbers are never reused or renumbered — designs, criteria, and tasks cite
  them.
- **Every statement lands.** Everything the user said in the brief or the
  interview appears somewhere above — as a story, a decision, an
  out-of-scope line, or an open question. A user statement with no home is a
  defect.
- **Actors before citation.** A story only names actors the Actors section
  defines.
- **The story list is total.** Every story the PRD defines ships. Work that
  should come later is an Out of Scope line, or it is not a story yet.
- **No acceptance criteria.** Validation criteria live in
  `specs/validation/validation-criteria.json` — the single acceptance oracle.
  The PRD never duplicates them.
- **Depth lives in feature files.** When a feature needs more than its stories
  can carry, write `specs/requirements/features/<slug>.md` and keep the PRD
  body lean; the feature file elaborates, it never contradicts.
