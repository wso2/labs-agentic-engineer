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
 * The Issues chat's `classify_report` tool (`agents/issues/classify.ts`): one
 * choice question to the Jev classifier. Every failure degrades to
 * "unknown, ask" — a classifier outage never fails a turn.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { classifyReport, buildClassifyReportTool, CLASSIFY_REPORT, type JevOptions } from "../src/agents/issues/classify.js";

const KEY = "jev-secret-key-123";
const URL = "https://jev.test/v1/systemone";
const UNKNOWN = { kind: "unknown", confidence: 0, alternatives: [], needsClarification: true };

function answer(choice: string, confidence: number, probabilities?: Record<string, number>) {
  return {
    model: "jev-1.13.0",
    answers: {
      kind: {
        type: "choice",
        choice,
        confidence,
        probabilities: probabilities ?? { bug: 0, feature: 0, improvement: 0, question: 0, [choice]: confidence },
      },
    },
  };
}

function stub(body: unknown, status = 200) {
  const calls: { url: string; init: RequestInit }[] = [];
  const fetch = (async (url: string | URL | Request, init?: RequestInit) => {
    calls.push({ url: String(url), init: init ?? {} });
    return new Response(JSON.stringify(body), { status });
  }) as typeof globalThis.fetch;
  return { fetch, calls };
}

function opts(fetch: typeof globalThis.fetch, extra: Partial<JevOptions> = {}): JevOptions {
  return { apiKey: KEY, url: URL, fetch, ...extra };
}

test("sends one choice question to jev-latest", async () => {
  const { fetch, calls } = stub(answer("bug", 1));
  await classifyReport(opts(fetch), { message: "the save button does nothing", recentMessages: ["hi"] });
  assert.equal(calls.length, 1);
  assert.equal(calls[0]!.url, URL);
  assert.equal(calls[0]!.init.method, "POST");
  const headers = new Headers(calls[0]!.init.headers);
  assert.equal(headers.get("authorization"), `Bearer ${KEY}`);
  const body = JSON.parse(String(calls[0]!.init.body));
  assert.equal(body.model, "jev-latest");
  assert.equal(body.state.message, "the save button does nothing");
  assert.deepEqual(body.state.recent_messages, ["hi"]);
  assert.equal(body.state.where_the_user_is.page, "issues");
  assert.equal(body.questions.kind.type, "choice");
  assert.deepEqual(Object.keys(body.questions.kind.criteria), ["bug", "feature", "improvement", "question"]);
});

test("a confident bug files without clarification", async () => {
  const { fetch } = stub(answer("bug", 1, { bug: 1, feature: 0, improvement: 0, question: 0 }));
  const r = await classifyReport(opts(fetch), { message: "m" });
  assert.equal(r.kind, "bug");
  assert.equal(r.confidence, 1);
  assert.equal(r.needsClarification, false);
  assert.equal(r.alternatives.length, 2);
  assert.deepEqual(r.alternatives[0], { kind: "bug", p: 1 });
});

test("below 0.8 asks", async () => {
  const { fetch } = stub(answer("improvement", 0.75, { bug: 0.2, feature: 0.05, improvement: 0.75, question: 0 }));
  const r = await classifyReport(opts(fetch), { message: "m" });
  assert.equal(r.kind, "improvement");
  assert.equal(r.needsClarification, true);
  assert.deepEqual(r.alternatives, [
    { kind: "improvement", p: 0.75 },
    { kind: "bug", p: 0.2 },
  ]);
});

test("exactly 0.8 does not ask", async () => {
  const { fetch } = stub(answer("feature", 0.8));
  const r = await classifyReport(opts(fetch), { message: "m" });
  assert.equal(r.needsClarification, false);
});

test("a question never files", async () => {
  const { fetch } = stub(answer("question", 0.97));
  const r = await classifyReport(opts(fetch), { message: "m" });
  assert.equal(r.kind, "question");
  assert.equal(r.needsClarification, true);
});

test("no key -> unknown, no request", async () => {
  const fetch = (async () => {
    throw new Error("no request expected");
  }) as typeof globalThis.fetch;
  assert.deepEqual(await classifyReport({ apiKey: undefined, url: URL, fetch }, { message: "m" }), UNKNOWN);
});

test("401 -> unknown", async () => {
  const { fetch } = stub({ error: "unauthorized" }, 401);
  assert.deepEqual(await classifyReport(opts(fetch), { message: "m" }), UNKNOWN);
});

test("malformed body (no answers.kind) -> unknown", async () => {
  const { fetch } = stub({ model: "jev-1.13.0", answers: {} });
  assert.deepEqual(await classifyReport(opts(fetch), { message: "m" }), UNKNOWN);
});

test("an unrecognised choice -> unknown", async () => {
  const { fetch } = stub(answer("chore", 0.99));
  assert.deepEqual(await classifyReport(opts(fetch), { message: "m" }), UNKNOWN);
});

test("fetch rejects -> unknown", async () => {
  const fetch = (async () => {
    throw new TypeError("network down");
  }) as typeof globalThis.fetch;
  assert.deepEqual(await classifyReport(opts(fetch), { message: "m" }), UNKNOWN);
});

test("timeout -> unknown", async () => {
  const fetch = ((_url: string | URL | Request, init?: RequestInit) =>
    new Promise((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(init.signal?.reason));
    })) as typeof globalThis.fetch;
  assert.deepEqual(await classifyReport(opts(fetch, { timeoutMs: 20 }), { message: "m" }), UNKNOWN);
});

test("the key never reaches the tool result", async () => {
  const { fetch } = stub(answer("bug", 0.9));
  const r = await classifyReport(opts(fetch), { message: "m" });
  assert.ok(!JSON.stringify(r).includes(KEY));
  const failed = await classifyReport(opts(stub({}, 401).fetch), { message: "m" });
  assert.ok(!JSON.stringify(failed).includes(KEY));
});

test("the tool classifies through the same path and validates its input", async () => {
  const { fetch, calls } = stub(answer("bug", 1));
  const t = buildClassifyReportTool(opts(fetch));
  assert.equal(CLASSIFY_REPORT, "classify_report");
  const out = await t.execute!({ message: "it crashes" }, { toolCallId: "c1", messages: [], context: {} });
  assert.equal((out as { kind: string }).kind, "bug");
  assert.equal(JSON.parse(String(calls[0]!.init.body)).state.message, "it crashes");
  const schema = t.inputSchema as { safeParse: (v: unknown) => { success: boolean } };
  assert.equal(schema.safeParse({ message: "" }).success, false);
  assert.equal(schema.safeParse({ message: "x", recentMessages: Array(7).fill("a") }).success, false);
});
