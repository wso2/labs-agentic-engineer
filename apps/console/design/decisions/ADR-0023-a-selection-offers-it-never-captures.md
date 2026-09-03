# ADR-0023: A selection offers; only an explicit act opens an input

- **Status:** Accepted
- **Date:** 2026-08-30 (wayfinder map
  [#650](https://github.com/wso2/labs-agentic-engineer/issues/650), settled by
  the prototype on
  [#651](https://github.com/wso2/labs-agentic-engineer/issues/651); first drawn
  on by [#666](https://github.com/wso2/labs-agentic-engineer/issues/666))
- **Context:** aiming the agent at part of a spec needs a selection, and the
  spec is not a page — it is a live Tiptap editor on a `Y.XmlFragment` that the
  user types into and an agent streams into at the same time. The first
  prototype rendered the document as static HTML and let a selection open the
  "ask the agent" box, which took focus so the next keystroke was the
  instruction. On static HTML that reads fine. On the real surface it is
  unusable: in an editor, drag-select is overwhelmingly how you **retype,
  delete or copy**, and every one of those becomes impossible the moment a box
  claims the keyboard.

  Two rounds of fixes went into the symptoms — keeping the native selection
  alive so a copy still worked, then a type-to-focus shim that forwarded the
  first keystroke into the box (and dropped IME and dead-key input doing it).
  Neither touched the cause, which was that the interface had decided what the
  user meant by selecting.

## Decision

- **A selection may only OFFER.** It paints highlighting and may show a small
  affordance next to itself. It never opens an input, never takes focus, never
  moves the caret, and never clears the range.
- **An input opens only on an explicit act** — clicking the affordance, a
  keyboard shortcut, or a control the user pressed. That act is also the moment
  focus moves, and not before.
- **Dismissing an offer restores nothing and destroys nothing.** In particular
  it does not call `removeAllRanges()`: dropping the caret because a suggestion
  was waved away is the editor losing the user's place.
- **A control rendered inside the document is `contentEditable="false"` and
  preventDefaults `mousedown`**, so the browser cannot move the caret into it
  before the click lands.
- **A rebuild of in-document decorations skips the block holding the caret**, so
  an agent streaming elsewhere cannot yank the caret out from under someone
  mid-sentence.

## Consequences

- The ordinary editing gestures keep working with no special handling: select
  and retype replaces text, ⌘C copies, Delete deletes. None of them need to know
  the aiming feature exists.
- Both selections stay legible at once — the exact character range the user
  dragged, and the softer wash over the blocks that would actually be sent. A
  user copying a phrase still sees the phrase; a user aiming still sees the
  blocks.
- The type-to-focus shim and its IME caveat are not needed and must not return.
  Their existence was the signal that the rule was being broken somewhere
  upstream.
- **The rule is checkable in review:** any code path that focuses an input, or
  mutates the selection, as a *consequence of a selection changing* is a defect.
  Focus follows a click or a key the user pressed, never a range.
- It binds every surface that joins this model, not just markdown. The six
  structured views queued on the wayfinder map inherit it: a clicked node offers
  the same affordance and opens the same box on the same explicit act, which is
  what lets one selection layer serve all of them
  ([#664](https://github.com/wso2/labs-agentic-engineer/issues/664)).
- It costs one interaction step. Selecting and asking is now select → click →
  type rather than select → type. That is the price of the document remaining an
  editor, and it is the right trade: aiming is occasional, editing is constant.
