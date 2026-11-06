# ADR-0006 — An assumed technical decision is kept in place or changed by a comment

**Status:** Accepted · 2026-11-01
**Amends:** [ADR-0005](ADR-0005-the-design-card-reviews-the-designs-files.md)
(what Checked hashes for `decisions.md`). **Issue:**
[#959](https://github.com/wso2/labs-agentic-engineer/issues/959).

## Context

The design agent tags each technical decision it made for the user with
`*assumed*` in `specs/design/decisions.md`, and the tag stays until the user
confirms or changes the decision. The spec editor settles an assumed
requirement with Keep, Remove and I'll edit, because a requirement is its
words. A technical decision is not: the architecture, data model and
contracts already follow it. The Design page drew the file read-only and
offered no way to settle any of its lines.

## Decision

1. **An assumed technical decision offers Keep and Change…, nothing else.**
   Keep drops the tag in place, through the Room. Change… opens the file's 💬
   box with the line quoted above it, read-only, also when the draft is
   edited; the comment goes to the agent with Address comments. The agent
   rewrites the decision in its own words, and the line can come back still
   assumed: those words are the agent's reading of the comment, so the user
   rereads them and keeps them like any other assumed decision.
2. **The Design page stays a review surface.** `decisions.md` gets its own
   read-only viewer; no design file is typed into on the Design page.
3. **Dropping an `*assumed*` tag is never a change.** It already moves no
   design basis and changes no build; now it also leaves a Checked file
   checked: the hash of `decisions.md` is taken of the file as it reads once
   every decision is kept, made by Keep's own edit.

## Rejected

- **The spec editor's Remove and I'll edit on a decision.** Deleting or
  retyping the line changes no artifact that makes the choice.
- **Mounting the spec editor on `decisions.md`.** It makes a design file
  typeable and brings the spec's decorations.
- **Keep all.** Each Keep is a decision someone read.
- **An editable quote.** Once edited, it no longer names the decision the
  comment is about.
- **Marking a line whose Change… comment waits.** A comment is not pinned to a
  spot in a file (ADR-0005); matching its quote to a line breaks once the line
  is reworded.
- **Unticking Checked on a Keep.** The words someone checked are still the
  words in the file.

## Consequences

- A new hash or diff over spec or design text strips the tag first.
- The Overview's Technical decisions row counts the decisions still assumed.
  An assumed decision does not hold Build.
