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

// Exercises the /chat contract every platform ai-agent speaks under server
// memory (skills/agent-building) against the REAL AI SDK.
//
// READ THIS BEFORE TRUSTING IT. Nothing here imports from this repo: the store
// and the handler below are written in this file, and the skill's prescribed
// SQL is never executed. So this file CANNOT catch the skill drifting — delete
// the user fence from its SELECT and every test here still passes. The gate
// that catches that is agent-building-skill-gate.test.ts, which asserts on the
// markdown itself; the two are meant to be read together.
//
// What this file genuinely proves is one thing a repo change could not
// silently invalidate: that the AI SDK's generateText + stepCountIs returns
// `steps[].response.messages` in an order which, appended to the caller's own
// turn, reconstitutes a usable history INCLUDING the tool trail. Everything
// below is that fact, dressed in the contract's shape so the shape stays
// legible. The claims it demonstrates (not proves-in-production):
//   1. A turn without a conversationId creates one and returns it; the caller
//      stores ONLY the id.
//   2. The agent persists the FULL conversation (history + user turn + trail,
//      tool results included) — the next turn remembers without the caller
//      resending anything.
//   3. Conversations are fenced by user: another user's id is as good as
//      nonexistent (404 semantics, never 403).

import { test } from "node:test";
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { generateText, stepCountIs, tool, type ModelMessage } from "ai";
import { MockLanguageModelV3 } from "ai/test";
import { z } from "zod";

type Generated = Awaited<ReturnType<MockLanguageModelV3["doGenerate"]>>;

const usage: Generated["usage"] = {
  inputTokens: { total: 1, noCache: 1, cacheRead: undefined, cacheWrite: undefined },
  outputTokens: { total: 1, text: 1, reasoning: undefined },
};

/** First call makes a tool call; every later call answers in text. */
function toolCallingModel() {
  let call = 0;
  return new MockLanguageModelV3({
    doGenerate: async (): Promise<Generated> => {
      call += 1;
      if (call === 1) {
        return {
          finishReason: { unified: "tool-calls", raw: undefined },
          usage,
          content: [{
            type: "tool-call", toolCallId: "call-1",
            toolName: "listHotels", input: JSON.stringify({ city: "Paris" }),
          }],
          warnings: [],
        };
      }
      return {
        finishReason: { unified: "stop", raw: undefined },
        usage,
        content: [{ type: "text", text: `answer ${call}` }],
        warnings: [],
      };
    },
  });
}

// --- the prescribed store + turn flow (skills/agent-building), Map-backed ---

type Row = { userId: string; messages: ModelMessage[] };
const store = new Map<string, Row>();

function loadConversation(id: string, userId: string): ModelMessage[] | null {
  const row = store.get(id);
  return row && row.userId === userId ? row.messages : null; // WHERE user_id = $2
}
// Mirrors the skill's INSERT .. ON CONFLICT upsert: there is NO separate
// "create the row" step. A new id is minted in memory only, and the row comes
// into existence on the first successful save — so a turn that throws before
// saving leaves nothing behind to orphan. The earlier shape here (insert an
// empty row up front, update-only save) is one the skill explicitly forbids.
function newConversationId(): string {
  return randomUUID();
}
function saveConversation(id: string, userId: string, messages: ModelMessage[]): void {
  const row = store.get(id);
  if (row && row.userId !== userId) return; // WHERE conversations.user_id = $2
  store.set(id, { userId, messages });
}

/** The exact handler flow skills/agent-building prescribes. */
async function chatTurn(userId: string, body: { conversationId?: string; message: string }) {
  // A fresh mock model per turn: each real request gets its own model call
  // sequence, so the mock's call counter must not leak state across turns.
  const model = toolCallingModel();
  let history: ModelMessage[];
  let conversationId: string;
  if (body.conversationId !== undefined) {
    const loaded = loadConversation(body.conversationId, userId);
    if (loaded === null) return { status: 404 as const };
    history = loaded;
    conversationId = body.conversationId;
  } else {
    conversationId = newConversationId();
    history = [];
  }
  const full: ModelMessage[] = [...history, { role: "user", content: body.message }];
  const result = await generateText({
    model,
    messages: full,
    tools: {
      listHotels: tool({
        inputSchema: z.object({ city: z.string() }),
        execute: async () => [{ id: "h1", name: "Ritz" }],
      }),
    },
    stopWhen: stepCountIs(5),
  });
  saveConversation(conversationId, userId, [
    ...full,
    ...result.steps.flatMap((s) => s.response.messages),
  ]);
  return {
    status: 200 as const,
    body: { conversationId, text: result.text, toolCalls: result.toolCalls as unknown[] },
  };
}

test("first turn creates a conversation and the reply lives in text", async () => {
  const res = await chatTurn("user-a", { message: "find me a hotel in Paris" });
  assert.equal(res.status, 200);
  assert.ok(res.body!.conversationId, "an id is issued");
  assert.equal(res.body!.text, "answer 2");
  assert.equal(res.body!.toolCalls.length, 1);
  // the wire carries NO messages array
  assert.ok(!("messages" in res.body!));
});

test("second turn remembers: full conversation, tool trail included, persisted server-side", async () => {
  const first = await chatTurn("user-b", { message: "find me a hotel in Paris" });
  const id = first.body!.conversationId;
  const afterFirst = store.get(id)!.messages;
  assert.equal(afterFirst[0]!.role, "user", "caller's own turn persisted first");
  assert.ok(afterFirst.some((m) => m.role === "tool"), "tool result in the stored trail");

  const second = await chatTurn("user-b", { conversationId: id, message: "book the first one" });
  assert.equal(second.status, 200);
  const afterSecond = store.get(id)!.messages;
  assert.ok(afterSecond.length > afterFirst.length, "history grew — append-only");
  assert.equal(afterSecond[afterFirst.length]!.role, "user", "second user turn appended after the first turn's trail");
});

test("another user's conversation id is a 404, and nothing about it leaks", async () => {
  const first = await chatTurn("user-c", { message: "hello" });
  const id = first.body!.conversationId;
  const stolen = await chatTurn("user-d", { conversationId: id, message: "what did user-c say?" });
  assert.equal(stolen.status, 404);
  assert.equal(store.get(id)!.userId, "user-c", "row untouched");
});

test("an unknown conversation id is a 404, indistinguishable from foreign", async () => {
  const res = await chatTurn("user-e", { conversationId: randomUUID(), message: "hi" });
  assert.equal(res.status, 404);
});
