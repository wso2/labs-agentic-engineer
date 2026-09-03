/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import type { BoundaryDirection } from "../domain/cellModel";

/**
 * Post-dagre corridor clearing.
 *
 * Dagre lays the internal graph out on ranks and knows nothing about the cell
 * boundary, so a component that also talks to a gateway gets its boundary edge
 * routed straight through whatever dagre parked downstream of it. In the
 * common `a -> b`, `b -> c`, `a -> east d` shape, the east edge leaves `a` and
 * disappears under `b` and `c`, which sit on the same rank line.
 *
 * This pass runs on dagre's output and nudges only the nodes that actually sit
 * in a boundary edge's way, perpendicular to that edge, so the line stays
 * visible. It is deliberately timid: a nudge that would collide with another
 * node is tried on the opposite side and then abandoned, leaving dagre's
 * placement untouched. Dense graphs therefore keep the layout dagre chose.
 */

export interface PlacedNode {
  id: string;
  x: number;
  y: number;
}

export interface CorridorOptions {
  width: number;
  height: number;
  /** Clear space demanded on either side of the edge line, and between boxes. */
  clearance: number;
}

type Axis = "x" | "y";
type Span = { min: number; max: number };
type Box = Record<Axis, Span>;

/**
 * Every direction reduces to two facts: the axis its edge travels along, and
 * which end of that axis the boundary sits at. The rest of this module is
 * written once against those, so a direction is never switched on again.
 */
const TRAVEL: Record<BoundaryDirection, { axis: Axis; toward: -1 | 1 }> = {
  east: { axis: "x", toward: 1 },
  west: { axis: "x", toward: -1 },
  south: { axis: "y", toward: 1 },
  north: { axis: "y", toward: -1 }
};

/**
 * Sweeps are cheap (a handful of components) but can trade a node back and
 * forth between two corridors, so the loop is capped rather than run to a fixed
 * point.
 */
const maxSweeps = 4;

function across(axis: Axis): Axis {
  return axis === "x" ? "y" : "x";
}

function boxOf(node: PlacedNode, options: CorridorOptions): Box {
  return {
    x: { min: node.x, max: node.x + options.width },
    y: { min: node.y, max: node.y + options.height }
  };
}

function separated(a: Span, b: Span, gap: number) {
  return a.max + gap <= b.min || b.max + gap <= a.min;
}

function overlaps(a: Box, b: Box, gap: number) {
  return !separated(a.x, b.x, gap) && !separated(a.y, b.y, gap);
}

/** Widen a span by `clearance` on both sides. */
function widened(span: Span, clearance: number): Span {
  return { min: span.min - clearance, max: span.max + clearance };
}

/**
 * How far `candidate` sits ahead of `source` on the way to the boundary.
 * Negative means it is behind the source, so the edge never reaches it.
 */
function distanceAhead(source: Box, candidate: Box, direction: BoundaryDirection) {
  const { axis, toward } = TRAVEL[direction];
  return toward === 1 ? candidate[axis].min - source[axis].max : source[axis].min - candidate[axis].max;
}

/** Is `candidate` far enough ahead to be in the way, and inside the corridor? */
function blocks(source: Box, candidate: Box, direction: BoundaryDirection, clearance: number) {
  const { axis, toward } = TRAVEL[direction];
  const ahead =
    toward === 1 ? candidate[axis].max > source[axis].max : candidate[axis].min < source[axis].min;
  const band = widened(source[across(axis)], clearance);
  return ahead && !separated(band, candidate[across(axis)], 0);
}

/**
 * The move that takes `node` fully out of `source`'s corridor, off the `sign`
 * side of it.
 */
function escape(
  source: Box,
  node: PlacedNode,
  direction: BoundaryDirection,
  sign: -1 | 1,
  options: CorridorOptions
): PlacedNode {
  const perpendicular = across(TRAVEL[direction].axis);
  const band = widened(source[perpendicular], options.clearance);
  const span = boxOf(node, options)[perpendicular];
  const distance = sign === -1 ? band.min - span.max : band.max - span.min;
  return { ...node, [perpendicular]: node[perpendicular] + distance };
}

