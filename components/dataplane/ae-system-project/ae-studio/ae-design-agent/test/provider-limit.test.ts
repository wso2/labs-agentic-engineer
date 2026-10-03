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
 * Provider limits (`shared/provider-limit.ts`): the pure wait-or-limit rule on
 * a fake clock, the headers it reads, the one `model_provider_429` line per
 * 429 (scrubbed, capped), and a stub provider answering `429 retry-after: 600`
 * ending a real `/v1` turn with ONE `provider_limit` frame, a `turn-failed`
 * naming it, and no retry.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { APICallError, RetryError, streamText } from "ai";
import {
  MODEL_MAX_RETRIES,
  PROVIDER_LIMIT_AFTER_MS,
  ProviderLimitError,
  providerLimit,
  providerLimitIn,
  resetAtOf,
  retryAfterMs,
  watchProviderLimits,
  type ProviderLimitLogLine,
} from "../src/shared/provider-limit.js";
import { createModel, type ModelConnection } from "../src/shared/model.js";
import { ORG_ID, startEdge, startTurn, streamOf } from "./helpers/edge.js";

const KEY = "ollama-test-key-0123456789abcdef";
const MIN = 60_000;
const NOW = Date.parse("2026-09-26T12:00:00Z");

test("providerLimit: short stated waits retry; a long one, or five minutes of 429s, is a provider limit", () => {
  const table: Array<[retryAfterMs: number | undefined, waitedMs: number, verdict: string]> = [
    [20_000, 0, "wait"],
    [60_000, 4 * MIN, "wait"],
    [PROVIDER_LIMIT_AFTER_MS, 0, "wait"], // exactly five minutes is still a wait
    [PROVIDER_LIMIT_AFTER_MS + 1, 0, "provider_limit"],
    [600_000, 0, "provider_limit"],
    [20_000, 5 * MIN, "provider_limit"], // short waits that add up
    [undefined, 0, "wait"], // no retry-after at all: back off
    [undefined, 5 * MIN - 1, "wait"],
    [undefined, 5 * MIN, "provider_limit"], // a provider that states no reset
  ];
  for (const [after, waited, verdict] of table) {
    assert.equal(providerLimit(after, waited), verdict, `retryAfterMs=${after} waitedMs=${waited}`);
  }
});

test("retryAfterMs reads retry-after-ms, then retry-after in seconds or as an HTTP date", () => {
  const h = (init: Record<string, string>) => new Headers(init);
  assert.equal(retryAfterMs(h({ "retry-after-ms": "1500" }), NOW), 1500);
  assert.equal(retryAfterMs(h({ "retry-after": "20" }), NOW), 20_000);
  assert.equal(retryAfterMs(h({ "retry-after": new Date(NOW + 90_000).toUTCString() }), NOW), 90_000);
  assert.equal(retryAfterMs(h({ "retry-after": "soon" }), NOW), undefined);
  assert.equal(retryAfterMs(h({}), NOW), undefined);
});

test("resetAtOf: the stated wait, else the latest rate-limit reset header in any of its spellings", () => {
  const h = (init: Record<string, string>) => new Headers(init);
  assert.equal(resetAtOf(h({ "retry-after": "600" }), NOW)?.toISOString(), "2026-09-26T12:10:00.000Z");
  assert.equal(resetAtOf(h({ "x-ratelimit-reset-requests": "30", "x-ratelimit-reset-tokens": "120" }), NOW)?.toISOString(), "2026-09-26T12:02:00.000Z");
  assert.equal(resetAtOf(h({ "x-ratelimit-reset": String(NOW / 1000 + 3600) }), NOW)?.toISOString(), "2026-09-26T13:00:00.000Z");
  assert.equal(resetAtOf(h({ "ratelimit-reset": "2026-09-26T14:05:00Z" }), NOW)?.toISOString(), "2026-09-26T14:05:00.000Z");
  assert.equal(resetAtOf(h({ "content-type": "application/json" }), NOW), undefined);
});

/** A base fetch answering every call with a 429 carrying `headers` and `body`. */
function always429(headers: Record<string, string>, body: string): { calls: number; fetch: typeof globalThis.fetch } {
  const state = { calls: 0, fetch: (async () => {
    state.calls += 1;
    return new Response(body, { status: 429, headers });
  }) as typeof globalThis.fetch };
  return state;
}

