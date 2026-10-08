---
name: prd-contract
description: The shape of the requirements under specs/requirements/ — the product page prd.md, one file per feature, product-wide.md — the ID rules, the tags a line can carry, and what the requirements deliberately exclude. Use whenever writing or amending the requirements.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# The requirements contract — specs/requirements/

The requirements speak **product language**: what the system does and for whom,
never how it is built. Engineering altitude begins at `specs/design/`.

## The files

Requirements are `prd.md`, `product-wide.md` and one file per feature.

```
specs/requirements/
  prd.md                          the product page: frame, actors, feature list
  features/F1-submit-expenses.md  one feature: its stories, decisions, questions
  features/F2-approvals.md
  product-wide.md                 everything that spans more than one feature
  product-wide/security.md        a topic split out of product-wide.md (optional)
  sources/t-e-policy-v3.md        what one attached document says, and where it landed
```

**One home.** Each story, decision, out-of-scope line and open question lives
in exactly one of these files: the one where it would have to change. One
feature's item goes in that feature's file; an item spanning more than one
feature goes in `product-wide.md`; the product's frame goes in `prd.md`.
`prd.md` holds no stories. There are no per-story detail files.

- "A receipt is required above $25" → `features/F1-submit-expenses.md`
- "A deputy approves while the manager is on leave" → `features/F2-approvals.md`
- "All amounts in company currency" constrains three features → `product-wide.md`
- "No mobile app" is out of scope for the product → `prd.md`; "no approval
  chains beyond Finance" is out of scope for Approvals → its file

## What a feature is

