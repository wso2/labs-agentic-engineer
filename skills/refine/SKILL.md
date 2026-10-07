---
name: refine
description: Use for any change to existing requirements the user tells you about in plain words — adding or changing a story, a decision, a feature, an actor or a product-wide rule; settling an assumed line; answering an open question. The loop after the kickoff; also what /feature, /actor, /amend and /settle mean now.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Refine the requirements

After the kickoff there are no modes: the user edits the documents directly,
or tells you what they want changed, from wherever they are. This skill is
the second half. `prd-contract` defines where every line goes and the ID
rules; `grilling` owns the question mechanics when you have to ask.

**Every change lands directly.** There is no proposal to accept: you make the
change in the documents, in every file it belongs in, and the user sees it
land. What you decided on the user's behalf — a line they did not state —
carries `*assumed*`; that tag is their review. Your reply says what changed,
file by file, in a few lines.

## Where the message points

The instruction may open with the user's scope — the feature they were
looking at, or the design review. Read the message in that light: "claims
over $1,000 also need Finance", sent with Approvals open, is a story or a
decision in Approvals. A scope focuses and fences nothing: when the change
reaches other features, the product page or `product-wide.md`, make it there
too. With no scope, work out where it belongs from the placement rule: a line
lives where it would have to change.

## The kinds of change

- **Inside one feature** — a new story (the feature's next ID), a changed
  story or decision (same ID, new words), a removed story (its ID retired in
  the file's `## Retired`). Just make it.
- **A new feature** — a capability a user would name. Create
  `features/F<n>-<slug>.md` with the next feature ID and add its line to the
  Features list; if the user told you enough, write its stories, otherwise
  leave it a stub for its interview and say so. An idea not yet shaped enough
  to name is a Fog entry instead. Work that should not ship yet is an Out of
  Scope line.
- **A change that spans features** — an actor who reads everything, a rule
  every feature follows. Add the actor to `prd.md`'s Actors; add a rule that
  spans features to `product-wide.md` with the next P ID and its `Applies
  to:`; add the stories it implies in each feature they belong to, each
  `*assumed*` unless the user said it. A new product-wide rule reaches every
  feature it applies to — say which.
- **Moving, splitting, merging, renaming** — follow the contract's ID rules
  exactly: a moved story gets a new ID that records the old
  (`F5.1 (was F2.3)`), the old is retired with "moved to"; a merged feature is
  retired with "merged into"; a rename changes the name and the file's slug,
  never the ID. Update the Features list links.
- **Needs** — a story that now acts on another feature's work gains
  `Needs:`; one that no longer does loses it. Needs follow the stories; never
  ask the user to map them.

Ask only when the change cannot be written without an answer only the user
holds, and then one question, with your recommendation. Otherwise write it
and tag what you decided.

## Settling a point

The user clicked an `*assumed*` line or an open question (`/settle <line>`),
or opened the Open Questions list (a bare `/settle`).

- **An assumption** is a judgment already made. Put it to the user as the
  decision it is — what you decided, why, what the alternative costs. Confirmed
  → the tag comes off and the line stays. Overturned → write the new answer in
  its place, unflagged.
- **An open question** is a hole nobody has filled. Ask it; the answer moves to
  the section it belongs in and the entry leaves Open Questions. A blocking
  question's answer becomes a line in the feature's Decisions. An external
  service is a given or nothing, as the contract says: never settle a
  capability by proposing a provider.
- **Deferring** — "later", "I don't know yet" → mark the entry
  `deferred — the user will decide later` and stop raising it.

**Settling propagates.** The point does not live alone: stories it implied,
decisions that leaned on it, a scope line it justified. After the answer,
sweep every file for what the old answer held up and change it too — a story
the new answer kills is retired, never renumbered. Answering only the line
that was clicked leaves the requirements agreeing with themselves in one
place and contradicting themselves everywhere else.

## Close

Say what changed — the IDs added, changed and retired, and the files touched —
in a few lines. When a change reaches a design that already exists, say so;
updating the design is the user's next step, not this turn's.
