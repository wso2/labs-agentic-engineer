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
 * One requirements turn per format, replayed from provider cassettes
 * (`provider-cassette.ts`) through the real `createModel` and turn loop:
 *
 * - `anthropic-requirements`: Anthropic's own API — signed reasoning, a
 *   server-run `web_search`, then `addFile` (constructed from the provider's
 *   documented stream shape).
 * - `ollama-requirements`: `gpt-oss:20b` on Ollama's OpenAI-compatible
 *   endpoint, recorded live (auth scrubbed).
 *
 * And the switch between them: an Anthropic conversation holding a server
 * `web_search` continues on the OpenAI-compatible connection with a request
 * such a host accepts.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import type { StreamPart } from "@aep/agent-stream";
import { runConversationTurn, TurnGuard } from "../src/conversation/run-conversation-turn.js";
import { InMemoryConversationStore } from "../src/store/memory-store.js";
import { anthropicConnection, createModel, type ModelConnection } from "../src/shared/model.js";
import { replayProvider } from "./provider-cassette.js";

const cassettes = (name: string) => fileURLToPath(new URL(`./fixtures/provider-cassettes/${name}`, import.meta.url));

const ANTHROPIC = anthropicConnection("sk-ant-test-key-0000000000", "claude-sonnet-5");
const OLLAMA: ModelConnection = {
  format: "openai-compatible",
  baseURL: "https://ollama.com/v1",
  authScheme: "bearer",
  capabilities: {
    claudeCode: false,
    claudeSubscription: false,
    promptCache: false,
    generatedAgents: false,
    nativePdf: false,
    webSearch: "ollama-api",
    imageInput: "unknown",
  },
  apiKey: "ollama-test-key-0000000000",
  model: "gpt-oss:20b",
};

async function replayTurn(
  store: InMemoryConversationStore,
  conn: ModelConnection,
  cassette: string,
  turnId: string,
): Promise<{ events: StreamPart[]; requests: ReturnType<typeof replayProvider>["requests"] }> {
  const provider = replayProvider(cassettes(cassette));
  const events: StreamPart[] = [];
  await runConversationTurn({
    id: "conv",
    instruction: "Write the PRD for the idea.",
    files: {},
    webSearch: true,
    model: createModel(conn, { fetch: provider.fetch }),
    connection: conn,
    journal: { text: "start", turnId },
    store,
    guard: new TurnGuard(),
    onEvent: (p) => events.push(p),
  });
  return { events, requests: provider.requests };
}

const manifestOf = (events: StreamPart[]) => events.find((e) => e.type === "manifest");

test("Anthropic: a requirements turn with a server web_search replays clean", async () => {
  const store = new InMemoryConversationStore();
  const { events, requests } = await replayTurn(store, ANTHROPIC, "anthropic-requirements", "t-1");

  assert.equal(requests.length, 2);
  for (const r of requests) {
    assert.equal(r.url, "https://api.anthropic.com/v1/messages");
    assert.equal(r.headers["x-api-key"], ANTHROPIC.apiKey);
  }
  const tools = requests[0]!.body.tools as Array<{ name: string; type?: string }>;
  assert.equal(tools.find((t) => t.name === "web_search")?.type, "web_search_20250305");
  assert.ok(tools.some((t) => t.name === "addFile"));

  const manifest = manifestOf(events);
  assert.deepEqual(Object.keys(manifest?.files ?? {}), ["specs/requirements/prd.md"]);
  assert.equal(manifest?.usage?.model, "claude-sonnet-5");
  const stored = await store.get("conv");
  assert.deepEqual(stored?.turns.map((t) => t.connection), ["anthropic@api.anthropic.com"]);
});

test("OpenAI-compatible: a requirements turn recorded on Ollama gpt-oss:20b replays clean", async () => {
  const store = new InMemoryConversationStore();
  const { events, requests } = await replayTurn(store, OLLAMA, "ollama-requirements", "t-1");

  assert.equal(requests.length, 2);
  for (const r of requests) {
    assert.equal(r.url, "https://ollama.com/v1/chat/completions");
    assert.equal(r.headers.authorization, `Bearer ${OLLAMA.apiKey}`);
    assert.equal(r.body.model, "gpt-oss:20b");
    assert.deepEqual(r.body.stream_options, { include_usage: true });
  }
  const tools = requests[0]!.body.tools as Array<{ type: string; function: { name: string } }>;
  assert.deepEqual(
    tools.filter((t) => t.function.name === "web_search").map((t) => t.type),
    ["function"],
    "Ollama's search is a client tool",
  );

  const manifest = manifestOf(events);
  assert.deepEqual(Object.keys(manifest?.files ?? {}), ["specs/requirements/prd.md"]);
  assert.equal(manifest?.usage?.model, "gpt-oss:20b");
  assert.ok((manifest?.usage?.inputTokens ?? 0) > 0, "the usage chunk was read");
  assert.equal(events.some((e) => e.type === "error"), false);
});

// Unfiltered, the openai-compatible converter sends Anthropic's server
// web_search as an assistant tool_call no tool message answers, which an
// OpenAI-compatible host refuses with a 400.
test("an Anthropic conversation with a web_search continues on an OpenAI-compatible connection with an acceptable request", async () => {
  const store = new InMemoryConversationStore();
  await replayTurn(store, ANTHROPIC, "anthropic-requirements", "t-1");
  const before = structuredClone((await store.get("conv"))!.messages);
  const { events, requests } = await replayTurn(store, OLLAMA, "ollama-requirements", "t-2");

  const messages = requests[0]!.body.messages as Array<{ role: string; content?: unknown; tool_calls?: Array<{ id: string; function: { name: string } }>; tool_call_id?: string; reasoning_content?: string }>;
  const answered = new Set(messages.filter((m) => m.role === "tool").map((m) => m.tool_call_id));
  for (const m of messages) {
    for (const call of m.tool_calls ?? []) {
      assert.notEqual(call.function.name, "web_search", "the server-run search is not replayed as a client call");
      assert.ok(answered.has(call.id), `tool_call ${call.id} has its tool message`);
    }
    assert.equal(m.reasoning_content, undefined, "Anthropic's signed reasoning is not replayed");
  }
  assert.ok(JSON.stringify(messages).includes("Stripe's PaymentIntents API exists"), "the prose is kept");
  assert.ok(messages.some((m) => m.tool_calls?.some((c) => c.function.name === "addFile")), "client tool calls are kept");

  assert.ok(manifestOf(events), "the turn completed");
  const stored = (await store.get("conv"))!;
  assert.deepEqual(stored.messages.slice(0, before.length), before, "the stored Anthropic turn is untouched");
  assert.equal(stored.messages.filter((m) => m.role === "user").length, 2, "the new turn landed in the transcript");
  assert.deepEqual(stored.turns.map((t) => t.connection), ["anthropic@api.anthropic.com", "openai-compatible@ollama.com"]);
});
