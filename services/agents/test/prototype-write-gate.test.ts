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
 * The prototype write gate end to end in the agents service: a turn's model
 * writes a prototype through the real tool loop, and the bundle the turn builds
 * judges it with the real isolated render check on the Oxygen theme's runtime.
 * The static stages are pinned in @aep/agent-stream; this pins what only the
 * service can: that the render check is wired in and sees what the model wrote.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { OpErr, OpResult, StreamPart } from "@aep/agent-stream";
import { runConversationTurn, TurnGuard } from "../src/conversation/run-conversation-turn.js";
import { checkPrototypeRender } from "../src/prototype/render-check.js";
import { InMemoryConversationStore } from "../src/store/memory-store.js";
import { mockModel, type MockStep } from "../src/shared/mock-model.js";

const MANIFEST_PATH = "specs/design/components/web/prototype.json";
const SOURCE_PATH = "specs/design/components/web/prototype.tsx";

const MANIFEST = JSON.stringify({
  schemaVersion: 3,
  name: "Demo",
  entryScreen: "screen.home",
  roles: [{ id: "user", name: "User" }],
  states: [
    { id: "state.default", name: "Default" },
    { id: "state.empty", name: "Empty" },
  ],
  screens: [{ id: "screen.home", name: "Home", roleIds: ["user"] }],
  flows: [],
});

/** Draws in every state; `rows` is the part a test breaks. */
function source(rows: string): string {
  return `
import { Heading, Screen, Text, defineApp, useDisplayState } from "@wso2/prototype-kit";

function Home() {
  const state = useDisplayState();
  const rows: string[] = ${rows};
  return (
    <Screen>
      <Heading id="heading.home" text="Home" />
      <Text id="text.rows" text={state === "state.empty" ? "Nothing here yet" : rows.join(", ")} />
    </Screen>
  );
}

export default defineApp({ screens: { "screen.home": Home } });
`;
}

const GOOD = source('["a", "b"]');
/** Draws in the default state and throws in the empty one: only a render finds it. */
const THROWS_WHEN_EMPTY = GOOD.replace('state === "state.empty" ? "Nothing here yet"', 'state === "state.empty" ? (undefined as unknown as string[]).join(",")');

type Write = [path: string, content: string] | { remove: string };

async function writes(...contents: Write[]): Promise<{ results: OpResult[] }> {
  const steps: MockStep[] = contents.map((w, i) =>
    Array.isArray(w)
      ? { kind: "toolCall", toolCallId: `c${i}`, toolName: "addFile", input: { path: w[0], content: w[1] } }
      : { kind: "toolCall", toolCallId: `c${i}`, toolName: "removeFile", input: { path: w.remove } },
  );
  steps.push({ kind: "text", text: "done" });
  const events: StreamPart[] = [];
  await runConversationTurn({
    id: "proto",
    instruction: "write the prototype",
    files: {},
    model: mockModel(steps),
    store: new InMemoryConversationStore(),
    guard: new TurnGuard(),
    onEvent: (p) => events.push(p),
  });
  const results = events.filter((e) => e.type === "tool-result").map((e) => (e as unknown as { output: OpResult }).output);
  return { results };
}

test("a prototype that draws in every role and state lands, manifest first", async () => {
  const { results } = await writes([MANIFEST_PATH, MANIFEST], [SOURCE_PATH, GOOD]);
  assert.deepEqual(
    results.map((r) => r.ok),
    [true, true],
    JSON.stringify(results),
  );
});

test("a screen that throws in one display state is refused with the kit's RENDER_FAILED finding, then a fix lands", async () => {
  const { results } = await writes([MANIFEST_PATH, MANIFEST], [SOURCE_PATH, THROWS_WHEN_EMPTY], [SOURCE_PATH, GOOD]);
  assert.equal(results.length, 3);
  const refused = results[1] as OpErr;
  assert.equal(refused.ok, false);
  assert.equal(refused.code, "INVALID_PROTOTYPE");
  const render = refused.findings?.find((f) => f.code === "RENDER_FAILED");
  assert.ok(render, JSON.stringify(refused.findings));
  assert.equal(render.file, "prototype.tsx");
  assert.match(render.location, /screen\.home as user in state\.empty/);
  assert.match(refused.message, /RENDER_FAILED/);
  assert.equal(results[2]!.ok, true, "the corrected file is accepted");
});

test("screens that are not the manifest's are refused by the render check", async () => {
  const mismatched = GOOD.replace('"screen.home": Home', '"screen.other": Home');
  const { results } = await writes([MANIFEST_PATH, MANIFEST], [SOURCE_PATH, mismatched]);
  const refused = results[1] as OpErr;
  assert.equal(refused.ok, false);
  assert.equal(refused.code, "INVALID_PROTOTYPE");
  assert.ok(refused.findings?.some((f) => f.code === "SCREEN_MISMATCH"), JSON.stringify(refused.findings));
});

test("the source before its manifest is refused without rendering anything", async () => {
  const { results } = await writes([SOURCE_PATH, GOOD]);
  const refused = results[0] as OpErr;
  assert.equal(refused.code, "INVALID_PROTOTYPE");
  assert.deepEqual(refused.findings?.map((f) => f.code), ["MISSING_FILE"]);
});

test("a manifest change that breaks the screens already written is refused by the render check", async () => {
  // The screen renamed: prototype.tsx still draws "screen.home", which the new manifest no longer has.
  const renamed = MANIFEST.replaceAll('"screen.home"', '"screen.start"');
  const { results } = await writes([MANIFEST_PATH, MANIFEST], [SOURCE_PATH, GOOD], { remove: MANIFEST_PATH }, [MANIFEST_PATH, renamed]);
  const refused = results[3] as OpErr;
  assert.equal(refused.ok, false, JSON.stringify(results));
  assert.equal(refused.code, "INVALID_PROTOTYPE");
  assert.match(refused.message, /prototype\.json would break/);
  assert.ok(refused.findings?.some((f) => f.code === "SCREEN_MISMATCH"), JSON.stringify(refused.findings));
});

test("the service's event loop keeps running while a prototype is drawn", async () => {
  // How long one render takes here, so the gap below is judged against it.
  const started = performance.now();
  assert.deepEqual(await checkPrototypeRender({ manifest: MANIFEST, source: GOOD }), []);
  const renderMs = performance.now() - started;

  const ticks: number[] = [];
  const timer = setInterval(() => ticks.push(performance.now()), 5);
  try {
    const { results } = await writes([MANIFEST_PATH, MANIFEST], [SOURCE_PATH, GOOD]);
    assert.deepEqual(results.map((r) => r.ok), [true, true]);
  } finally {
    clearInterval(timer);
  }
  const longestGap = Math.max(...ticks.slice(1).map((t, i) => t - ticks[i]!));
  // A synchronous check would hold the loop for a whole render: no tick in between.
  assert.ok(longestGap < renderMs / 2, `longest gap between timer ticks ${longestGap.toFixed(0)} ms, one render ${renderMs.toFixed(0)} ms`);
});
