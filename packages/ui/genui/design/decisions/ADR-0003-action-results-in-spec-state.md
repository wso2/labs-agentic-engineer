# ADR-0003: Action results are written to spec state by the view

## Context

Generated UIs now include forms (e.g. create a customer). A form needs three
things a static display does not: user input reaching an action, validation
before and after the request, and the result shown back in the UI. json-render
offers `$bindState` for input, and per-binding `onSuccess` / `onError` hooks,
but those hooks only set state the model chose to write, and only when the
model remembers to write them.

## Decision

- **The request stays in the host.** A spec names a catalog action and passes
  state as params; it never contains a URL. The host's handler makes the call.
- **The view writes every catalog action's lifecycle to state** at
  `/actions/<action>` (`GenUiActionState`: `status`, `message`,
  `fieldErrors`), deterministically, whatever the spec says. Specs only read
  it, through `$state` and `visible`.
- **Validation messages live in the action's Zod schema** and are mapped to
  fields by the first issue per param. Server-side refusals come back through
  `GenUiActionError`; any other error shows a generic message, so internal
  details never reach the screen.
- **A failed action rejects inside json-render** so a spec's own `onSuccess`
  never runs on failure. Implementations get a wrapped `emit` that swallows
  that rejection, since the outcome is already in state and reported to the
  host.
- **Inputs write through `setProp`** on `GenUiRenderProps`, so a design system
  still never imports the renderer library.
- **One store per spec.** json-render's `StateProvider` keeps the store it
  mounted with, so the renderer is remounted (keyed) when a new spec brings a
  new store.

## Consequences

- A model gets feedback right by binding to a fixed path, which the system
  prompt spells out, instead of composing `onSuccess` / `onError` chains.
- Adding an action that takes form input is a schema with user-facing
  messages plus a host handler; nothing in the adapter changes.
- A new spec starts with fresh state, including typed values.
