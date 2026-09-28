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

import { test } from "node:test";
import assert from "node:assert/strict";
import { MAX_CONTENT_CHARS, MAX_RESULTS, WebSearchError, searchOllama } from "../src/index.js";

const KEY = "ollama-test-key-0000000000";

interface Captured {
  url: string;
  init: RequestInit;
}

function stubFetch(respond: () => Response): { calls: Captured[]; fetch: typeof globalThis.fetch } {
  const calls: Captured[] = [];
  const fetch = (async (url: string | URL | Request, init?: RequestInit) => {
    calls.push({ url: String(url), init: init ?? {} });
    return respond();
  }) as typeof globalThis.fetch;
  return { calls, fetch };
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

test("searches the connection's own host with its key as Bearer, refusing redirects", async () => {
  const stub = stubFetch(() => json({ results: [{ title: "T", url: "https://example.com", content: "C" }] }));
  const results = await searchOllama("  stripe payment intents  ", {
    baseURL: "https://ollama.com/v1",
    apiKey: KEY,
    fetch: stub.fetch,
  });
  assert.deepEqual(results, [{ title: "T", url: "https://example.com", content: "C" }]);
  assert.equal(stub.calls.length, 1);
  const { url, init } = stub.calls[0]!;
  assert.equal(url, "https://ollama.com/api/web_search");
  assert.equal(init.method, "POST");
  assert.equal(init.redirect, "error");
  assert.equal((init.headers as Record<string, string>).authorization, `Bearer ${KEY}`);
  assert.deepEqual(JSON.parse(String(init.body)), { query: "stripe payment intents", max_results: MAX_RESULTS });
});

test("the Anthropic-format base URL on the same host reaches the same endpoint", async () => {
  const stub = stubFetch(() => json({ results: [] }));
  await searchOllama("q", { baseURL: "https://ollama.com", apiKey: KEY, fetch: stub.fetch });
  assert.equal(stub.calls[0]!.url, "https://ollama.com/api/web_search");
});

test("caps each result's content and the result count", async () => {
  const long = "x".repeat(MAX_CONTENT_CHARS + 500);
  const many = Array.from({ length: MAX_RESULTS + 3 }, (_, i) => ({ title: `t${i}`, url: `https://e.com/${i}`, content: long }));
  const results = await searchOllama("q", { baseURL: "https://ollama.com/v1", apiKey: KEY, fetch: stubFetch(() => json({ results: many })).fetch });
  assert.equal(results.length, MAX_RESULTS);
  for (const r of results) assert.equal(r.content.length, MAX_CONTENT_CHARS + 1); // + the ellipsis
});

test("skips results with no URL rather than handing the model an uncitable one", async () => {
  const body = { results: [{ title: "no url", content: "c" }, { title: "ok", url: "https://e.com", content: 3 }] };
  const results = await searchOllama("q", { baseURL: "https://ollama.com/v1", apiKey: KEY, fetch: stubFetch(() => json(body)).fetch });
  assert.deepEqual(results, [{ title: "ok", url: "https://e.com", content: "" }]);
});

test("a non-2xx answer names the host and status, never the key or the body", async () => {
  const stub = stubFetch(() => new Response(`denied for ${KEY}`, { status: 401 }));
  await assert.rejects(searchOllama("q", { baseURL: "https://ollama.com/v1", apiKey: KEY, fetch: stub.fetch }), (err: unknown) => {
    assert.ok(err instanceof WebSearchError);
    assert.equal(err.status, 401);
    assert.equal(err.message, "web search failed: ollama.com answered 401");
    assert.ok(!err.message.includes(KEY));
    return true;
  });
});

test("a malformed answer and an unreachable host are search errors", async () => {
  const garbage = stubFetch(() => json({ nope: true }));
  await assert.rejects(searchOllama("q", { baseURL: "https://ollama.com/v1", apiKey: KEY, fetch: garbage.fetch }), WebSearchError);
  const down = (async () => {
    throw new TypeError("fetch failed");
  }) as typeof globalThis.fetch;
  await assert.rejects(searchOllama("q", { baseURL: "https://ollama.com/v1", apiKey: KEY, fetch: down }), /could not reach ollama.com/);
});

test("an empty query is refused before any request", async () => {
  const stub = stubFetch(() => json({ results: [] }));
  await assert.rejects(searchOllama("   ", { baseURL: "https://ollama.com/v1", apiKey: KEY, fetch: stub.fetch }), WebSearchError);
  assert.equal(stub.calls.length, 0);
});