test("watchProviderLimits on a fake clock: 20s waits pass back to the SDK until five minutes of them, then it stops", async () => {
  let now = NOW;
  const lines: ProviderLimitLogLine[] = [];
  const waits: string[] = [];
  const base = always429({ "retry-after": "20", "x-ratelimit-remaining": "0", "content-type": "application/json" }, "{}");
  const fetch = watchProviderLimits(base.fetch, {
    apiKey: KEY,
    host: "ollama.com",
    format: "openai-compatible",
    model: "gpt-oss:20b",
    org: "org-1",
    log: (l) => lines.push(l),
    now: () => now,
    onWait: (host) => waits.push(host),
  });
  let stopped: unknown;
  for (let i = 0; i < 100 && stopped === undefined; i++) {
    try {
      const res = await fetch("https://ollama.com/v1/chat/completions");
      assert.equal(res.status, 429, "a wait hands the 429 back for the SDK to retry");
      now += 20_000;
    } catch (err) {
      stopped = err;
    }
  }
  assert.ok(stopped instanceof ProviderLimitError);
  assert.equal(stopped.host, "ollama.com");
  // 0s, 20s, … 300s: the sixteenth 429 is the one five minutes after the first.
  assert.equal(base.calls, 16);
  assert.equal(lines.length, base.calls, "one line per 429");
  assert.deepEqual(lines.map((l) => l.verdict), [...Array(15).fill("wait"), "provider_limit"]);
  assert.equal(lines[15]!.waitedMs, 5 * MIN);
  assert.deepEqual(lines[0]!.limitHeaders, { "retry-after": "20", "x-ratelimit-remaining": "0" });
  assert.equal(lines[0]!.org, "org-1");
  assert.deepEqual(waits, Array(15).fill("ollama.com"), "every wait is announced, the provider limit is not");
});

test("any other response ends a streak: a short 429 long before does not count toward the five minutes", async () => {
  let now = NOW;
  const lines: ProviderLimitLogLine[] = [];
  const answers = [429, 200, 429];
  const base = (async () =>
    new Response("{}", { status: answers.shift()!, headers: { "retry-after": "2" } })) as typeof globalThis.fetch;
  const fetch = watchProviderLimits(base, {
    apiKey: KEY,
    host: "ollama.com",
    format: "openai-compatible",
    model: "gpt-oss:20b",
    log: (l) => lines.push(l),
    now: () => now,
  });
  assert.equal((await fetch("https://ollama.com/v1/chat/completions")).status, 429);
  now += MIN;
  assert.equal((await fetch("https://ollama.com/v1/chat/completions")).status, 200);
  now += 6 * MIN;
  assert.equal((await fetch("https://ollama.com/v1/chat/completions")).status, 429, "a new streak is a wait");
  assert.deepEqual(lines.map((l) => [l.verdict, l.waitedMs]), [["wait", 0], ["wait", 0]]);
});

test("a 429's logged body is scrubbed of the key before it is cut to 300 characters", async () => {
  const lines: ProviderLimitLogLine[] = [];
  const body = `{"error":{"type":"rate_limit_error","message":"plan spent for key ${KEY}"}}${"x".repeat(400)}`;
  const fetch = watchProviderLimits(always429({ "retry-after": "600" }, body).fetch, {
    apiKey: KEY,
    host: "ollama.com",
    format: "openai-compatible",
    model: "gpt-oss:20b",
    log: (l) => lines.push(l),
    now: () => NOW,
  });
  await assert.rejects(fetch("https://ollama.com/v1/chat/completions"), (err: unknown) => {
    assert.ok(err instanceof ProviderLimitError);
    assert.equal(err.resetAt?.toISOString(), "2026-09-26T12:10:00.000Z");
    return true;
  });
  assert.equal(lines.length, 1);
  assert.equal(lines[0]!.body.length, 300);
  assert.ok(!JSON.stringify(lines).includes(KEY), "no key literal in the log line");
  assert.match(lines[0]!.body, /plan spent for key <redacted>/);
  assert.equal(lines[0]!.verdict, "provider_limit");
});

