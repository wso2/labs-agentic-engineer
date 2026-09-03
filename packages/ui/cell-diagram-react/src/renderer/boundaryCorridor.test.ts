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

import { describe, expect, it } from "vitest";
import type { BoundaryDirection } from "../domain/cellModel";
import { clearBoundaryCorridors, type PlacedNode } from "./boundaryCorridor";

const options = { width: 100, height: 100, clearance: 20 };

function directions(entries: Record<string, BoundaryDirection[]>) {
  return new Map(Object.entries(entries).map(([id, dirs]) => [id, new Set(dirs)]));
}

/** Does the corridor east of `source` still run clear of `node`? */
function clearsEastCorridor(source: PlacedNode, node: PlacedNode) {
  const downstream = node.x + options.width > source.x + options.width;
  const inBand = node.y < source.y + options.height + options.clearance && source.y - options.clearance < node.y + options.height;
  return !(downstream && inBand);
}

describe("clearBoundaryCorridors", () => {
  it("nudges a straight run of components off an east boundary edge", () => {
    // The `a -> b`, `b -> c`, `a -> east d` shape: dagre puts all three on one
    // rank line, so the east edge from `a` runs under `b` and `c`.
    const placed: PlacedNode[] = [
      { id: "a", x: 0, y: 0 },
      { id: "b", x: 200, y: 0 },
      { id: "c", x: 400, y: 0 }
    ];

    const result = clearBoundaryCorridors(placed, directions({ a: ["east"] }), options);
    const [a, b, c] = result;

    expect(a).toEqual({ id: "a", x: 0, y: 0 });
    expect(clearsEastCorridor(a!, b!)).toBe(true);
    expect(clearsEastCorridor(a!, c!)).toBe(true);
    // Straddle the corridor rather than piling up on one side.
    expect(b!.y).toBeLessThan(a!.y);
    expect(c!.y).toBeGreaterThan(a!.y);
    // Only the perpendicular axis moves — dagre's ranking is preserved.
    expect(b!.x).toBe(200);
    expect(c!.x).toBe(400);
  });

  it("leaves a layout dagre already routed cleanly alone", () => {
    const placed: PlacedNode[] = [
      { id: "a", x: 0, y: 0 },
      { id: "b", x: 200, y: 300 }
    ];

    expect(clearBoundaryCorridors(placed, directions({ a: ["east"] }), options)).toEqual(placed);
  });

  it("ignores nodes behind the source — only the run to the boundary matters", () => {
    const placed: PlacedNode[] = [
      { id: "upstream", x: -200, y: 0 },
      { id: "a", x: 0, y: 0 }
    ];

    expect(clearBoundaryCorridors(placed, directions({ a: ["east"] }), options)).toEqual(placed);
  });

  // One case per direction, because a direction is a row in the module's TRAVEL
  // table and a wrong axis or sign there is invisible to the east-only cases.
  // `blocker` sits directly in the corridor; `along` is the axis the edge
  // travels, which must not move, and the other axis is where the nudge goes.
  const perDirection = [
    { direction: "east" as const, source: { x: 0, y: 0 }, blocker: { x: 300, y: 0 }, along: "x" as const },
    { direction: "west" as const, source: { x: 300, y: 0 }, blocker: { x: 0, y: 0 }, along: "x" as const },
    { direction: "south" as const, source: { x: 0, y: 0 }, blocker: { x: 0, y: 300 }, along: "y" as const },
    { direction: "north" as const, source: { x: 0, y: 300 }, blocker: { x: 0, y: 0 }, along: "y" as const }
  ];

  perDirection.forEach(({ direction, source, blocker, along }) => {
    it(`clears the corridor perpendicular to the axis for a ${direction} edge`, () => {
      const placed: PlacedNode[] = [
        { id: "a", ...source },
        { id: "b", ...blocker }
      ];
      const across = along === "x" ? "y" : "x";
      const size = along === "x" ? options.height : options.width;

      const [, moved] = clearBoundaryCorridors(placed, directions({ a: [direction] }), options);

      // The edge's own axis is untouched: dagre's ranking survives.
      expect(moved![along]).toBe(blocker[along]);
      // And the blocker has left the band entirely.
      expect(Math.abs(moved![across] - source[across])).toBeGreaterThanOrEqual(size + options.clearance);
    });
  });

  it("keeps dagre's placement when both sides of the corridor are occupied", () => {
    // `blocker` is boxed in above and below; moving it either way would collide,
    // so the occluded edge is accepted rather than creating an overlap.
    const placed: PlacedNode[] = [
      { id: "a", x: 0, y: 0 },
      { id: "blocker", x: 200, y: 0 },
      { id: "above", x: 200, y: -130 },
      { id: "below", x: 200, y: 130 }
    ];

    const result = clearBoundaryCorridors(placed, directions({ a: ["east"] }), options);

    expect(result.find((node) => node.id === "blocker")).toEqual({ id: "blocker", x: 200, y: 0 });
  });

  it("returns the input untouched when no component touches a boundary", () => {
    const placed: PlacedNode[] = [
      { id: "a", x: 0, y: 0 },
      { id: "b", x: 200, y: 0 }
    ];

    expect(clearBoundaryCorridors(placed, directions({}), options)).toEqual(placed);
  });
  it("does not push a node out of one corridor and into another", () => {
    // Two east sources whose bands nearly touch. Clearing `e`'s corridor by the
    // nearest route would drop `c` straight into `a`'s, so it has to go the
    // other way.
    const placed: PlacedNode[] = [
      { id: "a", x: 0, y: 0 },
      { id: "e", x: 0, y: 150 },
      { id: "b", x: 300, y: 0 },
      { id: "c", x: 300, y: 150 }
    ];

    const result = clearBoundaryCorridors(placed, directions({ a: ["east"], e: ["east"] }), options);
    const byId = new Map(result.map((node) => [node.id, node]));
    const [a, e, b, c] = ["a", "e", "b", "c"].map((id) => byId.get(id)!);

    expect(clearsEastCorridor(a, b)).toBe(true);
    expect(clearsEastCorridor(a, c)).toBe(true);
    expect(clearsEastCorridor(e, b)).toBe(true);
    expect(clearsEastCorridor(e, c)).toBe(true);
    // And the two blockers did not land on top of each other.
    expect(Math.abs(b.y - c.y)).toBeGreaterThanOrEqual(options.height);
  });

  it("settles rather than trading a node between two corridors forever", () => {
    // `b` sits in a's east corridor and in s's north corridor at once. Whatever
    // the pass decides, it has to stop deciding.
    const placed: PlacedNode[] = [
      { id: "a", x: 0, y: 0 },
      { id: "s", x: 300, y: 400 },
      { id: "b", x: 300, y: 0 }
    ];

    const result = clearBoundaryCorridors(placed, directions({ a: ["east"], s: ["north"] }), options);

    // No pair of nodes may overlap, whatever compromise it reached.
    result.forEach((left, index) => {
      result.slice(index + 1).forEach((right) => {
        const apart =
          Math.abs(left.x - right.x) >= options.width || Math.abs(left.y - right.y) >= options.height;
        expect(apart).toBe(true);
      });
    });
  });
});
