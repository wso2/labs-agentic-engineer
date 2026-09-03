# Design notes — `@aep/ui-cell-diagram-react`

This package is vendored from `@kanushka/cell-diagram-react` and kept close to
upstream so it can be re-synced (see the `description` in `package.json` for the
tag it currently tracks). What follows is what we have added on top. Read it
before pulling a new upstream tag: these are the parts a sync will not carry.

## Local divergences

### The boundary corridor pass (`src/renderer/boundaryCorridor.ts`)

Dagre lays out the internal graph and never sees the cell boundary. A component
that also talks to a gateway therefore gets its boundary edge routed straight
through whatever dagre ranked downstream of it, and the edge disappears under
those nodes. The shape that hits it is the ordinary one:

```text
a -> b
b -> c
a -> east d
```

All three components land on one rank line and the east edge from `a` runs
under `b` and `c`.

`clearBoundaryCorridors` runs on dagre's output, inside `layoutCell`, and holds
one rule: no component may sit in another component's perpendicular band on the
side that component's boundary edge leaves by. Blockers move on the
perpendicular axis only, nearest first, alternating sides so the cell stays
balanced around the edge. Dagre's ranking survives.

The pass gives up rather than making things worse. A nudge that would collide
with another node is retried on the opposite side and then abandoned, so dense
graphs keep the layout dagre chose and no two nodes ever overlap. The cost is
that in a crowded cell an edge can still end up hidden. We took that over a pass
that shuffles a layout dagre already solved.

Corridors are cleared in sweeps rather than one pass, because clearing one can
push a node back into another. A move is taken only if it collides with nothing
and blocks no corridor the node was not already standing in, and the sweep
repeats until nothing moves. The round cap stops two corridors that can only be
satisfied in turn from trading the same node back and forth.

### Known limits

Only boundary edges get a corridor. An internal edge that skips a rank can still
run under a component, and so can the gateway-to-external segment outside the
wall.

The band is anchored on the source component, not on the line from the source to
its gateway. The gateway is pinned to the middle of its wall, so when the source
sits well off the cell's center the real edge is a diagonal and the band is a
rough stand-in. Anchoring on the gateway would be circular: the gateway's
position comes from the cell size, which comes from where the components end up,
which is what this pass is deciding.

The alternative we did not take was feeding the gateways into the dagre graph as
real nodes with the boundary edges as real edges, so dagre solves it directly.
Gateways are pinned to the walls by geometry (`gatewayPosition`), so dagre would
be ranking nodes it is not allowed to move, and every existing layout shifts.
The post-pass only touches the layouts that are actually wrong.

### The dev harness (`dev/`, `vite.config.ts`)

The package is a library with no way to drive it by hand, which is part of why
the corridor bug went unnoticed. `pnpm dev` serves `dev/` on port 8091: a DSL
textarea beside a live diagram, with the theme, `tolerant`, `compact` and
`readOnly` switches and the compile diagnostics.

`tsconfig.json` includes `dev` so it is typechecked; `tsconfig.build.json`
excludes it so nothing reaches `dist`.
