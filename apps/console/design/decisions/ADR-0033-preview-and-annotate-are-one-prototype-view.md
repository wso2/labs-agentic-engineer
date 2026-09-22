# ADR-0033: Preview and Annotate are one prototype view, and a click means what the mode says

- **Status:** Accepted
- **Date:** 2026-09-23 (feature
  [#813](https://github.com/wso2/labs-agentic-engineer/issues/813); review shell
  settled by prototype, variant D "Inspector")
- **Related:** repo
  [ADR-0034](../../../../docs/decisions/ADR-0034-a-web-application-is-reviewed-as-a-prototype-before-build.md)
  (the prototype model, stage order and build gate);
  [ADR-0007](./ADR-0007-design-gate-is-build-trigger.md) (Build is the approval).

## Context

A reviewer does two things with a prototype: uses it as the application it
stands for, and points at what is wrong with it. Both happen on the same
screens, in the same session, usually alternating — walk to the detail page,
notice the missing column, say so, walk on. Every click on a rendered button is
ambiguous between the two: *press it* or *this one*. The Excalidraw prototype
had no answer, so feedback went to chat as prose and the agent guessed what it
referred to.

## Decisions

1. **One page, two modes.** `/projects/:project/prototype/:component` renders
   the prototype once. **Preview** and **Annotate** are a toggle in the review
   bar, not two pages or two renderings, so switching never loses the screen,
   flow, display state or open overlay the reviewer is looking at. Screen, flow,
   state and mode ride the URL, written with `replace`, so a link opens exactly
   that view and Back leaves the prototype instead of stepping through its
   screens.

2. **The mode decides what a click means, in one place.** Every interactive
   node reports one `ACTIVATE` to a pure reducer; registry components render
   presentation only. In Preview the action runs (navigate, open or close an
   overlay, switch a tab or step, select a row). In Annotate the same click
   toggles that component's selection and nothing navigates. Selection is
   ordered, multi-select, and a second click deselects. Escape clears it, and so
   does changing screen, flow, role or display state, a refreshed model, and
   leaving Annotate — a request can never point at something no longer on
   screen. Selectable nodes carry `data-prototype-component-id` (the model's
   stable ID) and `aria-pressed`.

3. **Annotate is the only place feedback UI exists.** Preview shows the review
   bar and the application in a browser-window frame, nothing else. Annotate
   slides an inspector in from the right: the selection as chips (or **Whole
   screen** when nothing is selected), the request text, **Add request**
   (disabled while the text is blank), the queued requests as cards naming their
   screen and components, and one **Send all**. Queued components show numbered
   pins while in Annotate. The page takes over the viewport — no console chrome,
   file list or chat — and **Back to Spec** is the way out.

4. **Requests are queued locally and sent as one structured turn.** **Send all**
   posts the whole batch as a single `/prototype` turn carrying the typed
   `prototypeFeedback` (path, and per request its screen, flow, state, component
   IDs and text) — never prose composed by the console. The queue clears only
   when that turn succeeds, and the prototype is re-read exactly once then; a
   failed turn keeps the queue for a retry. Mode switching and sending are
   disabled while any agent turn runs on the project.

5. **Read-only means view state only.** No control submits, mutates, or calls
   anything; a form shows its values, an approval moves to the screen that
   follows it. The page has no history, rollback, version list or approve
   control — Build is the approval.

## Alternatives considered

**A separate annotation page or overlay copy.** Two renderings of one model
drift, and moving between them loses the reviewer's place.

**Modifier-click to annotate inside Preview.** Undiscoverable, and a reviewer
who does not know the modifier presses the button they meant to comment on.

**Comment on each click, sent immediately.** One agent turn per note, each a
whole-file rewrite, the page refreshing under the reviewer between notes.

**An always-visible feedback panel.** Tried in the shell variants; it crowds the
application the reviewer is judging and makes it read as part of the console.

## Consequences

- The reducer is the whole interaction contract and is tested without a DOM;
  browser tests cover only what needs real input (Escape, double-click toggle,
  leaving Annotate, changing screen).
- A new node kind must render through the registry's dispatch to be selectable;
  one that wired its own click handler would act in Annotate.
- Feedback lands in the project chat like any other turn, so the record of what
  was asked lives where every other request lives.
