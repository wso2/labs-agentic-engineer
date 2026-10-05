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
import http from "node:http";
import type { AddressInfo } from "node:net";
import { startMcpAuthProxy } from "./mcp_auth_proxy.js";
import { interceptJsonRpc, type LocalMcpTools } from "./mcp_local_tools.js";

test("tools/call for a local tool never reaches upstream; tools/list gains the local descriptors", async () => {
  let upstreamCalls = 0;
  const upstream = http.createServer((req, res) => {
    upstreamCalls++;
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ jsonrpc: "2.0", id: 1, result: { tools: [{ name: "list_org_component_endpoints" }] } }));
  });
  await new Promise<void>((r) => upstream.listen(0, "127.0.0.1", () => r()));
  const up = `http://127.0.0.1:${(upstream.address() as AddressInfo).port}/`;
  const proxy = await startMcpAuthProxy({
    upstreamUrl: up,
    source: { getToken: async () => "pub", invalidate: () => {} },
    canRefresh: false,
    onFatal: () => {},
    local: {
      descriptors: [{ name: "get_remote_git_file_contents", description: "d", inputSchema: { type: "object" } }],
      call: (name) => (name === "get_remote_git_file_contents" ? Promise.resolve({ content: [{ type: "text", text: "{}" }] }) : undefined),
    },
  });
  type Reply = { result: { tools: { name: string }[] } };
  const post = (body: unknown) =>
    fetch(proxy.url, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) }).then(
      (r) => r.json() as Promise<Reply>,
    );

  const call = await post({ jsonrpc: "2.0", id: 7, method: "tools/call", params: { name: "get_remote_git_file_contents", arguments: {} } });
  assert.deepEqual(call, { jsonrpc: "2.0", id: 7, result: { content: [{ type: "text", text: "{}" }] } });
  assert.equal(upstreamCalls, 0);

  const list = await post({ jsonrpc: "2.0", id: 1, method: "tools/list" });
  assert.deepEqual(list.result.tools.map((t: { name: string }) => t.name), ["list_org_component_endpoints", "get_remote_git_file_contents"]);
  assert.equal(upstreamCalls, 1);
  await proxy.close();
  upstream.close();
});

// ---- interceptJsonRpc: what is answered here and what goes upstream ----

const local: LocalMcpTools = {
  descriptors: [{ name: "mine", description: "d", inputSchema: { type: "object" } }],
  call: (name, args) =>
    name === "mine" ? Promise.resolve({ content: [{ type: "text", text: JSON.stringify(args) }] }) : undefined,
};
const body = (v: unknown) => Buffer.from(JSON.stringify(v));

test("a call to a tool upstream owns is forwarded", async () => {
  const d = await interceptJsonRpc(body({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "list_org_component_endpoints" } }), local);
  assert.deepEqual(d, { kind: "forward" });
});

test("a local call carries its arguments and its id", async () => {
  const d = await interceptJsonRpc(body({ jsonrpc: "2.0", id: "a", method: "tools/call", params: { name: "mine", arguments: { x: 1 } } }), local);
  assert.deepEqual(d, { kind: "answer", payload: { jsonrpc: "2.0", id: "a", result: { content: [{ type: "text", text: '{"x":1}' }] } } });
});

test("a local tool that throws answers a tool error, never upstream", async () => {
  const throwing: LocalMcpTools = { descriptors: [], call: () => Promise.reject(new Error("boom")) };
  const d = await interceptJsonRpc(body({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "mine" } }), throwing);
  assert.deepEqual(d, { kind: "answer", payload: { jsonrpc: "2.0", id: 2, result: { content: [{ type: "text", text: "boom" }], isError: true } } });
});

test("tools/list is forwarded for a merge; batches, other methods and junk forward unchanged", async () => {
  assert.deepEqual(await interceptJsonRpc(body({ jsonrpc: "2.0", id: 1, method: "tools/list" }), local), { kind: "forward-and-merge-list" });
  assert.deepEqual(await interceptJsonRpc(body([{ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "mine" } }]), local), { kind: "forward" });
  assert.deepEqual(await interceptJsonRpc(body({ jsonrpc: "2.0", id: 1, method: "initialize" }), local), { kind: "forward" });
  assert.deepEqual(await interceptJsonRpc(Buffer.from("not json"), local), { kind: "forward" });
  assert.deepEqual(await interceptJsonRpc(Buffer.alloc(0), local), { kind: "forward" });
});

test("a tools/list reply that is not JSON is passed through untouched", async () => {
  const upstream = http.createServer((_req, res) => {
    res.writeHead(200, { "content-type": "text/event-stream" });
    res.end("event: message\ndata: {}\n\n");
  });
  await new Promise<void>((r) => upstream.listen(0, "127.0.0.1", () => r()));
  const proxy = await startMcpAuthProxy({
    upstreamUrl: `http://127.0.0.1:${(upstream.address() as AddressInfo).port}/`,
    source: { getToken: async () => "pub", invalidate: () => {} },
    canRefresh: false,
    onFatal: () => {},
    local,
  });
  const r = await fetch(proxy.url, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list" }) });
  assert.equal(r.headers.get("content-type"), "text/event-stream");
  assert.equal(await r.text(), "event: message\ndata: {}\n\n");
  await proxy.close();
  upstream.close();
});
