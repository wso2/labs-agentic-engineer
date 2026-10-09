# Prototype review

A clickable prototype of each designed web application, tried and commented on
in the browser. The agent makes it (`/prototype`), the person reviews it, and
the comments go back to the chat as a typed batch. Why it is shaped this
way: [ADR-0001](decisions/ADR-0001-prototype-review-is-a-full-screen-overlay.md)
(the overlay), [ADR-0002](decisions/ADR-0002-the-revision-lands-in-the-open-review.md)
(the revision lands in the open review),
[ADR-0003](decisions/ADR-0003-the-comment-bubble-is-drawn-by-the-host.md)
(the bubble is the host's, over the frame).
Code: `features/prototype/`. The spec is wso2/labs-agentic-engineer#885
(tickets #886–#890, #892, #893); there is no PRD entry for it in this repo.

## Where it shows

- **Prototype tab** (`PrototypeWorkspace`, route `projects/$p/prototype`): one
  centered entry per web application (a contract artifact whose `design.json`
  type is `web-application`) with status None, Ready, Invalid (the reason in a
  tooltip) or Revising, and one action: Review once a prototype renders, else
  Make prototype (Try again when invalid). No action while the first one is
  being made. Revising an existing prototype goes through commenting in review;
  remaking it from a changed design is the Design tab's Make prototype. The tab
  shows a dot while a prototype is unreviewed.
- **Design actions:** Make prototype (`MakePrototypeButton`), hidden until the
  design has a web application. One app sends `/prototype <c>`, several send a
  bare `/prototype`. Disabled unless the chat can send.
- **Chat:** an "Open prototype" note after a `/prototype` exchange that wrote
  a prototype which is valid now (one component opens its review, several
  open the tab). It is derived (`model/note.ts`) from the log, which the
  history carries, and the room, so a reload and teammates see it too.

## Files

A prototype is `specs/design/components/<c>/prototype.{json,tsx}` in the room,
read with `useRoomFiles`. Status comes from the kit's `parseManifestJson`
(`model/prototypes.ts`). The agent's writes are gated by the kit's rules and the
render check; Go re-checks on save (see ADR-0042).

## Review

`PrototypeReview`: a header of only the title and Close (X, Esc), the kit
`PrototypeWindow` (browser chrome, read-only `prototype://<screen>` address
bar; styled by the console with `--proto-window-*` Oxygen variables) around
the `PrototypeFrame` at full width, and below it one floating dock
(`ReviewDock`) holding every control, in three groups split by dividers:

- **View** (`ViewControls`): Role and State, two compact native selects (the
  name inline before the value), and Reset data (an icon button, its name in
  the tooltip).
- **Mode** (`ModeTools`): the Preview · Comment tool pair (labelled, with
  icons, `V` and `C` in tooltips; the active tool tinted primary in Comment,
  neutral in Preview) and, in Comment mode, "Click anything to comment".
- **Comments** (`CommentQueue`): `N comments` (its list holds Comment on
  this screen) and Send to agent, which shows the revising status itself.

There is no screen or flow picker: the reviewer moves through the prototype
by using it (Preview), and an entry in the comment list goes to its comment's
screen, role and state. The view starts on the manifest's entry screen; the
kit reducer's `NAVIGATE` and `SET_FLOW` stay for the frame and the kit CLI.

The dock sits below the window in the layout (a column: window, then dock),
so its space is reserved and it never covers the prototype's last rows; what
it opens grows upward over the prototype. Below 1100px it drops the Comment
mode hint (the window's tag still says it); below 1000px the selects drop
their inline names and keep the value. Bubbles stay inside the stage (the
window's area, `BubbleBounds`: Popper's flip and overflow boundary), so a
bubble at the window's foot never covers the dock; a whole-screen comment's
bubble opens at its spot, or (from the list) above the dock.

The view and the open comment bubble are one pure state, the kit's
(`reduceReview` in `@wso2/prototype-kit/host`: the view reducer plus
`CommentBubble` = on the selection, on the whole screen (at a spot, or from
the list), or a queued comment opened from the dock's list or a pin). Comment is the user's word for the view's
`annotate` mode. `V` returns to Preview, `C` toggles Comment (neither while
typing); Escape closes the bubble,
then clears the selection, then closes (the kit's `useReviewKeys`, captured so
a used Escape never reaches the dialog). With focus in the prototype, only
the frame's `proto:escape` counts (sent when the prototype left the key
unused); the review ignores an Escape whose target is the frame.

Comment mode shows itself three ways, only while it is on: a primary ring
round the prototype window (its `--proto-window-border`/`-shadow`), a
"Comment mode · Esc" tag in the window bar (`PrototypeWindow`'s `tag`), and
the dock's hint "Click anything to comment". Inside the frame the kit
draws the comment cursor (an arrow with a bubble: solid with a "+" over an
element that takes a comment, hollow over empty space), so it follows the
pointer natively with no bridge message. A click where it is hollow is a
whole-screen comment there (`proto:screen-click`).

- **Bubble** (`CommentBubble`, `QueuedCommentBubble` in `AnchoredBubble`): an
  click in Comment mode opens it at the element (Shift-click adds elements), drawn by
  the console over the frame from the boxes the frame reports
  (`useFrameAnchors`), never inside it (ADR-0003). Add or Cmd/Ctrl+Enter
  queues the comment. A click away closes it; focus stays where the click
  put it, and when it put it nowhere (the dialog's blank parts), it goes
  back to the element (the kit's `focusLeftBehind`). A click on empty space
  in the prototype opens a whole-screen comment at that spot (the kit's
  `SCREEN_CLICK`; `useFrameAnchors().point` places the bubble there and
  keeps it there as the prototype scrolls), with a hollow pin at the spot
  while it is written; with a bubble open, it only closes it.
- **Comments** (`CommentQueue`, the dock's group): `N comments`, `Send to
  agent`, and a list, opening upward over the prototype, of every queued
  comment across screens, roles and states, ending in `Comment on this
  screen` (the keyboard's whole-screen comment, its bubble at the dock; the
  pointer's is a click on empty space), its elements named
  by their labels (the kit's `targetLabel`, from each visited screen's
  labels as the frame reported them; ids for a screen not seen since a
  reload). An entry goes to its screen, role and state, opens the
  comment and closes the list. While a revision runs, Send to agent
  becomes a disabled `Revising…` button with a spinner, and a polite live
  region reads out `Agent is revising… (N comments)` (the button is the one
  place that shows it; there is no separate label or header chip); it marks comments written on an earlier version and flags those
  whose element is gone. Callouts above the dock say why a revision failed
  (with Retry), why a send was refused, that the queue is full, and how many
  comments were written on an earlier version or lost their element. Its empty
  list says how to start: "Press C or choose Comment, then click anything on
  the screen: an element, or empty space for the whole screen".

- **Preview** acts: navigation, forms, mock data. **Comment** only selects;
  selected elements are pinned and a comment is typed against them (max 4000
  characters, 50 comments per batch, the kit CLI's limits; the contract calls
  a comment a request). A batch out with the agent keeps its places until its
  turn ends (`queueFull`): comments written meanwhile fill only the rest, so
  a failed batch given back with them is still one batch Retry can send.
- **Pins:** a queued comment's pin (the frame's, in both modes, keyboard
  reachable) opens its bubble (`OPEN_PIN`) with Edit (in place) and Remove
  (the rest renumber). A whole-screen comment made at a spot has its pin
  there, drawn by the frame at the spot of its document so it scrolls with
  the page (`FrameView.screenPins`); one made from the list has none. Closing
  a bubble with Escape, Add or Remove puts focus back on its element or pin
  (`PrototypeFrame.focusElement`, `focusScreenPin`). Each screen's pins are
  drawn whenever it shows, so comments on several screens, roles and states
  all go in the one batch Send to agent sends.
- **Drafts** (the kit's `useCommentDraft`, the same in the kit CLI's
  preview): the text of a bubble on elements is never lost. Escape, a click
  away, a plain click on other elements or closing the review keeps it as a
  draft, shown as a hollow pin; selecting the same elements or clicking the
  draft pin reopens it. A draft pin clicked in Preview switches to Comment
  (`SELECT_ELEMENTS`) and reopens it there. Drafts are not counted or sent,
  and survive Send. A whole-screen comment's text is kept the same way, as
  the screen's draft, its hollow pin at its spot (none without one); the
  pin, the next click on empty space or Comment on this screen reopens it.
  A comment's spot is client-only queue data: the batch carries
  `elementIds: []` and no spot, as before. A sent batch a revision gives back
  comes back without spots (no pins; listed as Whole screen).
- **Queue** (the kit's `FeedbackQueue`; `model/feedback.ts` makes the batch):
  per component, with its drafts, kept across closing the review and leaving
  the tab. Each comment keeps the revision it was written on; the queue's
  hash, which the batch names, is its first comment's revision, moved on to
  each revision that lands while it waits (`onRevision`). Comments, limits,
  pins and the hash are the kit's (`@wso2/prototype-kit/feedback`); the hash
  is plain JavaScript, so it works over plain HTTP.
- **Send to agent:** refused with the reason while the chat is not idle (queue
  kept). Otherwise `chatStore.send("/prototype <c>", {kind: "prototype",
  feedback})`; on success the chat opens behind the review, the queue clears
  (drafts kept), the review stays open and the design data is read again. The batch is journaled with the turn and comes back in
  the history (`ConversationMessage.prototypeFeedback`); the chat row shows
  `feedbackSummary` of it (`model/summary.ts`: screen, role and state named
  from the manifest, element ids, text), not the wire text.
- **Revision** (ADR-0002; `model/revision.ts`, kept per project and
  component in the review store, `model/reviewStore.ts`, which
  `usePrototypeReviews` reads): the store lives for the page, like the chat
  store, so the review and the batch out with the agent outlive the overlay
  and the Prototype tab; it records a turn's end even with no review open,
  and the next time the tab follows the prototype a failed batch comes back.
  The sent batch is held until its turn ends. While the prototype is revising
  the review shows the last ready revision, not the files the agent is still
  writing. Revising → ready swaps the new revision in through
  `MANIFEST_REPLACED` (same screen, role and state, else the role's entry
  screen), the frame reloads from the seed data, and an `Updated · N comments
  addressed` toast offers What changed (closes the review, opens the chat at
  its latest turn, the one that revised the prototype). Comments held while
  it ran move on to the new revision (the next batch names it), each marked
  "Written on an earlier version". Those are checked against the elements
  the frame says it draws for the revision and view now showing (the kit's
  `orphansOnScreen`, same screen, role and state only; a report from before
  the swap is not used, as the frame echoes the version it drew and
  `PrototypeFrame` drops a report of an earlier one; nor is one of another
  screen), so a comment is flagged "Element no
  longer on this screen" when its screen is visited, with Keep as screen
  comment (`keepOnScreen`) or Remove. A failed
  turn (`chatStore.onTurnEnd`) or an invalid revision keeps the last ready
  revision showing and puts the batch back in front of the queue
  (`restored`), with the reason (the reviewer's situation: the prototype
  wasn't updated, the previous version still shows) and Retry. A turn that
  failed after writing valid files leaves them showing: the batch comes back
  onto them (the queue's hash moves on, each comment marked "Written on an
  earlier version"), and the notice says the agent stopped partway and what
  shows is what it changed before it stopped. A stream lost
  mid-turn reports no outcome; the status (ready or invalid) decides.
  Reopened mid-revision, the review shows the same state.
- **Reviewed dot:** shown while a valid prototype's current revision is
  unreviewed in this browser (not while a turn is revising it).
  `model/reviewed.ts` stores `{component: hash}` in localStorage
  (`aep:prototype-reviewed:<project>`); every access is guarded.

## Wire

`turnBody` puts `prototypeFeedback` (`PrototypeFeedbackInput`, from the
generated API types) on the JSON body with `collab: true`; the instruction is
the bare `/prototype` or `/prototype <c>` with the same component. The mock
(`mocks/fixtures/prototype.ts`) writes the manifest then the source, answers
feedback by number, and refuses a bad batch as Go does. Its revisions: "on
the right" and "bigger total" apply, "remove" takes the Reject button away
(orphaning comments on it), "fail" fails the turn (`turn-failed`, status
failed) writing nothing, and "partway" fails it after writing what the
requests before it ask (send "Remove the Reject button" then "Stop partway"
to see a failed turn's edits showing).

## Theme

`@wso2/prototype-theme-oxygen`, drawn with the console's own theme
(`@aep/ui-theme`, the `aepTheme` `main.tsx` applies), so the prototype and the
console cannot drift. Until the frame's app first draws, the review passes
`PrototypeFrame` a loading state ("Starting the prototype…") that covers the
frame, so a click while the runtime starts is not lost; a frame that never
draws shows the kit's "didn't start" error instead. The review passes the
console's resolved scheme (`useColorScheme`: the mode, or the system's under
System), so the prototype is light or dark with the console. Its frame has no
storage (opaque origin), so a script that touches `localStorage` in every
frame (for example a Playwright init script) raises there; limit it to the top
frame.
