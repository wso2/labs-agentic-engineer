# ADR-0001 — Prototype review is a full-screen overlay; Annotate sends typed feedback to the chat

**Status:** Accepted · 2026-10-03
**Related:** [repo ADR-0042](../../../../docs/decisions/ADR-0042-a-prototype-is-react-on-a-fixed-kit-in-a-sandbox.md)
(the kit, the frame, the Oxygen theme). Spec: #860.

## Context

The agent writes a clickable prototype per designed web application. A person
has to try it and say what to change. A prototype is an app, not a card: it
needs the whole window to read as one, and the chat is where the agent
listens, so feedback has to reach the same conversation as everything else.

## Decision

1. **Full-screen overlay.** Review is an Oxygen `Dialog fullScreen` over the
   console (`PrototypeReview`), opened by the route
   `/projects/$p/prototype?review=<component>` and closed with X or Escape. The
   Prototype tab (`PrototypeWorkspace`) lists the designed web apps; Make
   prototype also sits in the Design actions.
2. **The kit's frame and reducer, not a copy.** The overlay renders the kit's
   `PrototypeFrame` driven by `reducePrototypeView`, as the CLI host does:
   Preview acts, Annotate only selects. Only the host chrome (toolbar,
   feedback panel) is the console's, in Oxygen UI. The frame's theme is
   `@wso2/prototype-theme-oxygen`, loaded as a raw asset (`frame-runtime.js?raw`,
   about 2 MB) the first time a review opens, so the console bundle stays small.
3. **Annotate sends typed feedback to the chat.** Requests are queued per
   component (screen, flow, role, state, element ids, text) and sent together
   by Send all as `/prototype <component>` with `prototypeFeedback` on the turn
   body, never as free text. The server validates it (400 before a turn opens)
   and the agent answers each request by number, applied or declined. A queue
   is kept when a send fails or the chat is busy, and across closing the
   overlay. It records the hash of the revision its first request was made on,
   and the panel says when the prototype has since changed.
4. **The chat stays the one place for agent work.** A prototype turn is a
   normal chat turn; the console reads the user's row as its requests (the
   batch is journaled with the turn), offers "Open prototype" after a turn
   that wrote a valid prototype (derived from the history and the room), and
   shows a dot on the Prototype tab until this browser has seen the current
   revision (a per-browser record of hashes, never shared).

## Consequences

- The kit's behaviour (what Preview and Annotate do) is changed once, in the
  kit; the console follows by importing it.
- The overlay hides the console, so the chat is opened before a send and the
  overlay closes on success.
- The frame runtime is the largest lazy chunk in the console image.
- The chat summary and the Open prototype note survive a reload and reach
  teammates, since both come from the persisted turn. The summary names
  elements by id: the labels the frame shows are not part of the batch.
- The "reviewed" dot is per browser: a browser that never looked at an
  existing prototype shows the dot.