/**
 * Move nodes out of the straight run between a component and the cell boundary.
 *
 * `boundaryDirections` maps a component id to the boundary directions its edges
 * leave or enter by (an inbound edge occludes exactly like an outbound one).
 * Returns a new array in the order given.
 */
export function clearBoundaryCorridors(
  placed: PlacedNode[],
  boundaryDirections: Map<string, Set<BoundaryDirection>>,
  options: CorridorOptions
): PlacedNode[] {
  if (placed.length < 2 || boundaryDirections.size === 0) {
    return placed;
  }

  const current = new Map(placed.map((node) => [node.id, node]));
  const at = (id: string) => current.get(id)!;

  const corridors = placed.flatMap((node) =>
    Array.from(boundaryDirections.get(node.id) ?? [], (direction) => ({ sourceId: node.id, direction }))
  );

  const blockersFor = ({ sourceId, direction }: (typeof corridors)[number]) => {
    const source = boxOf(at(sourceId), options);
    return placed
      .filter((node) => node.id !== sourceId)
      .map((node) => at(node.id))
      .filter((node) => blocks(source, boxOf(node, options), direction, options.clearance))
      // Nearest obstruction first, so the alternating sides read outward from
      // the source rather than in declaration order.
      .sort((left, right) => {
        const box = (node: PlacedNode) => boxOf(node, options);
        return distanceAhead(source, box(left), direction) - distanceAhead(source, box(right), direction);
      });
  };

  /** The corridors, other than its own, that `node` would stand in at `box`. */
  const corridorsBlockedAt = (nodeId: string, box: Box) =>
    corridors.filter(
      ({ sourceId, direction }) =>
        sourceId !== nodeId && blocks(boxOf(at(sourceId), options), box, direction, options.clearance)
    );

  /**
   * A move is only worth making if it lands somewhere no worse than it left:
   * no overlap, and no corridor blocked that this node was not already
   * standing in. Staying in one it already blocked is not a regression.
   */
  const isSafe = (blocker: PlacedNode, candidate: PlacedNode) => {
    const box = boxOf(candidate, options);
    const collides = placed
      .filter((peer) => peer.id !== blocker.id)
      .some((peer) => overlaps(box, boxOf(at(peer.id), options), options.clearance));
    if (collides) {
      return false;
    }

    const before = new Set(corridorsBlockedAt(blocker.id, boxOf(blocker, options)));
    return corridorsBlockedAt(blocker.id, box).every((corridor) => before.has(corridor));
  };

  // One sweep can undo another's work: a node moved clear of one corridor can
  // be pushed back into it while clearing the next. Sweeping until nothing
  // moves settles that. The round cap stops a pair of corridors that can only
  // be satisfied in turn from trading the same node back and forth forever.
  for (let round = 0; round < maxSweeps; round += 1) {
    let moved = false;

    corridors.forEach((corridor) => {
      const source = boxOf(at(corridor.sourceId), options);

      blockersFor(corridor).forEach((blocker, index) => {
        // Alternate sides so a chain of blockers straddles the corridor instead
        // of piling up on one side of an increasingly lopsided cell.
        const sides: readonly (-1 | 1)[] = index % 2 === 0 ? [-1, 1] : [1, -1];

        for (const sign of sides) {
          const candidate = escape(source, blocker, corridor.direction, sign, options);
          if (isSafe(blocker, candidate)) {
            current.set(blocker.id, candidate);
            moved = true;
            return;
          }
        }
        // Nowhere safe to go: dagre's placement stands and the edge stays under
        // the node. Better a hidden line than an overlapping pair.
      });
    });

    if (!moved) {
      break;
    }
  }

  return placed.map((node) => at(node.id));
}
