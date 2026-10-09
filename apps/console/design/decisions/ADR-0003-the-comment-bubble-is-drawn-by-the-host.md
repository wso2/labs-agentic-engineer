# ADR-0003 — The comment bubble is drawn by the host over the sandboxed frame, never inside it

**Status:** Accepted · 2026-10-06
**Related:** [ADR-0001](ADR-0001-prototype-review-is-a-full-screen-overlay.md),
[repo ADR-0042](../../../../docs/decisions/ADR-0042-a-prototype-is-react-on-a-fixed-kit-in-a-sandbox.md).
Spec: #885.

## Context

Commenting in place needs a bubble at the clicked element. The prototype runs
untrusted, agent-written code in a sandboxed opaque-origin frame: the host
cannot measure its DOM, and anything drawn inside it is the prototype's to
read or alter.

## Decision

The frame reports element boxes (on toggle, on pin clicks, and as geometry
after scroll, resize and re-render); the kit's headless `useFrameAnchors`
maps them into the host's viewport; the host draws its own popover there
(`AnchoredBubble`, an Oxygen `Popper` on a virtual anchor). Pins stay in the
frame, since they belong to the element's layout; the bubble, its text and
the send bar are the host's.

Rejected: drawing the bubble inside the frame.

## Consequences

- Typed text never enters the untrusted frame.
- The kit stays theme-agnostic: each host draws the bubble in its own UI
  (Oxygen in the console, the CLI host's styles there).
- The bubble only follows the element as fast as the frame re-reports
  geometry; an element the frame does not report draws no bubble.
