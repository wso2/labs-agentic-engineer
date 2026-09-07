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
Decisions taken from an org default are ordinary entries; a decision the agent
made itself, because the user has not answered it yet, ends with the literal
tag `*assumed*` — that exact emphasised word, no parentheses or brackets around
it. The console reads the tag to count what is unsettled and to draw the
Settle control on the line, so a decorated variant costs the user the ability
to challenge the judgment. The tag lives exactly as long as the user has not
answered: the moment they do — even by confirming what was assumed — the tag
comes off and the line stays as a settled decision.>

## Out of Scope
<what this project deliberately does not do>

## Open Questions
<numbered; each is a fact only the user holds — marked, never guessed. It is
resolved when its answer moves to the section it belongs in and the entry
leaves this list. An entry the user has declined for now is marked
"deferred — the user will decide later", which tells you to stop raising it.
Open questions gate nothing: the document is readable, designable and
buildable with them outstanding.>

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
- **Depth lives in feature files, and the PRD links to it.** When a feature
  needs more than its stories can carry, write
  `specs/requirements/features/<slug>.md` and keep the PRD body lean; the
  feature file elaborates, it never contradicts. Name it from the PRD as a
  markdown link on the story it deepens — `[<feature name>](features/<slug>.md)`
  — so a reader following the document arrives at the depth instead of being
  told it exists somewhere. The link text is the feature's NAME, never the
  path: the console renders the PRD, and it opens the file in place.
