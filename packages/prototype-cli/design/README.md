# `@wso2/prototype-cli` — design notes

`prototype init | check | preview | export`. Exit codes: 0 ok, 1 findings or a
failed operation, 2 a usage error (including an unknown theme or an explicit
busy `--port`).

## Preview

A local server bound to 127.0.0.1: the host page, `host.js` (prebuilt by esbuild
at package build time), the theme's `frame-runtime.js`, an SSE stream (`update`
with the last good revision, `findings`) and `POST /feedback`. It answers only
requests addressed to `127.0.0.1:<port>` or `localhost:<port>` and takes
feedback only as JSON from its own origin; a declared or streamed body over
1 MiB gets 413. The host page is sent with `frame-ancestors 'none'` and
`X-Frame-Options: DENY`. If a runtime file is missing the server answers 500
rather than crashing.

The watcher rechecks once the files are quiet for 120 ms and keeps the last good
revision, so a half-written file shows findings over the previous render. It
survives unreadable files: it shows a finding and keeps the last good render.
The kit's check runs off the event loop, so the server answers while a revision
renders; a newer change supersedes a check still in flight.

## Host page

Frames the app in the kit's `PrototypeWindow`; uses the kit's `/host` reducers
for all view state (`reduceReview`: the view plus the open comment bubble) and
its `/feedback` for the request shape, limits, the comment queue
(`FeedbackQueue`: `enqueue`, `editRequest`, `dequeue`, drafts,
`submissionOf`, `earlierComments`, `targetLabel`) and the revision hash. The
review's behaviour is the kit's, shared with the console: the comment draft
(`useCommentDraft`), the keys (`useReviewKeys`, bubble phase), the bubble's
placement (`placeBubble`) and focus on a click away (`focusLeftBehind`). `--persist` keeps snapshots in
`localStorage` under `proto:data:<revision hash>`; a new revision starts from
the seed.

The layout is the console's review's, in the host's own styles: a header of
only the prototype's name, the window, and below it one floating dock
(`Dock`) with every control, in groups split by dividers: View
(`ViewControls`: Role and State, compact selects with their name inline, and
Reset data as an icon button), then, in preview only, Mode (`ModeTools`) and
Comments (`CommentQueue`). An export's dock has the View group alone. There
is no screen or flow picker: the reviewer moves through the prototype by
using it, and a comment's list entry goes to its screen, role and state. The
dock sits below the window in the layout (`.ph-body` is a column), so it
never covers the prototype's last rows; what it opens grows upward over the
prototype. Below 1100px it drops the Comment mode hint, below 1000px the
selects' inline names (the value stays). Bubbles keep above the stage's
bottom (`BubbleBounds`, the window's area), so they never cover the dock.

Comment mode (the view's `annotate`; preview only, never in an export)
comments in place, as the console's review does, in the host's own styles:

- The dock's Preview · Comment tool pair (`ModeTools`): labelled, with
  icons, `V` and `C` in their tooltips; the active tool is tinted orange in
  Comment and neutral in Preview. While in Comment mode the window has an
  orange ring, its bar a "Comment mode · Esc" tag, and the dock the
  hint "Click anything to comment"; the frame draws the kit's comment cursor
  (solid "+" bubble over an element, hollow over empty space).
- A click on an element opens a comment bubble at it (`AnchoredBubble`, by
  the kit's `useFrameAnchors` and `placeBubble`: below the element, or above
  it when there is no room, and kept there as the prototype scrolls or the
  window resizes). A click away closes it; focus stays where the click put
  it, or goes back to the element when it put it nowhere. Shift-click adds or removes elements, keeping the text. Add or
  Cmd/Ctrl+Enter queues the comment and leaves a numbered pin. The text is
  held to the comment limit, with a counter near it.
- A click on empty space (where the cursor shows the hollow bubble) comments
  on the whole screen: the bubble opens at the spot, with a hollow pin there
  while it is written; added, the comment leaves a numbered pin at the spot,
  which scrolls with the prototype's page. With a bubble open, that click
  only closes it (as a click away does). The spot is the host's own: the
  feedback file never has it (`elementIds: []`, as before).
- A pin (on an element, or a whole-screen comment's at its spot) opens its
  comment, in either mode, to read, Edit (Save or Cmd/Ctrl+Enter) or Remove.
- Text is never lost: a bubble closed with text in it (click away, Escape,
  another plain click, leaving the screen) keeps it as a draft where it was
  written, shown as a hollow pin; reopening the same elements, or the draft
  pin, restores it. A whole-screen comment's text is kept the same way, as
  the screen's draft, its hollow pin at its spot; the pin, the next click on
  empty space or Comment on this screen reopens it. Drafts are not counted
  or saved.
  An empty bubble just closes.
- The dock's comments (`CommentQueue`) replace the old side panel: the
  count, which opens upward a list of every comment across screens, roles
  and states, its elements named by their labels (an entry goes there
  and opens the comment) and, below it, Comment on this screen (the
  keyboard's whole-screen comment, its bubble at the dock), and Save
  feedback, which writes
  `.prototype/feedback.json` as before (`POST /feedback`; the drafts are
  never in it). Above the dock it says when the queue is full (50; saving
  does not empty it, so the way on is removing one), how many comments were
  written on an earlier revision (each marked in the list) and the save's
  outcome. Its empty list says how to start ("Press C or choose
  Comment, then click anything on the screen: an element, or empty space
  for the whole screen").
- Keys on the host page: V returns to Preview and C toggles Comment mode
  (neither while typing); Escape closes
  the bubble, then clears the selection (the frame reports an Escape it did
  not use itself). There is no review to close here.

The queue lives in the page's memory: it survives revisions and is saved with
the hash it was started against (`prototypeHash`): a revision here is the
author's edit, not one the reviewer asked for and waited on, so unlike the
console the queue is not moved onto it (no `onRevision`). There is no agent
turn, so no revision lifecycle.

## Export

One HTML file: the host bundle, the frame runtime and the revision inlined as a
JSON config, escaped against `</script>` breakout (tested). Its CSP allows
inline script with `'unsafe-eval'` because the `srcdoc` frame inherits it;
`connect-src 'none'`. No Comment mode, no persistence.

## Tests

Seam 1: the built bin (`test/*.test.ts`, node) and the browser lane
(`vitest.browser.config.ts`, `pnpm --filter @wso2/prototype-cli test:browser`):
tests run in the browser and drive Playwright pages through node-side commands
(`test/browser/commands.ts`: clicks with modifiers, page-level keys, element
boxes in the page's viewport, viewport resizes, the cursor the frame draws at
a point), so `annotate.browser.test.ts` asserts the bubble against the real
frame's layout and the comment cursor per mode and target. Seam 2: `test/consumer.test.ts` installs the packed
tarballs with npm in a temp directory.
