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
 * The model seam per format: which provider SDK a connection builds, what the
 * request carries (auth header, endpoint, usage opt-in), and the per-call
 * options (effort / reasoning_effort, the output ceiling, the cache marker)
 * each format takes.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { generateText, streamText } from "ai";
import { createAnthropic } from "@ai-sdk/anthropic";
import type { TurnConnection } from "@aep/agent-stream";
import { config } from "../src/shared/config.js";
import {
  anthropicConnection,
  connectionFingerprint,
  connectionFromWire,
  createModel,
  maxOutputTokensFor,
  modelCacheBreakpoint,
  modelProviderOptions,
  resolveModelId,
  supportsEffort,
  type ModelConnection,
} from "../src/shared/model.js";
import { HostRefusedError } from "../src/shared/guarded-fetch.js";

const KEY = "sk-ant-test-key-000000000000";
const OLLAMA_KEY = "ollama-test-key-0000000000";

/** An OpenAI-compatible connection on Ollama, as aep-api resolves it. */
const OLLAMA: ModelConnection = {
  format: "openai-compatible",
  baseURL: "https://ollama.com/v1",
  authScheme: "bearer",
  contextWindow: 131072,
  outputLimit: 32000,
  capabilities: {
    claudeCode: false,
    claudeSubscription: false,
    promptCache: false,
    generatedAgents: false,
    nativePdf: false,
    webSearch: "ollama-api",
    imageInput: "no",
  },
  apiKey: OLLAMA_KEY,
  model: "gpt-oss:20b",
};

test("Anthropic format: Sonnet 5 turns carry the configured effort; Haiku 4.5 and Sonnet 4.5 carry none", () => {
  const anthropic = (model: string) => ({ format: "anthropic" as const, model });
  assert.deepEqual(modelProviderOptions(anthropic("claude-sonnet-5")), { anthropic: { effort: config.reasoningEffort } });
  assert.equal(modelProviderOptions(anthropic("claude-haiku-4-5")), undefined);
  assert.equal(modelProviderOptions(anthropic("claude-haiku-4-5-20251001")), undefined);
  assert.equal(supportsEffort("claude-sonnet-4-5"), false);
  assert.equal(supportsEffort("claude-opus-5"), true);
});

test("OpenAI-compatible: effort becomes reasoning_effort, and the Claude deny-list does not apply", () => {
  const expected = { aep: { reasoningEffort: config.reasoningEffort } };
  assert.deepEqual(modelProviderOptions(OLLAMA), expected);
  assert.deepEqual(modelProviderOptions({ format: "openai-compatible", model: "claude-haiku-4-5" }), expected);
});

test("the output ceiling is AGENT_MAX_OUTPUT_TOKENS, lowered to the connection's own limit", () => {
  assert.equal(maxOutputTokensFor({}), config.maxOutputTokens);
  assert.equal(maxOutputTokensFor({ outputLimit: 8000 }), Math.min(config.maxOutputTokens, 8000));
  assert.equal(maxOutputTokensFor({ outputLimit: config.maxOutputTokens + 1 }), config.maxOutputTokens);
});

test("the cache marker rides only a connection that takes markers", () => {
  const withCache = modelCacheBreakpoint(anthropicConnection(KEY));
  assert.equal(withCache === undefined, !config.promptCache);
  assert.equal(modelCacheBreakpoint(OLLAMA), undefined);
});

test("resolveModelId: the turn's model wins; none falls back to AGENT_MODEL", () => {
  assert.equal(resolveModelId({ model: "claude-haiku-4-5" }), "claude-haiku-4-5");
  assert.equal(resolveModelId(), config.model);
});

interface Captured {
  url: string;
  headers: Record<string, string>;
  body: Record<string, unknown>;
}

/** A fetch that records each request and answers with `respond()`. */
function recorder(respond: () => Response): { calls: Captured[]; fetch: typeof globalThis.fetch } {
  const calls: Captured[] = [];
  const fetch = (async (url: string | URL | Request, init?: RequestInit) => {
    calls.push({
      url: String(url),
      headers: Object.fromEntries(new Headers(init?.headers).entries()),
      body: JSON.parse(String(init?.body)) as Record<string, unknown>,
    });
    return respond();
  }) as typeof globalThis.fetch;
  return { calls, fetch };
}

/** A minimal non-streamed Anthropic message. */
const anthropicMessage = () =>
  new Response(
    JSON.stringify({
      id: "msg_1",
      type: "message",
      role: "assistant",
      model: "claude-sonnet-5",
      content: [{ type: "text", text: "ok" }],
      stop_reason: "end_turn",
      usage: { input_tokens: 1, output_tokens: 1 },
    }),
    { status: 200, headers: { "content-type": "application/json" } },
  );

/** An OpenAI chat-completions stream: one text delta, then a usage chunk. */
const chatStream = () => {
  const data = (obj: unknown) => `data: ${JSON.stringify(obj)}\n\n`;
  const base = { id: "c1", object: "chat.completion.chunk", created: 1, model: "gpt-oss:20b" };
  return new Response(
    data({ ...base, choices: [{ index: 0, delta: { role: "assistant", content: "ok" }, finish_reason: null }] }) +
      data({ ...base, choices: [{ index: 0, delta: {}, finish_reason: "stop" }] }) +
      data({ ...base, choices: [], usage: { prompt_tokens: 12, completion_tokens: 3, total_tokens: 15 } }) +
      "data: [DONE]\n\n",
    { status: 200, headers: { "content-type": "text/event-stream" } },
  );
};

