---
name: interview
description: Use to interview the user about ONE feature of an existing product — the /interview F<n> flow that turns a feature's stub into its file of stories, decisions and questions. The instruction names the feature.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Interview one feature

The product pass (`start`) has written the product page, the product-wide
page and a stub per feature. This turn takes ONE feature — the instruction
names it (`F2`, or its name) — from a stub to a file the rest of the flow can
design from. Everything else stays as it is.

Read before you ask: `specs/requirements/prd.md`, `product-wide.md`, the
feature's file, the files of the features it needs, and any reference
document the project has. The `prd-contract` skill defines what the file
holds and every rule its lines follow; `grilling` owns the question
mechanics.

## The spine, then the file

1. **Ask the spine — two or three questions, one `ask_questions` form.** The
   spine is what this feature's stories cannot be written without: who does
   what in it, and the one or two choices every story leans on. Skip what the
   product page, the product-wide page, an organization default or a document
   already answers — and say so in a line, so the user sees why it was not
   asked ("Your policy already says claims go to Xero every night (p.9), so
   I won't ask about timing.").
2. **Write the feature's file the moment the answers land** — the whole file,
   per the contract: Purpose, `Needs:`, User Stories with the feature's next
   IDs, Decisions, Out of Scope, Open Questions. A line a document states
   carries its source and is not assumed. A line you decided because the
   user has not answered carries `*assumed*`, and your reply gives the reason
   for each, in a clause.
3. **Stop there.** The console walks the assumed lines with the user one at a
   time, and then offers the next feature; neither is yours to do in chat.
   Close with one sentence: how many stories and decisions the file has, and
   how many lines are assumed.

**Writing is not converging, but the walk is the round.** Do not ask a second
form about the file you just wrote: the assumed lines are the questions, and
the user answers them on the lines themselves.

## When the interview cannot go on

A **blocking question** is one whose answer changes every story of the
feature — Xero or ADP for a payroll export. Do not guess it and do not write
stories on top of a guess. Put it in the feature's Open Questions, closed by
`*blocking*`, with the two to four answers you would offer as indented
bullets (prd-contract, "Open questions"), leave the rest of the file a stub,
and say in one sentence what the interview waits on. When the user answers
it, the answer becomes a line in Decisions, and a later `/interview` of the
feature starts from there.

## Check facts yourself

A fact the world holds — what an external service's API allows, a published
limit, a regulation's retention period — you check with the tools you have
(web search, the organization's registered resources), without asking first.
Record what you found as a line with its source (`[Xero API docs]`); a choice
you draw from it is still the user's, so it carries `*assumed*`. Only the
user holds their own policies and preferences: those are questions, never
research.

## Stay in the feature

The feature's file is the one you write. A statement the user makes about
another feature, or about the whole product, lands where it belongs — the
other feature's file, `product-wide.md`, the product page — and your closing
sentence says which other files you changed. A need on a feature that is
still a stub is fine: record it (`Needs: F5.`) and move on.