test("providerLimitIn finds the limit inside the SDK's RetryError, and names 429 retries that ran out", () => {
  const limit = new ProviderLimitError("ollama.com", new Date(NOW));
  const wrapped = new RetryError({ message: "failed", reason: "errorNotRetryable", errors: [new Error("earlier"), limit] });
  assert.equal(providerLimitIn(wrapped, "ollama.com"), limit);
  const rateLimited = (status: number) =>
    new APICallError({ message: "rate limited", url: "https://ollama.com/v1/chat/completions", requestBodyValues: {}, statusCode: status, isRetryable: true });
  const exhausted = new RetryError({ message: "failed", reason: "maxRetriesExceeded", errors: [rateLimited(429), rateLimited(429)] });
  const found = providerLimitIn(exhausted, "ollama.com");
  assert.ok(found instanceof ProviderLimitError);
  assert.equal(found.resetAt, undefined);
  const overloaded = new RetryError({ message: "failed", reason: "maxRetriesExceeded", errors: [rateLimited(529)] });
  assert.equal(providerLimitIn(overloaded, "ollama.com"), undefined);
  assert.equal(providerLimitIn(new Error("boom"), "ollama.com"), undefined);
});

test("a short wait is retried by the SDK and the turn goes on, with one wait line", async () => {
  const lines: ProviderLimitLogLine[] = [];
  let calls = 0;
  const base = (async () => {
    calls += 1;
    if (calls === 1) return new Response("{}", { status: 429, headers: { "retry-after": "0.05" } });
    const data = (o: unknown) => `data: ${JSON.stringify(o)}\n\n`;
    const chunk = { id: "c1", object: "chat.completion.chunk", created: 1, model: "gpt-oss:20b" };
    return new Response(
      data({ ...chunk, choices: [{ index: 0, delta: { content: "ok" }, finish_reason: "stop" }] }) + "data: [DONE]\n\n",
      { status: 200, headers: { "content-type": "text/event-stream" } },
    );
  }) as typeof globalThis.fetch;
  const conn = {
    format: "openai-compatible" as const,
    baseURL: "https://ollama.com/v1",
    authScheme: "bearer" as const,
    capabilities: { claudeCode: false, claudeSubscription: false, promptCache: false, generatedAgents: false, nativePdf: false, webSearch: "none" as const, imageInput: "unknown" as const },
    apiKey: KEY,
    model: "gpt-oss:20b",
  };
  const result = streamText({ model: createModel(conn, { fetch: base, providerLimitLog: (l) => lines.push(l) }), prompt: "hi", maxRetries: MODEL_MAX_RETRIES });
  let text = "";
  for await (const t of result.textStream) text += t;
  assert.equal(text, "ok");
  assert.equal(calls, 2);
  assert.deepEqual(lines.map((l) => l.verdict), ["wait"]);
});

// --- A real turn against a stub provider ---------------------------------------

/** How the stub provider answers its `n`th request (1-based). */
type StubAnswer = { status: number; headers: Record<string, string>; body: string };

/** An OpenAI-compatible streamed completion answering "ok". */
function okCompletion(): StubAnswer {
  const data = (o: unknown) => `data: ${JSON.stringify(o)}\n\n`;
  const chunk = { id: "c1", object: "chat.completion.chunk", created: 1, model: "gpt-oss:20b" };
  return {
    status: 200,
    headers: { "content-type": "text/event-stream" },
    body: data({ ...chunk, choices: [{ index: 0, delta: { content: "ok" }, finish_reason: "stop" }] }) + "data: [DONE]\n\n",
  };
}

/** The org's connection: a public host whose requests the test carries to the stub. */
const CONN: ModelConnection = {
  format: "openai-compatible",
  baseURL: "https://llm.example/v1",
  authScheme: "bearer",
  capabilities: { claudeCode: false, claudeSubscription: false, promptCache: false, generatedAgents: false, nativePdf: false, webSearch: "none", imageInput: "unknown" },
  apiKey: KEY,
  model: "gpt-oss:20b",
};

/**
 * One real turn through the `/v1` edge, its model reaching a stub provider
 * that answers each request with `answer(n)`. Returns the stream's frames and
 * raw text, how many requests the provider saw, and the 429 lines logged.
 */