A **feature** is a capability of the product that a user would recognise by
name. The features divide the stories between them: **every story belongs to
exactly one feature**, the one that must change if the story changes. A shared
concept gets its own feature for the stories about the concept itself
(Notifications: "choose email or Slack"); each use of it stays with the job it
serves ("the manager is notified when a claim is submitted" is Approvals').

Every product has at least one feature — a five-story to-do app has one,
**Todos**. A feature is not a design component: Approvals may touch the API and
the web app.

## prd.md — the product page

```markdown
# <product name>

## Problem Statement
<who hurts, how, and what today's workaround costs — a short paragraph>

## Solution
<what this product is, in one paragraph a stakeholder can repeat>

## Actors
<one bullet per actor: name + what they can broadly see/do, product-level.
Every actor any file cites is defined here, and only here.>

## Features
- F1 [Submit expenses](features/F1-submit-expenses.md)
- F2 [Approvals](features/F2-approvals.md)

## Fog
<ideas not yet shaped into features, one bullet each, no IDs. An entry leaves
the Fog once it can be named with a purpose: it becomes a feature, several, or
folds into an existing one. Omit the section when empty.>

## Product-wide
<one line linking [Product-wide](product-wide.md)>

## Out of Scope
<what the product deliberately does not do>

## Open Questions
<questions about the product's frame: its actors, its feature list>
```

The feature list is one line per feature, in ID order: its ID, then a relative
link to its file, so the repository reads correctly on GitHub. Rename a feature
and you rename its file and update its link; its ID stays.

## features/F&lt;n&gt;-&lt;slug&gt;.md — one feature

```markdown
# <feature name>

## Purpose
<one or two lines: what it does and for whom>

Needs: F1.

## User Stories
- F2.1 As a manager, I see my team's pending claims in one list, oldest first.
- F2.2 As a manager, I approve or reject a claim with a reason.

## Decisions
- A claim is approved by the employee's line manager.

## Out of Scope
- Approval chains beyond Finance.

## Open Questions
1. Does a deputy see claims submitted before the manager's leave began?
```

The file is named `F<n>-<slug>.md`: the feature's ID, then its name in
lowercase words joined by hyphens. It names actors; it never defines them.

A feature that has been named but not interviewed is a **stub**: its file holds
only the heading and the Purpose.

**Write a section only when it has content** — in every file, apart from
`prd.md`'s Problem Statement, Solution, Actors and Features and a feature's
Purpose. Never write an empty heading, a placeholder or a comment in its place.
A section the user emptied may keep its heading; leave it as it is.

Any of the three kinds of file can end with two more sections, once they have
content: `## Further Notes`, for anything real that fits nowhere else, and
`## Retired`, the file's retired IDs (see IDs below).

## product-wide.md — more than one feature

```markdown
# Product-wide

Rules that apply to more than one feature.

## Requirements
- P1 Every approval, rejection and edit is recorded in an audit log. Applies to: all.
- P2 Expense records are kept for 7 years. [T&E Policy v3 · p.10] Applies to: all.
- P3 Amounts are in the company currency, stored in cents. Applies to: F1, F3. *assumed*

## Decisions
- One currency only; no multi-currency.

## Open Questions
<questions whose answer would change a product-wide line>
```

- A **P item** is a requirement the built product must meet across features:
  cross-cutting behaviour (the audit log), data and compliance (retention),
  performance and scale, security, data rules (currency). Each carries
  `Applies to:` and the feature IDs it constrains, or `all`. A build that
  picks a feature carries the P items that apply to it.
- A **decision** here is a policy choice that shapes several features without
  being built on its own.
- A requirement that concerns one feature only ("the approval screen loads in
  under a second") is a decision in that feature, not a P item.
- A large concern with its own user stories is a feature, not a P item. "Every
  edit is logged" is a P item; "an auditor searches the log and exports a year"
  is someone using it — a feature.
- The file always exists. When one topic grows too large to read inside it,
  move that topic to `product-wide/<topic>.md` (same sections) and leave a
  one-line link in its place; `product-wide.md` stays the entry point. P IDs
  are global, so a move changes no citation.
- Where the user says "fast", write a measurable version tagged `*assumed*`
  ("pages load in under 2 s").

## sources/&lt;document&gt;.md — what a document gave

When the user attached documents, each gets a coverage file: what it says,
place by place, and where each point landed in the requirements — or that it
landed nowhere and why. It is how the user sees their material was read, and
the one place that says what was left out.

```markdown
# T&E Policy v3.pdf

Pages: 14

- p.3: A receipt is required for any expense above $25. → F1.5
- p.4: Meals are capped at $50 per day. → F1
- p.9: Approved claims post to Xero every night. → F3.2
- p.10: Expense records are kept for seven years. → P2
- p.12: Corporate cards are reconciled monthly. → not used: corporate cards are out of scope
```

- The title is the document's name exactly as it was attached (a converted
  Office file keeps its own name: `Rates.xlsx`, not `Rates.xlsx.md`). The file
  name is that name as a slug.
- `Pages:` is how many pages it has; leave the line out for a document with no
  pages (a sheet, a deck counts slides).
- One line per point that matters to the product, in the document's order: the
  place (a page `p.3`, a sheet `Rates tab`, a slide `slide 4`, a heading), what
  it says in its own words, then `→` and the ID of the line or the feature it
  landed in, or `not used:` and why. A point that landed is cited by that line
  too (`[T&E Policy v3 · p.3]`).
- It is written with the requirements it explains, and changes when they do: a
  point moved to another feature moves its arrow.

## IDs

- **Features** are `F1`, `F2`, … — numbered in the order they are created.
- **Stories** are `F<n>.<m>`, numbered within their feature: `F2.3` is the third
  story ever numbered in Approvals.
- **Product-wide requirements** are `P1`, `P2`, … — one sequence across
  `product-wide.md` and its topic files.
- A new ID is one above the highest its sequence has **ever** used, `## Retired`
  entries included. Retired numbers leave gaps.
- **An ID is born once and retired once, never edited or reused.** Names and
  wording change freely; the ID stays. Designs, acceptance criteria, tasks and
  issues cite IDs — `F2`, `F2.3`, `P4` — never names or file slugs.
- **Retire** an ID by removing its line and recording it in the `## Retired`
  section at the end of the file that held it:

  ```markdown
  ## Retired
  - F2.3 moved to F5.1
  - F2.6 dropped
  ```

  A retired feature is recorded in `prd.md`'s `## Retired`
  (`- F4 Spending reports merged into F3`, `- F6 Budgets dropped`) and its file
  is deleted; its stories retire with it.
- **Moving a story** retires its ID and gives it a new one in its new feature,
  which records the old right after the ID: `- F5.1 (was F2.3) As a manager, …`.
- **Splitting a feature** makes a new feature; only the stories that move are
  retired and renumbered. **Merging** moves one feature's stories into the
  other and retires the emptied feature. **Renaming** changes the name and the
  file's slug, never the ID.

## Lines

A story, decision or P item is **one line**: what the product does is the list,
not an elaboration of it. A story names its actor: "As a manager, I …". After
its words a line can carry, in this order:

```
- F1.2 As an employee, I pick a category for each expense. [T&E Policy v3 · p.3] Needs: F5. *assumed*
- P3 Amounts are in the company currency. [T&E Policy v3 · p.2] Applies to: F1, F3. *assumed*
```

1. **Sources** — `[<document> · <place>]`, where the line came from: a page
   (`[T&E Policy v3 · p.4]`), a sheet (`[Limits.xlsx · Approvals tab]`), a
   heading, or a fact the agent checked (`[Xero API docs]`). A line taken from
   an organization default carries `[org default]`. What a document states is
   the user's own word: it carries its source and is not assumed.
2. **Needs or Applies to** — a P item's `Applies to:` goes here (see
   product-wide.md above). On a story, `Needs:` and the feature IDs it waits
   on (`Needs: F5.`): only this story needs that feature; its siblings do not. When every story
   of a feature needs another feature, write the need once, as the Purpose
   section's last line (`Needs: F1.`), not on each story. Needs are derived
   from what the stories say, never asked of the user: a wrong need means a
   wrong story, and fixing the story fixes the need.
