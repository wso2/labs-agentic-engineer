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
 * The trace-capture seam. Two properties matter more than the labels
 * themselves: capture can never break a turn, and a malformed conversation id
 * can never break one either — a debugging aid that can fail a generation is
 * worse than no debugging aid.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { Telemetry } from "ai";
import { nonFatalTelemetry, threadLabel } from "../src/shared/telemetry.js";

test("threadLabel renders a namespaced id as org/project/useCase", () => {
  assert.equal(
    threadLabel("org_default--proj_staff-maintain--design--0192a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"),
    "default/staff-maintain/design",
  );
});

test("threadLabel returns anything else verbatim rather than throwing", () => {
  // Evals, the playground and lazily-created conversations use plain ids. A
  // LABEL must never be able to fail a turn, so unparseable is not an error.
  for (const id of ["c-42", "", "org_only--two", "not--four--segments--but--five"]) {
    assert.equal(threadLabel(id), id, `\`${id}\` must pass through untouched`);
  }
});

test("a throwing observer hook cannot break the turn", async () => {
  const exploding: Telemetry = {
    onStart: () => {
      throw new Error("capture file is corrupt");
    },
  };
  // The SDK awaits integration callbacks with NO try/catch of its own, so an
  // unguarded throw here propagates into the generation — this is the failure
  // that once surfaced as "stream ended without a manifest".
  await assert.doesNotReject(() => Promise.resolve(nonFatalTelemetry(exploding).onStart!({} as never)));
});

test("a throwing executeTool falls through to the real tool instead of losing its result", async () => {
  let ran = 0;
  const exploding: Telemetry = {
    executeTool: () => {
      throw new Error("capture file is corrupt");
    },
  };
  const result = await nonFatalTelemetry(exploding).executeTool!({
    callId: "c1",
    toolCallId: "t1",
    execute: () => {
      ran += 1;
      return Promise.resolve("tool output");
    },
  } as never);

  // executeTool WRAPS the tool — swallowing it would drop the result and leave
  // the model with nothing, so the guard must call through, exactly once.
  assert.equal(result, "tool output");
  assert.equal(ran, 1, "the underlying tool runs exactly once");
});

test("hooks the integration does not implement are not invented", () => {
  const guarded = nonFatalTelemetry({ onStart: () => {} });
  assert.equal(typeof guarded.onStart, "function");
  assert.equal(guarded.onStepEnd, undefined, "absent hooks stay absent");
  assert.equal(guarded.executeTool, undefined, "absent wrapper stays absent");
});

test("a turn's functionId reaches a registered telemetry integration", async () => {
  // End-to-end through the REAL SDK telemetry path: `registerTelemetry` +
  // `agent.stream({ telemetry })`. DevTools reads `event.functionId` and writes
  // it to the run row, so this is the hop that decides whether a run is
  // attributable — asserted against a fake integration so it needs no provider,
  // no key and no capture file.
  const { registerTelemetry } = await import("ai");
  const { runTurn } = await import("../src/agents/main/run-turn.js");
  const { mockModel } = await import("../src/shared/mock-model.js");

  const seen: (string | undefined)[] = [];
  registerTelemetry({
    onStart: (event: { functionId?: string }) => {
      seen.push(event.functionId);
    },
  } as never);

  try {
    await runTurn({
      model: mockModel([{ kind: "text", text: "Done." }]),
      instructions: "You are a spec agent.",
      tools: {},
      messages: [],
      prompt: "hello",
      telemetry: { functionId: "default/staff-maintain/design" },
    });
    assert.deepEqual(seen, ["default/staff-maintain/design"]);
  } finally {
    registerTelemetry(); // clear — global state must not leak into other tests
  }
});