test("anthropicConnection is Anthropic's own API: x-api-key, the resolved model, first-party capabilities", () => {
  const conn = anthropicConnection(KEY, "claude-haiku-4-5");
  assert.equal(conn.format, "anthropic");
  assert.equal(conn.baseURL, "https://api.anthropic.com/v1");
  assert.equal(conn.authScheme, "x-api-key");
  assert.equal(conn.model, "claude-haiku-4-5");
  assert.deepEqual(conn.capabilities, {
    claudeCode: true,
    claudeSubscription: true,
    promptCache: true,
    generatedAgents: true,
    nativePdf: true,
    webSearch: "anthropic-server-tool",
    imageInput: "yes",
  });
  assert.equal(anthropicConnection(KEY).model, config.model);
});

test("connectionFromWire takes the wire's known fields only, beside the key and the model", () => {
  const wire = {
    format: "openai-compatible",
    baseURL: "https://ollama.com/v1",
    authScheme: "bearer",
    outputLimit: 32000,
    capabilities: { ...OLLAMA.capabilities, smuggled: true },
    smuggled: "x",
  } as unknown as TurnConnection;
  const conn = connectionFromWire(wire, OLLAMA_KEY, "gpt-oss:20b");
  assert.deepEqual(Object.keys(conn).sort(), ["apiKey", "authScheme", "baseURL", "capabilities", "format", "model", "outputLimit"]);
  assert.equal("contextWindow" in conn, false);
  assert.deepEqual(conn.capabilities, OLLAMA.capabilities, "capabilities keep their known fields only");
});

// The Anthropic branch sends exactly what the stock provider sends, so no
// request byte (and no cached prefix) differs.
test("createModel on Anthropic's connection sends the stock Anthropic provider's request", async () => {
  const ours = recorder(anthropicMessage);
  const stock = recorder(anthropicMessage);
  const call = { system: "sys", prompt: "hello", maxOutputTokens: 64 };
  await generateText({ model: createModel(anthropicConnection(KEY, "claude-sonnet-5"), { fetch: ours.fetch }), ...call });
  await generateText({ model: createAnthropic({ apiKey: KEY, fetch: stock.fetch })("claude-sonnet-5"), ...call });
  assert.equal(ours.calls.length, 1);
  assert.deepEqual(ours.calls, stock.calls);
  assert.equal(ours.calls[0]!.url, "https://api.anthropic.com/v1/messages");
  assert.equal(ours.calls[0]!.headers["x-api-key"], KEY);
  assert.equal(ours.calls[0]!.headers.authorization, undefined);
});

test("an Anthropic-format bearer connection sends the key as Authorization and no x-api-key", async () => {
  const rec = recorder(anthropicMessage);
  const conn = { ...anthropicConnection(KEY, "gpt-oss:20b"), baseURL: "https://ollama.com/v1", authScheme: "bearer" as const };
  await generateText({ model: createModel(conn, { fetch: rec.fetch }), prompt: "hello" });
  assert.equal(rec.calls[0]!.url, "https://ollama.com/v1/messages");
  assert.equal(rec.calls[0]!.headers.authorization, `Bearer ${KEY}`);
  assert.equal(rec.calls[0]!.headers["x-api-key"], undefined);
});

test("an OpenAI-compatible connection streams chat completions with Bearer auth, usage on, and reasoning_effort", async () => {
  const rec = recorder(chatStream);
  const result = streamText({
    model: createModel(OLLAMA, { fetch: rec.fetch }),
    prompt: "hello",
    maxOutputTokens: maxOutputTokensFor(OLLAMA),
    providerOptions: modelProviderOptions(OLLAMA)!,
  });
  let text = "";
  for await (const chunk of result.textStream) text += chunk;
  assert.equal(text, "ok");
  assert.equal((await result.usage).inputTokens, 12, "the usage chunk reached the turn");
  const { url, headers, body } = rec.calls[0]!;
  assert.equal(url, "https://ollama.com/v1/chat/completions");
  assert.equal(headers.authorization, `Bearer ${OLLAMA_KEY}`);
  assert.equal(headers["x-api-key"], undefined);
  assert.equal(body.model, "gpt-oss:20b");
  assert.deepEqual(body.stream_options, { include_usage: true });
  assert.equal(body.reasoning_effort, config.reasoningEffort);
  assert.equal(body.max_tokens, Math.min(config.maxOutputTokens, 32000));
});

test("createModel sends through the host guard by default", async () => {
  // An IP literal is refused at connect, before any byte leaves.
  for (const conn of [
    { ...anthropicConnection(KEY, "claude-sonnet-5"), baseURL: "https://169.254.169.254/v1" },
    { ...OLLAMA, baseURL: "https://10.0.0.1/v1" },
  ]) {
    await assert.rejects(generateText({ model: createModel(conn), prompt: "hello", maxRetries: 0 }), (err: unknown) => {
      const chain: unknown[] = [];
      for (let e: unknown = err; e; e = (e as { cause?: unknown }).cause) chain.push(e);
      return chain.some((e) => e instanceof HostRefusedError);
    });
  }
});

test("connectionFingerprint is format@host", () => {
  assert.equal(connectionFingerprint(anthropicConnection(KEY)), "anthropic@api.anthropic.com");
  assert.equal(connectionFingerprint({ format: "anthropic", baseURL: "https://ollama.com" }), "anthropic@ollama.com");
  assert.equal(connectionFingerprint(OLLAMA), "openai-compatible@ollama.com");
  assert.equal(connectionFingerprint({ format: "anthropic", baseURL: "https://llm.example:8443/v1" }), "anthropic@llm.example:8443");
});
