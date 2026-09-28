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
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { PassThrough } from "node:stream";
import { deniedValuesRule, readAepWebConfig } from "../src/aep-web.js";
import { WEB_SEARCH_TOOL, createWebSearchServer, formatResults, serveStdio } from "../src/mcp.js";
import { WebSearchError, type WebSearchResult } from "../src/index.js";

const SECRET = "sk-staged-secret-value-123456";

function server(search: (q: string) => Promise<WebSearchResult[]> = async () => []) {
  const queries: string[] = [];
  const handle = createWebSearchServer({
    name: "aep-web",
    version: "1.0.0",
    search: async (q) => {
      queries.push(q);
      return search(q);
    },
    deny: deniedValuesRule([SECRET], "WebSearch query blocked: it contains a staged secret value."),
  });
  return { handle, queries };
}

const call = (id: number, query: unknown) => ({
  jsonrpc: "2.0",
  id,
  method: "tools/call",
  params: { name: WEB_SEARCH_TOOL, arguments: { query } },
});

test("initialize echoes a protocol version it speaks, else answers its newest", async () => {
  const { handle } = server();
  const asked = await handle({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2025-03-26" } });
  assert.deepEqual(asked, {
    jsonrpc: "2.0",
    id: 1,
    result: { protocolVersion: "2025-03-26", capabilities: { tools: {} }, serverInfo: { name: "aep-web", version: "1.0.0" } },
  });
  const unknown = (await handle({ jsonrpc: "2.0", id: 2, method: "initialize", params: { protocolVersion: "1999-01-01" } })) as {
    result: { protocolVersion: string };
  };
  assert.equal(unknown.result.protocolVersion, "2025-06-18");
});

test("tools/list offers the one tool, and a notification is never answered", async () => {
  const { handle } = server();
  const listed = (await handle({ jsonrpc: "2.0", id: 3, method: "tools/list" })) as {
    result: { tools: { name: string; inputSchema: { required: string[] } }[] };
  };
  assert.deepEqual(
    listed.result.tools.map((t) => [t.name, t.inputSchema.required]),
    [[WEB_SEARCH_TOOL, ["query"]]],
  );
  assert.equal(await handle({ jsonrpc: "2.0", method: "notifications/initialized" }), undefined);
  assert.deepEqual(await handle({ jsonrpc: "2.0", id: 4, method: "resources/list" }), {
    jsonrpc: "2.0",
    id: 4,
    error: { code: -32601, message: "method not found: resources/list" },
  });
});

test("a query is searched and its results come back as text", async () => {
  const { handle, queries } = server(async () => [{ title: "Stripe", url: "https://stripe.com/docs", content: "Payment intents" }]);
  const res = (await handle(call(5, "stripe payment intents"))) as { result: { content: { text: string }[]; isError?: boolean } };
  assert.deepEqual(queries, ["stripe payment intents"]);
  assert.equal(res.result.isError, undefined);
  assert.match(res.result.content[0]?.text ?? "", /Stripe\nhttps:\/\/stripe\.com\/docs\n\nPayment intents/);
});

test("a query holding a staged secret is refused before it is sent", async () => {
  const { handle, queries } = server();
  const res = (await handle(call(6, `how do I call the API with ${SECRET}`))) as {
    result: { content: { text: string }[]; isError: boolean };
  };
  assert.equal(res.result.isError, true);
  assert.match(res.result.content[0]?.text ?? "", /staged secret/);
  assert.deepEqual(queries, [], "the refused query was searched");
});

test("a search failure is the tool's error, and only a WebSearchError's words reach the model", async () => {
  const failing = server(async () => {
    throw new WebSearchError("web search failed: ollama.com answered 429", 429);
  });
  const res = (await failing.handle(call(7, "q"))) as { result: { content: { text: string }[]; isError: boolean } };
  assert.deepEqual(res.result, { content: [{ type: "text", text: "web search failed: ollama.com answered 429" }], isError: true });

  const leaking = server(async () => {
    throw new Error(`socket closed with ${SECRET}`);
  });
  const other = (await leaking.handle(call(8, "q"))) as { result: { content: { text: string }[] } };
  assert.doesNotMatch(other.result.content[0]?.text ?? "", new RegExp(SECRET));
});

test("formatResults says so when there are none", () => {
  assert.equal(formatResults("q", []), 'No web results for "q".');
});

test("serveStdio answers one line per request and nothing for a notification", async () => {
  const { handle } = server();
  const input = new PassThrough();
  const output = new PassThrough();
  const written: string[] = [];
  output.on("data", (chunk: Buffer) => written.push(chunk.toString("utf8")));
  const served = serveStdio(handle, input, output);
  input.write(JSON.stringify({ jsonrpc: "2.0", id: 1, method: "ping" }) + "\n");
  input.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\nnot json\n");
  input.end();
  await served;
  // Answered concurrently (a slow search must not hold up a ping), so matched
  // by id, as JSON-RPC does, and not by order.
  const lines = written.join("").trim().split("\n").sort().map((l) => JSON.parse(l) as { id: unknown });
  assert.deepEqual(lines, [
    { jsonrpc: "2.0", id: 1, result: {} },
    { jsonrpc: "2.0", id: null, error: { code: -32700, message: "parse error" } },
  ]);
});

test("readAepWebConfig refuses a config with no key, and never quotes the file", () => {
  const dir = mkdtempSync(join(tmpdir(), "aep-web-"));
  const good = join(dir, "good.json");
  writeFileSync(good, JSON.stringify({ baseURL: "https://ollama.com/v1", apiKey: "k-0123456789ab", deniedValues: [], denialMessage: "no" }));
  assert.equal(readAepWebConfig(good).baseURL, "https://ollama.com/v1");

  const keyless = join(dir, "keyless.json");
  writeFileSync(keyless, JSON.stringify({ baseURL: "https://ollama.com/v1", apiKey: "", deniedValues: [], denialMessage: "no" }));
  assert.throws(() => readAepWebConfig(keyless), /is not an aep-web config/);

  const broken = join(dir, "broken.json");
  writeFileSync(broken, `{"apiKey": "${SECRET}",`);
  assert.throws(
    () => readAepWebConfig(broken),
    (err: Error) => !err.message.includes(SECRET) && /not readable JSON/.test(err.message),
  );
});
