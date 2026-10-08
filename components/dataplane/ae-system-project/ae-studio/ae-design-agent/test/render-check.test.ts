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

/**
 * The pod-wide bound on prototype render checks (AGT-16): each check is a
 * Node child with a 384 MiB heap inside the 1 Gi container, and one pod runs
 * every project's turns, so checks from different turns wait their turn.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { PrototypeFileTexts, PrototypeFinding, PrototypeRenderCheck } from "@aep/agent-stream";
import { MAX_RENDER_CHECKS, boundedRenderCheck } from "../src/prototype/render-check.js";

const FILES = {} as PrototypeFileTexts;

/** A check that holds until released, counting how many run at once. */
function heldCheck(fail = false) {
  let running = 0;
  let peak = 0;
  const waiting: Array<() => void> = [];
  const check: PrototypeRenderCheck = async () => {
    running++;
    peak = Math.max(peak, running);
    await new Promise<void>((r) => waiting.push(r));
    running--;
    if (fail) throw new Error("child died");
    return [] as PrototypeFinding[];
  };
  return { check, peak: () => peak, started: () => waiting.length, releaseOne: () => waiting.shift()?.() };
}

const tick = () => new Promise((r) => setImmediate(r));

test("one render check runs at a time in the pod; the next starts when it ends", async () => {
  assert.equal(MAX_RENDER_CHECKS, 1);
  const held = heldCheck();
  const bounded = boundedRenderCheck(held.check, MAX_RENDER_CHECKS);
  const all = Promise.all([bounded(FILES), bounded(FILES), bounded(FILES)]);
  await tick();
  assert.equal(held.started(), 1, "two checks wait");
  for (let i = 0; i < 3; i++) {
    held.releaseOne();
    await tick();
  }
  await all;
  assert.equal(held.peak(), 1);
});

test("a check that throws frees its slot", async () => {
  const held = heldCheck(true);
  const bounded = boundedRenderCheck(held.check, 1);
  const first = bounded(FILES);
  const second = bounded(FILES);
  await tick();
  held.releaseOne();
  await assert.rejects(first, /child died/);
  await tick();
  assert.equal(held.started(), 1, "the waiting check started");
  held.releaseOne();
  await assert.rejects(second, /child died/);
});