async function runStubTurn(answer: (n: number) => StubAnswer): Promise<{
  frames: Array<Record<string, unknown>>;
  sse: string;
  requests: number;
  lines: ProviderLimitLogLine[];
}> {
  let requests = 0;
  const provider: Server = createServer((_req, res) => {
    requests += 1;
    const a = answer(requests);
    res.writeHead(a.status, a.headers);
    res.end(a.body);
  });
  await new Promise<void>((r) => provider.listen(0, "127.0.0.1", r));
  const providerBase = `http://127.0.0.1:${(provider.address() as AddressInfo).port}`;
  const lines: ProviderLimitLogLine[] = [];
  const edge = await startEdge({
    connection: CONN,
    // The connection names a public host; the test's fetch carries its
    // requests to the stub instead, beneath the 429 watch.
    buildModel: (conn, ctx) =>
      createModel(conn, {
        ...ctx,
        fetch: (url, init) => globalThis.fetch(String(url).replace("https://llm.example", providerBase), init),
        providerLimitLog: (l) => lines.push(l),
      }),
  });
  try {
    const tok = await edge.token();
    const res = await startTurn(edge, tok, { instruction: "hello" });
    assert.equal(res.status, 202);
    const { turnId } = (await res.json()) as { turnId: string };
    const stream = await streamOf(edge, tok, turnId);
    const frames = stream.filter((f) => f.raw !== "[DONE]").map((f) => f.data as Record<string, unknown>);
    return { frames, sse: stream.map((f) => f.raw).join("\n"), requests, lines };
  } finally {
    await edge.close();
    await new Promise<void>((r) => provider.close(() => r()));
  }
}

test("a stub provider answering 429 retry-after: 600 ends the turn with one provider_limit frame and one log line", async () => {
  const { frames, sse, requests, lines } = await runStubTurn(() => ({
    status: 429,
    headers: { "content-type": "application/json", "retry-after": "600" },
    body: JSON.stringify({ error: { message: `usage limit reached for ${KEY}` } }),
  }));
  const errors = frames.filter((f) => f.type === "error");
  assert.equal(errors.length, 1, `exactly one error frame, got ${JSON.stringify(errors)}`);
  assert.equal(errors[0]!.code, "provider_limit");
  assert.equal(errors[0]!.host, "llm.example");
  const resetAt = Date.parse(String(errors[0]!.resetAt));
  assert.ok(Math.abs(resetAt - (Date.now() + 600_000)) < 30_000, "resetAt is the stated wait from now");
  assert.match(String(errors[0]!.error), /^llm\.example's usage limit is reached\. Try again after /);
  const end = frames.at(-1)!;
  assert.deepEqual(
    { type: end.type, reason: end.reason, code: end.code, host: end.host, resetAt: end.resetAt, message: end.message },
    { type: "turn-failed", reason: "agent-error", code: "provider_limit", host: "llm.example", resetAt: errors[0]!.resetAt, message: errors[0]!.error },
    "the stream ends turn-failed naming the limit",
  );
  assert.equal(frames.some((f) => f.type === "provider-wait"), false, "a provider limit is not a wait");
  assert.equal(requests, 1, "a provider limit is not retried");
  assert.equal(lines.length, 1, "exactly one model_provider_429 line");
  assert.equal(lines[0]!.msg, "model_provider_429");
  assert.equal(lines[0]!.source, "agents");
  assert.equal(lines[0]!.org, ORG_ID);
  assert.equal(lines[0]!.host, "llm.example");
  assert.equal(lines[0]!.format, "openai-compatible");
  assert.equal(lines[0]!.model, "gpt-oss:20b");
  assert.equal(lines[0]!.status, 429);
  assert.deepEqual(lines[0]!.limitHeaders, { "retry-after": "600" });
  assert.equal(lines[0]!.verdict, "provider_limit");
  assert.ok(!JSON.stringify(lines).includes(KEY), "no key literal in the log line");
  assert.ok(!sse.includes(KEY), "no key literal on the stream");
});

test("a short 429 wait streams one provider-wait frame, and the model's answer follows it", async () => {
  const { frames, requests } = await runStubTurn((n) =>
    n === 1 ? { status: 429, headers: { "retry-after": "0.05" }, body: "{}" } : okCompletion(),
  );
  assert.equal(requests, 2, "the wait was retried");
  const waits = frames.filter((f) => f.type === "provider-wait");
  assert.deepEqual(waits, [{ type: "provider-wait", host: "llm.example" }]);
  const types = frames.map((f) => f.type);
  const answered = types.indexOf("text-delta");
  assert.ok(answered > 0, "the model's answer follows");
  assert.ok(types.indexOf("provider-wait") < types.indexOf("start-step"), `the wait is said before the model answers: ${types.join(",")}`);
  assert.equal(frames.at(-1)?.type, "turn-completed", "the turn completes");
});

test("a turn the provider answers first time streams no provider-wait frame", async () => {
  const { frames, requests } = await runStubTurn(() => okCompletion());
  assert.equal(requests, 1);
  assert.equal(frames.some((f) => f.type === "provider-wait"), false);
  assert.equal(frames.at(-1)?.type, "turn-completed");
});
