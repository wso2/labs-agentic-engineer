---
name: task-planning
description: Use when asked to plan or re-plan a project's implementation Tasks from its spec and design — one Task per design component, with dependencies, using planTask/updateTask.
metadata:
  aep:
    kind: platform
---

# Task planning

Turn the design into a set of Tasks the platform can execute. **The unit of work
is the design component.** Every component under
`specs/design/components/<name>/` that needs work gets exactly one Task; the
component is the only thing a Task may target. Never invent a component.

Load this skill, read the design (`specs/design/design.md` and each
`components/<name>/design.json`), and any existing Tasks (`tasks/<issueNumber>.md`),
then plan.

## What a Task is

- **One component, one Task.** Title it after the work, e.g.
  "Implement order-service" for fresh work or "Add refunds to order-service" for
  a change. Keep titles unique and human-readable.
- **rationale** is one sentence: why this Task exists (what the component must do,
  or what changed).
- **dependsOn** are **component names** taken from the design's relationships:
  if `order-service`'s `design.json` connects to `user-service` and `catalog`
  (or the design's Interactions section says so), its Task `dependsOn`
  `["user-service", "catalog"]`. Never list issue numbers; never depend on the
  platform's own infrastructure (databases, gateways, IDPs). A dependency must be
  a real component with its own Task or design directory.

## Fresh and incremental are the same flow

Plan against the CURRENT STATE. When the `tasks/` context is empty this is a
fresh plan; when it is non-empty it is incremental — the SAME reasoning, just
with existing Tasks in view:

- **Pending Task of an affected component** → `updateTask` it (re-state scope,
  refresh `dependsOn`, rewrite the body) rather than planning a duplicate.
- **A component whose work is already done** but the design changed → plan a
  **delta** Task for just the new work (a new, distinctly-titled Task).
- **In-flight work** → `updateTask` with a note that the change lands on top of
  what is running; do not silently rewrite its scope.
- **Untouched components** → **do nothing.** Silence is correct; do not re-plan a
  component whose design did not change.

Because the accumulator dedupes by title and component, re-planning an unchanged
design converges to no-ops by construction — you will simply have nothing to add.

## Never invent a component

If a requirement is covered by **no** design component, do not plan a Task for
it and do not stretch an unrelated component to fit. Say so in your final text
and recommend regenerating the design so a component exists. Conversely, an
**obsolete** component (still has a Task but was removed from the design) →
`updateTask` its Task with an obsolescence note and stop; a human closes it (you
have no close tool).

## Write the bodies in the same turn

After you have planned the Tasks, write each one's full body with `updateTask`
(`set.body`) in this same turn — the whole design and the sibling Tasks are still
in context. A good body has:

- **Scope** — what to build/change in this component, concretely.
- **Acceptance criteria** — how we know it is done.
- **References** — point into the spec/design (the capabilities, the
  `design.json`, the `openapi.yaml`) rather than restating them.

Reference a Task you planned this turn by its `{ title }`; reference an existing
Task by its `{ issueNumber }`.

## When a tool rejects you

Each error carries what you need to fix it in one step:

- **UNKNOWN_COMPONENT** — the component (or a `dependsOn` entry) is not a known
  component; the result lists the known ones. Pick one, or drop the dependency.
- **UNKNOWN_REF** — the ref does not resolve; the result lists the addressable
  issue numbers and this-turn titles.
- **DUPLICATE_TITLE** — the title is taken (listed); choose a distinct one, or
  `updateTask` the existing Task instead of planning a new one.
- **DEPENDENCY_CYCLE** — the `dependsOn` would form a cycle (the path is listed);
  the design's relationships are acyclic, so re-read them and break the cycle.
