# ADR-0052 — A design's review state is a file in the spec repo

**Status:** Accepted · 2026-10-11 · Amended 2026-11-02 (point 2: the
technical decisions' hash)
**Related:** [ADR-0051](ADR-0051-design-is-a-review-loop-and-build-reads-the-prototype.md)
(the review loop),
[ADR-0040](ADR-0040-a-design-goes-out-of-date-per-feature.md) (the
finished-turn ledger),
[console ADR-0005](../../apps/console/design/decisions/ADR-0005-the-design-card-reviews-the-designs-files.md)
(the Design card).
**Detail:** `packages/agent-stream/src/contracts/design-review.ts` (the shape
and lifecycle), [`apps/console/design/design-card.md`](../../apps/console/design/design-card.md).

## Context

The review loop needs three things for each generated file: comments, a
Checked tick that a new revision clears, and the agent's replies. The console
needs them live, so teammates see each other's comments. The agent needs them
in the run that addresses them. Before this, none of it existed: comments were
mock-only, and there was no checked state and no revision number.

## Decision

**`specs/design/review.json`, one file in the project's spec repo, holds the
review. There are no aep-api tables.** The console reads and writes it through
the Room, the agent reads and writes it with its file tools, and git keeps its
history.

1. **Shape.** The file is `{ version: 1, artifacts: [{ path, checkedHash?,
   comments? }] }`. An artifact is a file that Design generates, keyed by its
   repo-relative path. A prototype is one artifact, keyed by its
   `prototype.json`. The artifacts are a list keyed by path, not a map,
   because the Go schema interpreter validates each entry of a list but not
   the values of a map.
2. **Checked is a content hash.** It is the lowercase hex SHA-256 of the
   file's UTF-8 text as stored. For a prototype it is the kit's
   `prototypeHash` of the two files. For `specs/design/decisions.md` it is
   taken without the `*assumed*` tags (amended below). A changed artifact unticks itself, and
   nothing else clears the tick.
3. **A comment** has an id, an author (the platform user's id and name), its
   text and `createdAt`. Its status moves `draft` → `sent` → `addressed`, and
   an addressed comment can be `reopened`. Some fields depend on that state:
   - a sent comment has `sentAt`;
   - an addressed comment has `reply`, `revision` and `addressedAt`;
   - only a prototype's comments have an `anchor` (screen, flow, role, state
     and element ids, from its full-screen review);
   - `question` is the agent's, on a comment it leaves `sent`, and never on a
     draft;
   - `answer` exists only with a `question`. The user's answer reopens the
     comment.
4. **Who writes what.**
   - The console writes drafts, edits, Reopen, Checked and answers. Address
     comments marks every waiting comment `sent` in one write before it starts
     the turn, and puts them back if the turn does not start. The console
     always writes the whole file and validates it first.
   - The agent acts only on sent and reopened comments, and never on a draft.
     It writes the replies and questions, and it writes the review file last
     in its run.
5. **One schema, three gates.** The schema is `designReviewSchema` (Zod) in
   `@aep/agent-stream`. It is published as
   `packages/contracts/schemas/design-review.schema.json` and vendored into
   `designspec` in aep-api and in ae-studio-tools. All three gates apply the
   same rules across entries: unique paths and comment ids, a reply on every
   addressed comment, an anchor only on a prototype's comments, no question on
   a draft, and no answer without a question. What each gate does with a
   malformed file:
   - the agent's write gate refuses the write;
   - the Room's commit (ae-studio-tools) commits it with a warning;
   - aep-api's design save gate refuses to tag it.
6. **Revision numbers are not stored here.** An artifact's revision is the
   number of completed turns that changed it. Every finished turn records the
   paths its own writes changed (`changed_paths` in the ledger), and
   `GET /spec/state` serves the counts as `artifactRevisions`, keyed as this
   file keys artifacts. `revision` on an addressed comment records the
   revision of the run that addressed it.
7. **The file never refuses a build** (ADR-0051, decision 7).

## Consequences

- Comments, checks and replies are versioned with the design they describe,
  on the same branch and in the same commits. No migration and no new API was
  needed.
- The design agent's turn snapshot admits `specs/design/review.json` by its
  exact path, as it admits `security.json`.
- Revision numbers depend on the ledger. Every turn reports the paths it
  changed, measured against the files it started with, so a file written and
  then put back does not count. A design or Address comments run also asks the
  Room for a forced save before it ends, and reports the commit it ended at
  (ADR-0040, amendment 2026-10-11). If that save fails, the run reports no end
  commit, and its features' basis falls back to the commit it started from.
- Anyone who can write the repo can edit the file. It records a review. It is
  not an approval and it does not gate anything.
- Every write replaces the whole file, so the console reads the file again at
  the moment it writes. A file that cannot be read is never written over.

## Alternatives considered

**aep-api tables and endpoints.** This would have been a second store beside
the spec, with its own history and branch rules. The agent would have needed
new tools to read and answer comments, and every comment would have been one
more thing for the platform to keep in step with git.

**Checked as a flag.** A flag stays ticked after the file changes. The
reviewer would then read "checked" on a revision they never saw.

**Revision numbers in the file.** The agent would have to count, and a
console write could disagree with it. The ledger already knows which run
changed which file.

## Amended 2026-11-02 — the technical decisions are hashed without their tags

The console now lets a user keep an assumed technical decision, which drops
its `*assumed*` tag and changes no words
([console ADR-0006](../../apps/console/design/decisions/ADR-0006-an-assumed-technical-decision-is-kept-in-place-or-changed-by-a-comment.md)).
So the Checked hash of `specs/design/decisions.md` is taken of its text with
each closing `*assumed*` tag, and the space before it, removed. A Keep then
leaves the file checked. Every other artifact is hashed as stored.
