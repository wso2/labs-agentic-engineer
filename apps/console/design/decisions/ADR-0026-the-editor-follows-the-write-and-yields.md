# ADR-0026 — The editor follows the write, and yields to the reader

**Status:** Accepted (2026-08-28) · **Issue:**
[#576](https://github.com/wso2/labs-agentic-engineer/issues/576) ·
**Supersedes** the cell-only burst navigation shipped with
[#575](https://github.com/wso2/labs-agentic-engineer/issues/575)

## Context

The spec editor streams artifacts live, but only `design.cell` steered the
selection: a change burst navigated to the Architecture tab **even over a
manual selection**, once per burst. Generalizing that to a multi-artifact
design turn would yank a reading user away on every file — a six-file run is
six yanks — while doing nothing keeps the user watching a finished document
while the real work streams elsewhere.

## Decision

**During a turn, the editor follows the write by default; the first manual
selection ends following for the rest of the turn.**

- When an artifact starts being written, the editor selects it — the default
  posture is watching the work land, in whatever renderer that artifact
  already has.
- A manual selection is a declaration of reading intent: from that moment the
  turn stops steering the editor entirely. The rail's pulse on the writing
  entry remains the live pointer back — rejoining is one click.
- A new turn resets to following.
- The cell's special case folds into this rule — it is simply the design
  plan's first entry, and it no longer overrides a manual selection.

The asymmetry is deliberate: a user who clicked away loses the auto-tour for
one turn and can rejoin with one click; a user being yanked while reading has
no way to opt out at all.

## Rejected

- **Yank back on every artifact** (the cell's old behavior, generalized) —
  hostile to reading; violates the spirit of "nothing ever auto-navigates"
  (#522).
- **Never follow** — the passive watcher, the common case at turn start,
  stares at a stale document.
- **A follow/unfollow toggle** — a control for a distinction one click
  already expresses.
