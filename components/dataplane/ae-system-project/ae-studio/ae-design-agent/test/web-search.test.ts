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
 * The turn's `web_search` tool per strategy (`tools/web-search.ts`): the
 * connection's `capabilities.webSearch` picks Anthropic's server tool, a
 * platform-executed tool over Ollama's API, or none — whatever SDK provider
 * the model reports.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { WebSearchError } from "@aep/web-search";
import { buildWebSearchTools, MAX_SEARCHES_PER_TURN, WEB_SEARCH } from "../src/agents/main/tools/web-search.js";
import { anthropicConnection, type ModelCapabilities } from "../src/shared/model.js";

const KEY = "ollama-test-key-0000000000";
const noFetch = (async () => {
  throw new Error("no request expected");
}) as typeof globalThis.fetch;

function ollama(webSearch: ModelCapabilities["webSearch"]) {
  const base = anthropicConnection(KEY, "gpt-oss:20b");
  return { ...base, baseURL: "https://ollama.com/v1", capabilities: { ...base.capabilities, webSearch } };
}

/** What the SDK hands a tool's execute; the Ollama tool reads only the abort signal. */
const OPTIONS = { toolCallId: "s1", messages: [], context: {} };

test("anthropic-server-tool: Anthropic's provider-executed web_search, with the per-turn budget", () => {
  const tools = buildWebSearchTools(anthropicConnection("sk-ant-test"), noFetch);
  const search = tools[WEB_SEARCH] as { type?: string; id?: string; args?: { maxUses?: number } };
  assert.equal(search.type, "provider");
  assert.equal(search.id, "anthropic.web_search_20250305");
  assert.equal(search.args?.maxUses, MAX_SEARCHES_PER_TURN);
});

test("none: no tool at all", () => {
  assert.deepEqual(buildWebSearchTools(ollama("none"), noFetch), {});
});

test("ollama-api: a client tool that searches the connection's host with its key, capped at the per-turn budget", async () => {
  const urls: string[] = [];
  const fetch = (async (url: string | URL | Request) => {
    urls.push(String(url));
    return new Response(JSON.stringify({ results: [{ title: "t", url: "https://e.com", content: "c" }] }), { status: 200 });
  }) as typeof globalThis.fetch;
  const search = buildWebSearchTools(ollama("ollama-api"), fetch)[WEB_SEARCH]!;
  assert.equal(search.type ?? "function", "function");
  const execute = search.execute!;
  for (let i = 0; i < MAX_SEARCHES_PER_TURN; i++) {
    assert.deepEqual(await execute({ query: "q" }, OPTIONS), { results: [{ title: "t", url: "https://e.com", content: "c" }] });
  }
  await assert.rejects(Promise.resolve(execute({ query: "q" }, OPTIONS)), WebSearchError);
  assert.equal(urls.length, MAX_SEARCHES_PER_TURN);
  assert.ok(urls.every((u) => u === "https://ollama.com/api/web_search"));
});
