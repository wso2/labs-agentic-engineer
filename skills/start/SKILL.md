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
asked about nor visibly assumed.

**The document arrives early and is refined in place.** Ask what the PRD cannot
be written without, write it, then keep asking against what it now says. The
job is not to extract a document in one pass — it is to guide the user to their
own requirements, and nobody can react to a document that does not exist yet.

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
- **Cite what informed what.** Where a PRD section rests on a document, say so
  in the section, by filename. The user must be able to see their material
  landed, and a later reader must be able to trace a decision to its source.

No documents listed is the ordinary case: the instruction says nothing and
you interview from the idea alone, exactly as below.

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

Then split what the walk noted in two:

- **The spine** — what the PRD cannot be written without: who the actors are,
  and the shape of the journey they take. Stories, decisions and scope all hang
  off these, so a wrong answer here rewrites the document.
- **Everything else** — a policy choice, an edge case, the depth of one
  feature. Real questions, all of them, but the PRD can exist and be read
  without their answers.

## Ask, write, then keep asking

`grilling` owns the question mechanics and the pacing. What follows is the
order this flow runs them in.

1. **Ask the spine.** One `ask_questions` form carrying the spine questions the
   walk noted — usually two or three. Anything the walk noted that is not spine
   waits; it is a better question once the document exists.
2. **Write the PRD the moment those answers land.** The whole document, every
   section, per the contract below — not a stub and not a draft. Questions you
   have not asked yet are filled with your recommended answer, tagged
   `*assumed*` where each lands; a fact only the user holds goes to Open
   Questions instead.
3. **Ask the next round in the same turn as the write.** The decisions you just
   flagged `*assumed*` are that round's agenda: take up every one a user could
   plausibly answer differently, say where it landed, and ask. Widest blast
   radius first — an assumption touching something the user already told you,
   or deciding scope, outranks an edge case you raised yourself. The turn that
   writes the PRD also asks about it, so the user reads document and question
   together — writing is not converging, and a turn never ends on a document
   whose assumptions nobody has seen.

   **Any answer settles the flag.** The tag marks a decision the user has not
   answered; once they answer — including by picking the very answer you
   assumed — it is their decision and `*assumed*` comes off that line, with
   the decision kept. Offer the assumed answer as an ordinary choice, never a
   "keep it as assumed" option: confirming is settling, and a round that ends
   with the same tags it began with did no work.
4. **Keep going until you converge.** Each round amends the PRD in place — it
   is the running record, never a draft you rewrite at the end, and story
   numbers are permanent from the first write on. Later rounds take up what the
   document exposed: a section the walk left thin, a story whose actor is
   undefined, something the user's own answers opened. Converge when every flag
   still standing would change nothing but its own line if the user overturned
   it — the same blast-radius test, run to decide you are done.

Depth is opt-in: the user can go deeper in chat on any feature at any point.

## An unanswered form stays live

A form the user walked away from keeps its questions owed: when anything else
arrives while one stands, re-present that form and wait for the answer.

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
before overwriting. A scoped change earns fewer questions than a cold start —
the document is already there to ask against, so ask narrowly and often rather
than broadly and once.

## Where this stops

`/start` ends at the PRD: design, components, and tasks are later steps with
their own skills and gates. It does **not** end at the first PRD — that is the
middle of the flow, not the end of it. Close when the interview has converged
(above), never merely because the file now exists.

Closing is a one-paragraph summary of the decisions taken, calling out every
`*assumed*` one and every open question still standing. Both are ordinary
states for a PRD to be in — neither holds the design up — so say what is
unsettled, not what it blocks.
