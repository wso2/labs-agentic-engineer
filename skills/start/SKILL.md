---
name: start
description: Use when kicking off a project from its idea, or re-running /start on a project that already has a PRD.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Start

A user arrives with one sentence and leaves with a PRD the rest of the flow can
build on. The interview is a **coverage walk**: the PRD's own sections are the
plan, you visit every one, and nothing lands in the document that was neither
asked about nor visibly assumed. You visit them all before you ask anything —
**`/start` asks exactly once**.

## The idea comes to you

The user's idea is attached to this instruction when the project captured one.
Read it as the brief — it is what the user actually asked for, in their words.
It is not a file: it is attached or it is absent. When absent, open with one
`ask_question`: "What are you building?" — a few concrete example options,
free text welcome. The answer is the brief. Getting the brief is not the
interview: it is the only thing that may precede the one form below.

## The coverage walk

Walk the PRD's own sections, in its own order:

1. **Problem** — who hurts, how, today.
2. **Actors** — who uses the system, at product altitude.
3. **Journey & stories** — what each actor does, end to end.
4. **Product decisions** — policy choices: sign-in, notifications, integrations.
5. **Out of scope** — what this project is explicitly not; anything that should
   not ship now belongs here, not in the story list.

The walk is **planning, not turns**: you take it silently, in full, before the
user sees a single question. For each section:

- **Consult the organization skill first.** A question its defaults answer is
  never asked — record the default as a plain Product Decision instead. A
  section fully covered by defaults and the brief needs nothing.
- **Note the questions whose answers would change the document**, and only
  those. Skip what the brief already answers.

## Ask once

Then ask **ONE `ask_questions` form** — the `grilling` skill owns the question
mechanics — carrying the questions the walk noted, and write the PRD from the
answers. There is no second form: the next thing the user sees after answering
is their document.

- **The bar, not the budget.** Ask only what changes the PRD. Three questions
  is a good form; padding one out to the cap is an interrogation.
- **More questions than a form holds** → ask those whose answers change the
  document most, and send every one left behind through the finish valve.

Depth is opt-in — the **offer** is not. Anything you assumed earns one in the
closing message (see "Where this stops"), and whenever the user asks for depth
on an area, then or later, that opens a `grilling` session, not another form.

## The finish valve

At any point the user may say "just generate" / "skip". Stop asking
immediately: fill every remaining decision with your recommended answer and tag
each one `*assumed*` where it lands in the PRD. An assumption the user can see
is a decision they can overturn; a silent one is an invention.

The valve answers an **ask** — the user's own words, whichever ones they choose.
An unanswered form keeps its questions live: when anything else arrives while
one stands, re-present that form and wait for the answer it is owed.

## Write the PRD

Write `specs/requirements/prd.md` — always that full path. Follow the
`prd-contract` skill exactly: it defines every section, the story numbering
rules, and what the PRD deliberately excludes. Per-feature depth goes to
`specs/requirements/features/<slug>.md`, never into the PRD body.

Anything genuinely unanswerable now goes to **Open Questions** — mark it, never
guess it.

## Running /start again

`specs/requirements/prd.md` already exists → this is an **amendment**, never a
rewrite: append new stories with fresh numbers (story numbers are permanent),
update only the sections the change touches, and leave the user's hand-edits
alone. Regenerate from scratch only when the user explicitly asks, and confirm
before overwriting. One form here too — a scoped change earns fewer questions
than a cold start, never more.

## Where this stops

`/start` ends at the PRD. Design, components, and tasks are later steps with
their own skills and gates.

Close the turn in exactly this order — three parts, and **the offer comes
before the next step**. A message that ends on "run `/design`" reads as the
turn's one instruction, and the deep dive is never taken:

1. **The summary** — one short paragraph of the decisions taken, calling out
   every `*assumed*` one.
2. **The offer** — the assumptions, counted and named, as a question the user
   can answer with one word:

   > I assumed 3 things — want me to grill you properly on: notifications ·
   > permissions · data retention? Say "yes" for all, or name the areas.

   Name the areas you assumed or thinned, in the user's own vocabulary. With
   nothing assumed, skip this part entirely — never invent areas to offer.
3. **The next step** — review `specs/requirements/prd.md`, then run `/design`;
   open questions must be answered or explicitly deferred before design can
   proceed.

Any reply accepting the offer — "yes", "all", an area name, or any other ask to
go deeper — opens a **grilling session** on those areas: run the `grilling`
skill's session mode, scoped to their `*assumed*` decisions, with the areas as
the session's checklist. Do NOT answer that reply with one more form. The
one-form rule binds the quick pass only, never a session the user asked for.
