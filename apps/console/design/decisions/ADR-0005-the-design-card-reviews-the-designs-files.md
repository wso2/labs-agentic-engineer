# ADR-0005 — The Design card reviews the design's files like a pull request

**Status:** Accepted · 2026-10-11 · Amended 2026-11-02 by
[ADR-0006](ADR-0006-an-assumed-technical-decision-is-kept-in-place-or-changed-by-a-comment.md)
(the technical decisions' Checked hash leaves out their `*assumed*` tags)
**Supersedes:** parts of [ADR-0001](ADR-0001-prototype-review-is-a-full-screen-overlay.md)
(the Prototype tab and route, Make prototype, Send all with
`prototypeFeedback`, the tab's reviewed dot) and of
[ADR-0002](ADR-0002-the-revision-lands-in-the-open-review.md) (Send, the held
batch, Retry), as marked in each. **Amends:**
[ADR-0002](ADR-0002-questions-are-answered-on-a-questions-card.md) (a design
run's questions are answered on the Design card).
**Builds on:** [root ADR-0051](../../../../docs/decisions/ADR-0051-design-is-a-review-loop-and-build-reads-the-prototype.md)
(the loop) and
[root ADR-0052](../../../../docs/decisions/ADR-0052-a-designs-review-state-is-a-file-in-the-spec-repo.md)
(`specs/design/review.json`). **Detail:** [design-card.md](../design-card.md).

## Context

Design now writes the whole design in one run, the prototype included, and
the user's comments go back to the agent in one Address comments run. The
Design card was a catalog of views with a Business/Technical filter, a
separate Prototype tab with its own Send, and comments pinned to spots that
existed only in mock mode. That card had no per-file state to review against,
and it offered three different ways to send work to the agent.

## Decision

1. **The rail is the generated files.** It shows one item per file, in the
   order Design makes them, each component's files under it. An item appears
   only once its file exists, and each file keeps its existing renderer. The
   card has no filter, no Wireframes item, no Prototype tab and no Make
   prototype. The prototype is an item, and its full-screen review opens from
   that item.
2. **A file is reviewed from its title line,** as in a pull request:
   - **Checked** works like GitHub's Viewed. It stores the file's hash, and a
     new revision unticks it ("Revision 2 · Changed since you checked it").
   - **💬 N** opens that file's threads and a text box.

   A comment is made only there, or in the prototype's own review, whose
   queued comments are Pending drafts on the prototype. Comments pinned to a
   spot inside a file are retired. The acceptance tests take comments but
   have no Checked.
3. **Comments are sent only from the Overview.** Address comments (N) marks
   every waiting comment sent in one write and then starts `/address-comments`.
   If the turn does not start, it puts the comments back. The prototype
   review's Send to agent is gone.
4. **One primary at a time.** The first rule that matches wins:
   1. Answer N questions;
   2. Address comments, while a comment is waiting;
   3. Design, while the spec is ahead of the design;
   4. Build, once every checkable file is checked;
   5. otherwise, Open on the first unchecked file.

   The card's header keeps only Build.
5. **The design's questions are answered on the card.** Answering a
   feature's open question writes the answer into the feature file's
   Decisions, which makes the feature's design out of date. Answering the
   agent's question on a comment reopens that comment. The chat only reports:
   a run's closing names the next step on the card, and when questions are
   left and the card is not open, the chat shows Answer N questions, which
   opens the card.
6. **The Spec card leads to the interview first.** While features are still
   stubs, Interview <next stub> is the Spec card's primary and Design is
   secondary. Once every feature is interviewed, Design is primary while
   anything waits to be designed.

## Rejected

- **A Business/Technical filter.** The Overview splits the review into
  business and technical lists. The rail keeps one order, the order the files
  are made in, because each file is built on the ones before it.
- **Comments pinned inside a file.** A spot in a generated document has no
  identity that survives the next regeneration. Only the prototype's elements
  have stable ids.
- **Sending from each file, or from the prototype review.** One batch makes
  one revision: the agent starts at the earliest file a comment touches, and a
  prototype change lands with the contract change it needs.
- **An approve button.** Build is the only decision. Checked records that
  someone looked; it gates nothing.
- **Answering in the chat or on the Questions card.** An answer has to land in
  the file it belongs to: the feature's Decisions, or the comment.

## Consequences

- The Overview, the rail and the 💬 counts all read one file from the Room.
  The spec's Next up, the build picker and the project track count waiting
  comments from the same file.
- A teammate's comments and checks show live. A Checked tick is shared, not
  per user, so one reviewer's tick counts for everyone.
- The retired pieces leave the console: `CommentSurface`, `CommentsPanel`,
  the mock comment endpoints, the wireframes viewer and its Excalidraw
  dependencies, `FlowView` and `DataModelView`, and the Prototype route (the
  chat's Open prototype now links to the prototype's item).
