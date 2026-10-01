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
 * Unit tests for the MCP client shim against a FAKE JSON-RPC http server (not
 * a fixture for the eval tree — this one is a minimal stand-in so `src/` stays
 * filesystem/eval-free). Covers the failure modes the turn loop relies on being
 * best-effort: a clean list/call round-trip, 401 (expired/short-TTL token), a
 * malformed response, a malformed descriptor, and an `isError:true` result —
 * none of them may throw the turn.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { loadMcpTools } from "../src/shared/mcp-client.js";
import { listen0 } from "../src/shared/listen.js";

type Handler = (req: IncomingMessage, res: ServerResponse, body: unknown) => void;

/** A tiny JSON-RPC http server driven by a per-test handler. */
async function fakeServer(handle: Handler) {
  const server = createServer((req, res) => {
    let raw = "";
    req.on("data", (c: Buffer) => (raw += c));
    req.on("end", () => {
      let body: unknown;
      try {
        body = raw ? JSON.parse(raw) : {};
      } catch {
        body = undefined;
      }
      handle(req, res, body);
    });
  });
  return listen0(server.listen(0));
}

function jsonRpcOk(res: ServerResponse, id: unknown, result: unknown): void {
  res.writeHead(200, { "content-type": "application/json" });
  res.end(JSON.stringify({ jsonrpc: "2.0", id, result }));
}

test("list/call round-trip: discovers tools/list and proxies tools/call, carrying the bearer token", async () => {
  const seenAuth: (string | undefined)[] = [];
  const seenCallArgs: unknown[] = [];
  const { baseUrl, close } = await fakeServer((req, res, body) => {
    seenAuth.push(req.headers.authorization);
    const { id, method, params } = body as { id: unknown; method: string; params: unknown };
    if (method === "tools/list") {
      jsonRpcOk(res, id, {
        tools: [
          {
            name: "list_external_resources",
            description: "list external resources",
            inputSchema: { type: "object", properties: {} },
          },
        ],
      });
    } else if (method === "tools/call") {
      seenCallArgs.push(params);
      jsonRpcOk(res, id, { content: [{ type: "text", text: '{"externalResources":[{"name":"openweather"}]}' }] });
    } else {
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({ jsonrpc: "2.0", id, error: { code: -32601, message: "method not found" } }));
    }
  });
  try {
    const tools = await loadMcpTools({ url: baseUrl, token: "tok-123" });
    assert.deepEqual(Object.keys(tools), ["list_external_resources"]);
    assert.ok(seenAuth.every((a) => a === "Bearer tok-123"), "Authorization header carried the token");

    const result = await tools.list_external_resources!.execute!({}, {} as never);
    assert.match(String(result), /openweather/);
    assert.deepEqual(seenCallArgs, [{ name: "list_external_resources", arguments: {} }]);
  } finally {
    await close();
  }
});

test("401 (expired token) degrades to an empty tool set + a warning, never throws", async () => {
  const { baseUrl, close } = await fakeServer((_req, res) => {
    res.writeHead(401, { "content-type": "application/json" });
    res.end(JSON.stringify({ error: "unauthorized" }));
  });
  const warnings: string[] = [];
  const origWarn = console.warn;
  console.warn = (msg?: unknown) => warnings.push(String(msg));
  try {
    const tools = await loadMcpTools({ url: baseUrl, token: "expired" });
    assert.deepEqual(tools, {});
    assert.ok(warnings.some((w) => /tool discovery failed/.test(w) && /401/.test(w)), warnings.join("\n"));
  } finally {
    console.warn = origWarn;
    await close();
  }
});

test("malformed tools/list response (not valid JSON) degrades to an empty tool set, never throws", async () => {
  const { baseUrl, close } = await fakeServer((_req, res) => {
    res.writeHead(200, { "content-type": "application/json" });
    res.end("not json at all {{{");
  });
  const warnings: string[] = [];
  const origWarn = console.warn;
  console.warn = (msg?: unknown) => warnings.push(String(msg));
  try {
    const tools = await loadMcpTools({ url: baseUrl, token: "tok" });
    assert.deepEqual(tools, {});
    assert.ok(warnings.some((w) => /tool discovery failed/.test(w)), warnings.join("\n"));
  } finally {
    console.warn = origWarn;
    await close();
  }
});

test("malformed tool descriptor (missing name) is skipped, valid siblings still load", async () => {
  const { baseUrl, close } = await fakeServer((_req, res, body) => {
    const { id, method } = body as { id: unknown; method: string };
    if (method === "tools/list") {
      jsonRpcOk(res, id, {
        tools: [{ description: "no name, skip me" }, { name: "list_org_endpoints", description: "ok" }],
      });
    } else {
      jsonRpcOk(res, id, {});
    }
  });
  try {
    const tools = await loadMcpTools({ url: baseUrl, token: "tok" });
    assert.deepEqual(Object.keys(tools), ["list_org_endpoints"]);
  } finally {
    await close();
  }
});

test("isError:true from tools/call throws (surfaces as a tool-error, not a swallowed success)", async () => {
  const { baseUrl, close } = await fakeServer((_req, res, body) => {
    const { id, method } = body as { id: unknown; method: string };
    if (method === "tools/list") {
      jsonRpcOk(res, id, {
        tools: [
          {
            name: "get_external_resource_schema",
            description: "schema lookup",
            inputSchema: { type: "object", properties: { name: { type: "string" } } },
          },
        ],
      });
    } else if (method === "tools/call") {
      jsonRpcOk(res, id, { content: [{ type: "text", text: "missing required argument: name" }], isError: true });
    } else {
      jsonRpcOk(res, id, {});
    }
  });
  try {
    const tools = await loadMcpTools({ url: baseUrl, token: "tok" });
    await assert.rejects(
      tools.get_external_resource_schema!.execute!({}, {} as never),
      /missing required argument: name/,
      "an isError:true result must reject, not resolve with the error text as if it were a normal answer",
    );
  } finally {
    await close();
  }
});

test("server unreachable degrades to an empty tool set, never throws", async () => {
  // Port 1 is never listening — fetch rejects (ECONNREFUSED-equivalent).
  const tools = await loadMcpTools({ url: "http://127.0.0.1:1/mcp", token: "tok" });
  assert.deepEqual(tools, {});
});

test("a server that accepts the request but never answers times out, never hangs", async () => {
  // The one failure "best-effort" did not cover: a connected server that says
  // nothing. `fetch` has no default timeout, so this used to hang forever — and
  // because tools/list runs BEFORE the first model call, the hang stalls
  // time-to-first-token with the SSE stream already open and empty.
  const { baseUrl, close } = await fakeServer(() => {
    /* accept the request and never respond */
  });
  try {
    const started = Date.now();
    const tools = await loadMcpTools({ url: baseUrl, token: "tok" }, { timeoutMs: 150 });
    assert.deepEqual(tools, {}, "a timed-out discovery must degrade to no tools");
    assert.ok(Date.now() - started < 5_000, "must abort at the timeout, not wait on the server");
  } finally {
    await close();
  }
});
