---
name: settle
description: Use for settling an unsettled point in an existing PRD — challenging an assumption the agent flagged, or answering an open question nobody has filled. The instruction names the point; a bare instruction means the whole Open Questions list.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Settle

One unsettled point in `specs/requirements/prd.md`, taken to a decision. The
instruction names the point — the line the user clicked. A bare instruction
means the **Open Questions** list.

The PRD carries **two kinds of unsettled**, and they are not the same job:

- **An assumption** is a judgment the agent already made. It is flagged
  `*assumed*` and it is doing real work in the document. Settling it
  **challenges an answer that already exists**.
- **An open question** is a hole nobody has filled — a fact only the user holds,
  which is why it was never guessed. Settling it **fills it for the first time**.

The `grilling` skill owns the question mechanics; `prd-contract` owns the shape
of what you write.

## Revision propagates

**This is the whole job.** The point does not live alone: the document was
written on top of it — stories it implied, decisions that leaned on it, an Out
of Scope line it justified. So settling is one question, then a **sweep for
everything the old answer held up**.

Amending only the line the user clicked leaves the PRD agreeing with itself in
one place and contradicting itself everywhere else. Story numbers stay permanent
through all of it: a story the new answer kills becomes an Out of Scope line,
never a deleted number.

The sweep ends at the PRD. When a settled point would also change a design that
already exists, say so in the close and leave it to the user.

## Challenging an assumption

Put the assumption to the user as the decision it is — what you decided, why,
and what the alternative costs — then:

- **Confirmed** — the tag comes off. It was a real decision all along; it is now
  the user's.
- **Overturned** — write the new answer in its place, unflagged, and sweep.

## Answering an open question

Ask it. The answer **moves to the section it belongs in** — a decision, a story,
a scope line — and the entry leaves Open Questions. An answer parked in the
Open Questions list is not an answer.

A bare instruction walks the list this way, one `ask_question` per entry,
including the deferred ones — the user opened the section, so they are taking
it up.

## Deferring

*"Later"*, *"I don't know yet"*, *"stop asking"* — mark the entry
`deferred — the user will decide later` and leave it in place.

Deferral is an **outcome of this conversation**, never something the user
reaches for on its own, and it gates nothing: it is the user's way of saying
*stop raising this*. Honour it — a deferred point stays out of later rounds
until the user brings it back.

## Close

Say what settled and what moved with it — the point, where its answer landed,
and every other line the sweep changed. A sweep that changed nothing else says
so, in a line.
