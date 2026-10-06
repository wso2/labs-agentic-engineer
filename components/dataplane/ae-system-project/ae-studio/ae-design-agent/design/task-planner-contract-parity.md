# Task planner: the Plan turn's contract with aep-api

The task planner is not a route of its own: a Plan is a design-agent turn
(`kind: plan`) that `aep-api` starts through `ae-studio-tools` on the Turn
socket. The turn's lifecycle and the lock it shares with chat are in
[turn-runtime.md](turn-runtime.md); this note is what the planner and
`aep-api` each own.

## Wire surface

| Piece | Where |
|---|---|
| Turn socket contract and golden streams | `packages/contracts/sockets/ae-studio/turn/` |
| Plan tools and their inputs | `src/agents/main/tools/task-plan.ts` (`planTask`, `updateTask`), schemas in `@aep/agent-stream` (`task-tools-schema.ts`) |
| Per-turn accumulator, the self-correctable errors | `src/agents/main/task-plan-accumulator.ts` |
| The stream's `task-op` lines | `src/edge/turn-socket.ts` (`taskOpOf`) |
| The consumer that writes the issues | `services/aep-api/internal/delivery/task/plan_tap.go` |

The planner's tools validate and accumulate; they never write. Each ok
`planTask` / `updateTask` result becomes a `task-op` line, and `aep-api`'s
plan tap mints and edits the Task issues from those lines. A failed call
(`UNKNOWN_COMPONENT`, `UNKNOWN_REF`, `DUPLICATE_TITLE`, `DEPENDENCY_CYCLE`) returns to the model as a tool result, which corrects
itself in the same turn.

## What the planner owns and what the platform owns

`dependsOn` on a planned Task lists design **component names**, never issue
numbers. `aep-api` resolves each to an issue number when it renders the
Task body, because the issue a dependency will get may not exist yet when
the dependent is planned; an unresolved name is still written by component.

The planner has no say over:

- the build's gate tasks and the validation task, which the platform mints
  elsewhere, so the planner never emits config-collection or resource
  provisioning tasks;
- the Task's labels and milestone, set by the plan tap;
- the stories stamp, derived from the design's citations.

So the planner expresses dependency awareness only as build order (a
consumer's Task lists its providers in `dependsOn`) and as the rationale text.

## Who may start a Plan

Only `aep-api`, through `ae-studio-tools` `/internal/v1` (AE-only M2M), which
relays the request onto the Turn socket; on the socket the mount is the gate and
no request carries a token ([ADR-0041](../../../../../../docs/decisions/ADR-0041-ae-studio-checks-platform-idp-tokens-itself.md)).
The model is the org's connection from the pod env, like every other turn.
