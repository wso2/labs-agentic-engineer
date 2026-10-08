---
name: start
description: Use when kicking off a project from its idea — the product pass that agrees the feature list and writes the product page, the product-wide page and a stub per feature — or re-running /start on a project that already has requirements.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Start

A user arrives with one sentence and leaves with a map of their product: the
product page, the product-wide page, and one file per feature. This is the
**product pass**. It agrees what the product is and which features it has; it
writes no stories. Each feature is then interviewed on its own (`/interview
F<n>`, the `interview` skill), in the order the product needs them, and the
user can stop after any one.

**The document arrives early and is refined in place.** Ask what the product
page cannot be written without, write it, then ask against what it now says.
Nobody can react to a document that does not exist yet.

## The idea comes to you

The user's idea is attached to this instruction when the project captured one.
Read it as the brief — it is what the user actually asked for, in their words.
It is not a file: it is attached or it is absent. When absent, open with one
`ask_question`: "What are you building?" — a few concrete example options,
free text welcome. The answer is the brief. Getting the brief is not the
interview: it is what the interview starts from.

## Reference documents outrank the idea

Some kickoffs list reference documents — files the user attached when they
created the project, named in the instruction by path. When they are listed,
**read every one before you plan anything.** They are the primary brief; the
typed idea is the anchor that says which part of them matters.

Every listed document is already in front of you: text documents are in your
workspace files, and PDFs and images are attached to this conversation
natively — look at an attached mockup or form, don't just acknowledge it. Never
fetch a reference document through a repository or MCP tool — a binary fetched
as text is garbage, and the tool will refuse it anyway.

Read them, then take the coverage walk against what they say:

- **Do not ask what a document already answers.** A document that settles a
  section settles it — the walk records the answer and moves on. Attaching a
  20-page spec and then being asked its contents back is the failure this
  channel exists to prevent.
- **Interview only where the documents are silent, ambiguous, or contradict
  each other.** A contradiction between two documents is a real question, and
  a good one: quote both and ask which holds.
- **Cite what informed what.** A line a document states carries that
  document's source tag, with the page, sheet or heading (`prd-contract`), and
  keeps the document's own words where they are already a requirement. The
  user must be able to see their material landed, and a later reader must be
  able to trace a decision to its source.
- **Write each document's coverage file** (`sources/<document>.md`,
  `prd-contract`) in the same pass: every point that matters to the product,
  place by place, with where it landed or why it did not. A point you leave out
  of the requirements is still listed, as `not used:` with the reason — that
  line is the user's one chance to see it was read and set aside.

An Office document arrives converted to markdown (`Policy.docx.md`): cite and
title it by its own name, `Policy.docx`, and its headings, sheets or slides as
the place.

No documents listed is the ordinary case: the instruction says nothing and
you interview from the idea alone, exactly as below.

## The walk

Walk the product page's own sections, in order, silently and in full before
the user sees a single question:

1. **Problem** — who hurts, how, today.
2. **Actors** — who uses the system, at product altitude.
3. **Features** — the capabilities a user would recognise by name
   (`prd-contract`, "What a feature is"). Every story the brief implies belongs
   to exactly one of them; a shared concept is its own feature; a vague idea is
   a Fog entry, not a feature.
4. **Product-wide** — what applies to more than one feature: sign-in,
   retention, currency, audit, performance (see **External services** and
   **Agents** below).
5. **Out of scope** — what this product is explicitly not.

For each section:

- **Consult the organization skill first.** A question its defaults answer is
  never asked — record the default as a line tagged `[org default]` instead. A
  section fully covered by defaults and the brief needs nothing.
- **External services: givens, never choices.** For each capability the
  product needs from a third party (payments, email, shipping, maps…), call
  `list_external_resources` first: a Registered External resource that fits
  is a given — record it as a settled decision from an org default, without a
  question. Otherwise the one question is whether the user already uses or
  must use a service for it; a named answer is a settled decision ("Currency
  conversion: Open Exchange Rates"), "no preference" leaves the capability
  only. Never ask which service they would LIKE, never propose one, never
  tag a provider `*assumed*` — the choice is made on the dependency's
  definition at design, with the design agent's suggestions in front of them.
- **Agents: suggest one where it fits.** On this platform, work an LLM does is
  done by an agent. Where the product holds such work (reading documents or
  images, sorting free text, summarising, drafting, answering in the user's
  own words…), suggest an agent for it once, unless the brief already asks for
  one. A yes is a decision naming what the agent does, in the feature it
  serves; a no is an Out of Scope line. An agent runs on the organization's
  own model connection, so it needs no `list_external_resources` call and no
  provider question.
- **Guardrails: propose one where an agent needs it.** Once an agent is
  agreed, call `list_guardrail_policies` for the guardrails this platform can
  apply, and match them against what the agent's work puts in front of it —
  personal data it should not see, content or requests the brief rules out.
  Propose each that fits once, in plain words, unless the brief already
  decides it. A yes is a decision in the feature the agent serves; a no is an
  Out of Scope line. Where nothing fits, ask nothing.
- **Note the questions whose answers would change the product page**, and only
  those. Questions about how one feature behaves wait for its interview.

## Ask, write, then ask once more

`grilling` owns the question mechanics and the pacing.

1. **Ask the spine — one `ask_questions` form.** The spine is who the actors
   are and which features the product has. Propose the feature list rather
   than asking for one: *"I see Submit expenses, Approvals, Payroll export and
   Notifications. Anything missing, or anything that is really two?"*
2. **Write the moment those answers land**, per `prd-contract`:
   - `specs/requirements/prd.md` — every section, the Features list linking
     each feature's file, the Fog when there are vague ideas;
   - `specs/requirements/product-wide.md` — the product-wide requirements
     (`P1`, … each with `Applies to:`) and decisions;
   - one stub per feature, `specs/requirements/features/F<n>-<slug>.md`: its
     heading, its Purpose, and its `Needs:` line when every story of it will
     act on another feature's work. No stories — those are the interview's.
   A line you decided because the user has not answered carries `*assumed*`.
3. **Ask the next round in the same turn as the write.** The lines you just
   tagged `*assumed*` whose answer would change the feature list or a
   product-wide rule are that round's agenda, widest blast radius first.
   Writing is not converging, and a turn never ends on a document whose
   assumptions nobody has seen. Any answer settles the tag, including picking
   the very answer you assumed.
4. **Close with the order.** When the feature list and the product-wide rules
   stand, say which feature to interview first and why — the one the others
   act on ("Submit expenses first: Approvals and Payroll export act on
   submitted claims") — and stop. The console offers that interview; interview
   order is not build order.

## An unanswered form stays live

A form the user walked away from keeps its questions owed: when anything else
arrives while one stands, re-present that form and wait for the answer.

## Running /start again

`specs/requirements/prd.md` already exists → this is a **change**, never a
rewrite: follow the `refine` skill. Regenerate from scratch only when the user
explicitly asks, and confirm before overwriting.

## Where this stops

`/start` ends at the product map: stories are each feature's interview, and
design, components and tasks are later steps with their own skills. Close
when the feature list and product-wide rules have converged, never merely
because the files now exist. The closing paragraph names what is still
`*assumed*` and every open question — both ordinary states that hold nothing
up — and the feature to interview first.
