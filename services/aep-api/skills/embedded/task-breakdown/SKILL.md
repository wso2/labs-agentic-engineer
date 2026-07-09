---
name: task-breakdown
description: Use when turning a published design into implementation tasks — planning a task batch from design.json components, ordering tasks by their dependencies, or writing the issue brief for one planned task.
metadata:
  aep:
    kind: platform
---

# Task breakdown

One published design becomes **one task per component**, emitted in
**topological order** over the design's dependency edges.

## When to use

- Planning the first task batch for a freshly published design.
- Re-planning after a spec/design diff (incremental mode).
- Writing the issue brief for a single planned task.

## Quick reference

| Dependency kind | Ordering effect | What the rationale records |
|---|---|---|
| `component` | consumer's task lists the provider's task in `dependsOn` | the build-order edge |
| `org-service` | none — provider lives in another project | the cross-project binding |
| `external` | none | a value-collection **gate** |
| `platform-resource` | none | a provisioning **gate** |

## Breaking down the design

A correct breakdown satisfies all four:

1. **One task per component** — every component in the design appears in
   exactly one task; every task targets exactly one component.
2. **Topological order** — for every `component` edge A→B, A's task lists B's
   task title in `dependsOn`. Array position is ignored; `dependsOn` carries
   the order.
3. **Gates flagged, never minted** — if a component has an `external`
   dependency, its rationale names the value-collection gate; if it has a
   `platform-resource` dependency, its rationale names the provisioning gate.
   The platform authors gate rows; you emit no task for a gate.
4. **Every dependency accounted for** — each dependency in the design appears
   in exactly one task's `dependsOn` (component kind) or rationale (org-service,
   external, platform-resource).

Split one component into more than one task only if a single PR physically
cannot land the work (e.g. a data migration must merge before feature code).
In incremental mode, emit one task per component the diff touches and skip
components with no change.

## The issue brief (detail phase)

A task's brief is self-contained: it states WHAT to build and the boundary,
never HOW. Acceptance criteria are externally checkable (e.g. "GET
/todos/{id} returns 404 for an unknown id"), not qualities ("good error
handling").

## Worked example

Design: `orders-web` (webapp) → `orders-api` (service) → `payments-api`
(service). `payments-api` also depends on external `stripe` and
platform-resource `payments-db`; `orders-api` also depends on org-service
`identity`.

Tasks, in topological order:

1. **payments-api** — "Build the payments API." `dependsOn: []`. Rationale:
   records the ledger; blocked until the `stripe` external values are
   collected and the `payments-db` resource is provisioned.
2. **orders-api** — "Build the orders API." `dependsOn: ["Build the payments
   API"]`. Rationale: places orders via the payments service; identifies users
   via the `identity` org service.
3. **orders-web** — "Build the orders web app." `dependsOn: ["Build the orders
   API"]`. Rationale: storefront UI over the orders API.

Every component appears once; every edge is an ordering; both of
payments-api's gates are flagged and none is minted as a task.

## Common mistakes

- Emitting a "collect Stripe config" or "provision the database" task — flag
  the gate in the rationale instead.
- Splitting a component by page, endpoint, or feature count — one component is
  one task.
- Relying on array position for order — only `dependsOn` orders the batch.
- A brief that prescribes file layout or libraries — state WHAT and the
  boundary; the agent chooses HOW.
