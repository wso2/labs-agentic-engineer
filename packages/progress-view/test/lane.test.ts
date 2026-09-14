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

// The interval arithmetic under a lane. The crew tests prove it against two real
// runs; these are the shapes those two runs happen not to contain.

import { test } from "node:test";
import assert from "node:assert/strict";
// Imported from the module rather than the entry point: this is the algebra
// UNDER a lane, and the package's entry point is the list of things a renderer
// actually calls.
import { laneSpans, mergeIntervals } from "../src/lane.js";

const shape = (spans: { kind: string; startMs: number; endMs: number }[]) =>
  spans.map((s) => [s.kind, s.startMs, s.endMs]);

test("lane: two agents spawned at once are ONE wait, not two overlapping ones", () => {
  // Drawn separately, the second wait covers the first and the working gap
  // between them disappears rather than being shown.
  assert.deepEqual(
    mergeIntervals([
      { startMs: 10, endMs: 40 },
      { startMs: 20, endMs: 60 },
      { startMs: 80, endMs: 90 },
    ]),
    [
      { startMs: 10, endMs: 60 },
      { startMs: 80, endMs: 90 },
    ],
  );
});

test("lane: an instant is not a stretch", () => {
  assert.deepEqual(mergeIntervals([{ startMs: 10, endMs: 10 }]), []);
  assert.deepEqual(laneSpans(100, 100), []);
  assert.deepEqual(laneSpans(100, 50), []);
});

test("lane: waits are clamped to the lane, never allowed to overhang it", () => {
  // A backgrounded child routinely outlives the parent's own settle. Drawn
  // past the lane's end it would put that child's time on the parent's row —
  // in the one view whose whole job is to say who spent what.
  assert.deepEqual(
    shape(laneSpans(0, 100, [{ startMs: 60, endMs: 500 }])),
    [
      ["working", 0, 60],
      ["waiting", 60, 100],
    ],
  );
});

test("lane: a lane spent entirely inside one child is one waiting stretch", () => {
  assert.deepEqual(shape(laneSpans(0, 100, [{ startMs: 0, endMs: 100 }])), [["waiting", 0, 100]]);
});

test("lane: a lane with nothing to wait on is one working stretch", () => {
  assert.deepEqual(shape(laneSpans(0, 100)), [["working", 0, 100]]);
});

test("lane: work between two waits keeps its own stretch", () => {
  assert.deepEqual(
    shape(
      laneSpans(0, 100, [
        { startMs: 20, endMs: 40 },
        { startMs: 60, endMs: 80 },
      ]),
    ),
    [
      ["working", 0, 20],
      ["waiting", 20, 40],
      ["working", 40, 60],
      ["waiting", 60, 80],
      ["working", 80, 100],
    ],
  );
});
