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
 * A write the output limit cuts off (`conversation/truncation.ts`): a step
 * that ends on `max_tokens` inside an `addFile`'s arguments fails the turn
 * with `OutputTruncatedError` naming the file and keeps the transcript; the
 * turn ends `agent-error` with code `output_truncated`.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { runConversationTurn } from "../src/conversation/run-conversation-turn.js";
import { OutputTruncatedError, TruncationWatch } from "../src/conversation/truncation.js";
import { codedErrorFrame } from "../src/conversation/turn-error.js";
import { InMemoryConversationStore } from "../src/store/memory-store.js";
import { anthropicConnection, createModel, maxOutputTokensFor } from "../src/shared/model.js";

/** An Anthropic stream that starts an addFile and runs out of output tokens halfway through its body. */
function cutOffStream(): Response {
  const ev = (name: string, data: unknown) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`;
  const body = [
    ev("message_start", {
      type: "message_start",
      message: { id: "m1", type: "message", role: "assistant", model: "claude-sonnet-5", content: [], stop_reason: null, usage: { input_tokens: 10, output_tokens: 1 } },
    }),
    ev("content_block_start", { type: "content_block_start", index: 0, content_block: { type: "tool_use", id: "toolu_1", name: "addFile", input: {} } }),
    ev("content_block_delta", { type: "content_block_delta", index: 0, delta: { type: "input_json_delta", partial_json: '{"path": "specs/requirements/prd.md", "content": "# Overview\\n\\nA checkout' } }),
    ev("content_block_stop", { type: "content_block_stop", index: 0 }),
    ev("message_delta", { type: "message_delta", delta: { stop_reason: "max_tokens" }, usage: { output_tokens: 32000 } }),
    ev("message_stop", { type: "message_stop" }),
  ].join("");
  return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
}

test("an addFile cut off by the output limit fails the turn naming the file", async () => {
  const conn = { ...anthropicConnection("sk-ant-test-0000000000", "claude-sonnet-5"), outputLimit: 32000 };
  const store = new InMemoryConversationStore();
  await assert.rejects(
    runConversationTurn({
      id: "cut",
      instruction: "write the PRD",
      files: {},
      model: createModel(conn, { fetch: (async () => cutOffStream()) as typeof globalThis.fetch }),
      connection: conn,
      journal: { text: "write the PRD", turnId: "t-1" },
      store,
      onEvent: () => {},
    }),
    (err: unknown) => {
      assert.ok(err instanceof OutputTruncatedError);
      assert.deepEqual(err.call, { toolName: "addFile", path: "specs/requirements/prd.md" });
      assert.equal(err.maxOutputTokens, maxOutputTokensFor(conn));
      assert.deepEqual(codedErrorFrame(err, "api.anthropic.com"), {
        type: "error",
        code: "output_truncated",
        error: err.message,
        toolName: "addFile",
        path: "specs/requirements/prd.md",
      });
      return true;
    },
  );
  const stored = await store.get("cut");
  assert.ok(stored, "the transcript is kept");
  assert.equal(stored.turns.length, 1);
});

test("TruncationWatch: a length finish with every call settled, or on prose, is not a cut-off write", () => {
  const settled = new TruncationWatch();
  for (const p of [
    { type: "start-step" },
    { type: "tool-input-start", id: "a", toolName: "addFile" },
    { type: "tool-input-delta", id: "a", delta: '{"path":"x.md","content":"ok"}' },
    { type: "tool-result", toolCallId: "a", toolName: "addFile" },
    { type: "text-delta", text: "and then some" },
    { type: "finish-step", finishReason: "length" },
  ]) settled.observe(p);
  assert.equal(settled.cutOffCall(), undefined);

  // An earlier step's failed call is not this step's cut-off.
  const earlier = new TruncationWatch();
  for (const p of [
    { type: "start-step" },
    { type: "tool-input-start", id: "a", toolName: "editFile" },
    { type: "finish-step", finishReason: "tool-calls" },
    { type: "start-step" },
    { type: "text-delta", text: "prose" },
    { type: "finish-step", finishReason: "length" },
  ]) earlier.observe(p);
  assert.equal(earlier.cutOffCall(), undefined);

  const unnamed = new TruncationWatch();
  for (const p of [
    { type: "start-step" },
    { type: "tool-input-start", id: "a", toolName: "addFile" },
    { type: "tool-input-delta", id: "a", delta: '{"pa' },
    { type: "finish-step", finishReason: "length" },
  ]) unnamed.observe(p);
  assert.deepEqual(unnamed.cutOffCall(), { toolName: "addFile" });
});