3. **A closing tag** — `*assumed*` or `*blocking*`, the literal emphasised word
   with nothing after it but punctuation, and no brackets or parentheses around
   it. The console reads these tags; a decorated variant is invisible to it.

**`*assumed*`** closes a line the agent decided because the user has not
answered it yet. The console counts these lines as the user's to confirm and
draws the Settle control on them, so the tag lives exactly as long as the user
has not answered: the moment they do — even by confirming what was assumed —
the tag comes off and the line stays as a settled line. An inference beyond
what a document states is assumed; the document's own statement is not.

### Decisions about external services and agents

A decision about an external service names it by capability ("transactional
email"). A concrete provider appears only as a given the business already holds
— a Registered External resource of the organization (`[org default]`, written
without asking) or a service the user said they already use or must use
("Payments: Stripe — finance has the account"). With no such given the line
stays capability-only; the agent never proposes a provider, and a provider line
is never tagged `*assumed*` — choosing a service is the user's, on the
dependency's definition at design.

An agent is a decision whose line says what the agent does ("Ticket category:
suggested by an agent"); it runs on the organization's own model connection, so
the line names no provider or model.

A guardrail is a check the AI gateway applies to an agent's model traffic —
one `list_guardrail_policies` offers — and its line says what is guarded, not
how ("Guardrail: contact details are masked before the model sees them"). A
rule the agent or the app itself follows is an ordinary decision,
never a "Guardrail:" line.

## Open questions

An open question is a fact only the user holds — marked, never guessed. The
list is numbered and lives in the file its answer would change. It is resolved
when its answer moves to the section it belongs in and the entry leaves the
list. An entry the user has declined for now is marked
`deferred — the user will decide later`, which tells you to stop raising it.

Open questions gate nothing, with one exception. A **blocking question** stops
one feature's interview — every story depends on its answer — and never a
build. It lives in that feature's Open Questions, closed by `*blocking*`, with
the answers to choose from as indented bullets:

```markdown
## Open Questions
1. Does finance post to one Xero organisation, or one per country? *blocking*
   - One organisation for every claim
   - One per country; a claim goes to the employee's country
```

When it is answered, the entry leaves Open Questions and the answer becomes a
line in the feature's Decisions. No blocking state is kept anywhere but the
file.

## Rules

- **Every statement lands.** Everything the user said in the brief, the
  documents or the interview appears somewhere above — as a feature, a story, a
  decision, a P item, a Fog entry, an out-of-scope line, or an open question. A
  user statement with no home is a defect.
- **Actors before citation.** A story only names actors `prd.md` defines.
- **A feature's stories are total.** Every story a feature holds ships when the
  feature is built. Work that should come later is an Out of Scope line, a Fog
  entry, or not a story yet.
- **No acceptance criteria.** They live under `specs/validation/acceptance/` —
  the acceptance oracle. The requirements never duplicate them.
