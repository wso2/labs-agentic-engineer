# ADR-0002 — Send keeps the prototype review open; the revision lands in it

**Status:** Accepted · 2026-10-06
**Supersedes:** [ADR-0001](ADR-0001-prototype-review-is-a-full-screen-overlay.md)
on "the overlay closes on success" (Send closes the overlay and opens the chat).
Spec: #885 (#889).

## Context

Closing the review on Send sent the reviewer to the chat; to see what the
agent changed they had to come back, reopen the review and find their place.
The prototype's status already says when a `/prototype` turn is revising it
and when it is ready or invalid again, and the chat store reports how each
turn ended.

## Decision

1. **Send keeps the review open.** The batch is held beside the review (in
   `PrototypeWorkspace`, since the overlay unmounts on close) until its turn
   ends; the queue starts again, keeping drafts. While the turn runs the bar
   says `Agent is revising… (N comments)`, Send is disabled, and comments
   written meanwhile are held in the queue.
2. **The shown revision is frozen while revising.** The room's files follow
   the agent's writes mid-turn; the review keeps showing the last ready
   revision until the prototype stops revising.
3. **Revising → ready swaps in place:** same screen, role and state when they
   still exist, else the role's entry screen (the kit's `MANIFEST_REPLACED`);
   the mock data starts from the new seed; an `Updated · N comments addressed`
   toast offers What changed (closes the review, opens the chat). Held
   comments whose elements are gone are flagged, to remove or keep as a
   whole-screen comment.
4. **A failed turn or an invalid revision** keeps the last ready revision
   showing and puts the sent batch back in front of the queue, with the
   reason and Retry. The outcome is the chat store's `onTurnEnd`; a turn whose
   stream was lost reports none, and the prototype's status (ready or
   invalid) decides.

Rejected: locking the prototype during the turn, a manual Reload banner,
asking each time, and queueing a second turn automatically.

## Consequences

- The review's session (queue, held batch, shown revision, notice) lives as
  long as the Prototype tab, like the queue before it; leaving the tab
  mid-turn forgets the held batch.
- The chat panel still opens on Send, behind the review; there is no
  scroll-to-turn yet, so What changed opens the chat at its latest turn.
