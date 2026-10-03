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
 * The tools-socket seam's two adapters: the real client over a Unix socket
 * (against a node:http fake of ae-studio-tools listening on a temp socket
 * path, packages/contracts/sockets/ae-studio/mcp/openapi.yaml) and the
 * in-process fake the tests and playground drive turns with.
 */

import { after, before, test } from "node:test";
import assert from "node:assert/strict";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createToolsSocket, ToolsSocketError } from "../src/tools-socket/client.js";
import { DESIGN_TOOL_NAMES, FakeToolsSocket } from "../src/tools-socket/fake.js";
import { loadMcpTools } from "../src/shared/mcp-client.js";
import type { TurnRecord } from "../src/tools-socket/client.js";

/** The eleven design tools (phase 3 exact values, `mcp_tools.go:188-312`). */
const ELEVEN = [
  "list_external_resources",
  "get_external_resource_schema",
  "list_org_endpoints",
  "list_org_component_endpoints",
  "list_platform_resource_types",
  "list_groups",
  "get_remote_git_file_contents",
  "search_remote_git_code",
  "validate_openapi_spec",
  "fetch_openapi_spec",
  "slice_openapi_spec",
];

const RECORD: TurnRecord = {
  turnId: "3f0e8a4c-1d2b-4c6d-8e9f-0a1b2c3d4e5f",
  project: "greeter",
  conversationId: "6a2b1c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
  kind: "browser",
  flow: "",
  status: "completed",
  baseRef: "base1",
  skillsRef: "skills1",
  startedAt: "2026-10-03T10:00:00.000Z",
  finishedAt: "2026-10-03T10:00:05.000Z",
  author: { id: "u1", name: "Ann" },
  model: "claude-sonnet-5",
  modelHost: "api.anthropic.com",
  inputTokens: 10,
  outputTokens: 20,
  cacheReadTokens: 0,
  cacheCreationTokens: 0,
};

interface Seen {
  method: string;
  url: string;
  authorization: string | undefined;
  body: unknown;
}

type Reply = { status: number; body?: unknown; type?: string };
type Route = (req: Seen) => Reply;

let dir: string;
let socketPath: string;
let server: Server;
const seen: Seen[] = [];
let route: Route = () => ({ status: 404 });

function problem(status: number, code: string): Reply {
  return { status, type: "application/problem+json", body: { type: "about:blank", title: code, status, code } };
}

function send(res: ServerResponse, reply: Reply): void {
  if (reply.body === undefined) {
    res.writeHead(reply.status);
    res.end();
    return;
  }
  res.writeHead(reply.status, { "content-type": reply.type ?? "application/json" });
  res.end(JSON.stringify(reply.body));
}

/** The JSON-RPC side of the fake ae-studio-tools: the eleven tools and an echoing call. */
function mcpRoute(body: unknown): Reply {
  const { id, method, params } = body as { id?: unknown; method: string; params?: { name?: string } };
  if (id === undefined) return { status: 202 };
  if (method === "tools/list") {
    const tools = ELEVEN.map((name) => ({ name, description: name, inputSchema: { type: "object", properties: {} } }));
    return { status: 200, body: { jsonrpc: "2.0", id, result: { tools } } };
  }
  if (method === "tools/call") {
    return { status: 200, body: { jsonrpc: "2.0", id, result: { content: [{ type: "text", text: `called ${params?.name}` }] } } };
  }
  return { status: 200, body: { jsonrpc: "2.0", id, error: { code: -32601, message: "method not found" } } };
}

before(async () => {
  dir = await mkdtemp(join(tmpdir(), "tools-sock-"));
  socketPath = join(dir, "mcp.sock");
  server = createServer((req: IncomingMessage, res: ServerResponse) => {
    let raw = "";
    req.on("data", (c: Buffer) => (raw += c));
    req.on("end", () => {
      const s: Seen = {
        method: req.method ?? "",
        url: req.url ?? "",
        authorization: req.headers.authorization,
        body: raw ? JSON.parse(raw) : undefined,
      };
      seen.push(s);
      send(res, s.url === "/mcp" ? mcpRoute(s.body) : route(s));
    });
  });
  await new Promise<void>((resolve) => server.listen(socketPath, resolve));
});

after(async () => {
  server.closeAllConnections();
  await new Promise<void>((resolve) => server.close(() => resolve()));
  await rm(dir, { recursive: true, force: true });
});

function reset(next: Route): void {
  seen.length = 0;
  route = next;
}

test("roomToken: POST /room-token, answers the token", async () => {
  reset(() => ({ status: 200, body: { token: "room-tok", expiresAt: "2026-10-03T11:00:00Z" } }));
  const tools = createToolsSocket(socketPath);
  assert.equal(await tools.roomToken(), "room-tok");
  assert.deepEqual(
    seen.map((s) => [s.method, s.url]),
    [["POST", "/room-token"]],
  );
});

test("roomToken: idp_unavailable surfaces as a transient ToolsSocketError", async () => {
  reset(() => problem(502, "idp_unavailable"));
  const err = await createToolsSocket(socketPath).roomToken().catch((e: unknown) => e);
  assert.ok(err instanceof ToolsSocketError);
  assert.deepEqual([err.status, err.code, err.permanent], [502, "idp_unavailable", false]);
});

test("postUsage: POST /turn-usage with the one record, 202 resolves", async () => {
  reset(() => ({ status: 202 }));
  await createToolsSocket(socketPath).postUsage(RECORD);
  assert.deepEqual(
    seen.map((s) => [s.method, s.url]),
    [["POST", "/turn-usage"]],
  );
  assert.deepEqual(seen[0]?.body, RECORD);
});

test("postUsage: a 400 is a permanent refusal, a 503 is not", async () => {
  const tools = createToolsSocket(socketPath);
  reset(() => problem(400, "invalid_record"));
  const refused = await tools.postUsage(RECORD).catch((e: unknown) => e);
  assert.ok(refused instanceof ToolsSocketError);
  assert.deepEqual([refused.code, refused.permanent], ["invalid_record", true]);

  reset(() => problem(503, "aep_api_unavailable"));
  const down = await tools.postUsage(RECORD).catch((e: unknown) => e);
  assert.ok(down instanceof ToolsSocketError);
  assert.equal(down.permanent, false);
});

test("lookup: GET /projects/{p}, with ?at when given; the reply drops `known`", async () => {
  reset(() => ({
    status: 200,
    body: { known: true, headSha: "h1", skillsSha: "s1", references: ["a.md", "b.pdf"] },
  }));
  const tools = createToolsSocket(socketPath);
  assert.deepEqual(await tools.lookup("greeter"), { headSha: "h1", skillsSha: "s1", references: ["a.md", "b.pdf"] });
  await tools.lookup("greeter", "abc1234");
  assert.deepEqual(
    seen.map((s) => [s.method, s.url]),
    [
      ["GET", "/projects/greeter"],
      ["GET", "/projects/greeter?at=abc1234"],
    ],
  );
});

test("lookup: 404 project_unknown is null; 404 ref_not_found is an error", async () => {
  const tools = createToolsSocket(socketPath);
  reset(() => problem(404, "project_unknown"));
  assert.equal(await tools.lookup("nope"), null);

  reset(() => problem(404, "ref_not_found"));
  const err = await tools.lookup("greeter", "deadbeef").catch((e: unknown) => e);
  assert.ok(err instanceof ToolsSocketError);
  assert.deepEqual([err.status, err.code], [404, "ref_not_found"]);
});

test("lookup: the descriptor's idea rides through when the socket answers one", async () => {
  reset(() => ({
    status: 200,
    body: { known: true, headSha: "h1", skillsSha: "s1", references: [], idea: "A greeter that waves" },
  }));
  assert.deepEqual(await createToolsSocket(socketPath).lookup("greeter"), {
    headSha: "h1",
    skillsSha: "s1",
    references: [],
    idea: "A greeter that waves",
  });
});

test("lookup: an idea that is not a string is an invalid_response error", async () => {
  reset(() => ({ status: 200, body: { known: true, headSha: "h1", skillsSha: "s1", references: [], idea: 7 } }));
  const err = await createToolsSocket(socketPath).lookup("greeter").catch((e: unknown) => e);
  assert.ok(err instanceof ToolsSocketError);
  assert.equal(err.code, "invalid_response");
});

test("lookup: a 200 without the snapshot shape is an invalid_response error", async () => {
  reset(() => ({ status: 200, body: { known: true, headSha: "h1" } }));
  const err = await createToolsSocket(socketPath).lookup("greeter").catch((e: unknown) => e);
  assert.ok(err instanceof ToolsSocketError);
  assert.equal(err.code, "invalid_response");
});

test("skills: GET /skills, answers the sha", async () => {
  reset(() => ({ status: 200, body: { skillsSha: "s9" } }));
  assert.deepEqual(await createToolsSocket(socketPath).skills(), { skillsSha: "s9" });
  assert.deepEqual(
    seen.map((s) => [s.method, s.url]),
    [["GET", "/skills"]],
  );
});

test("loadMcpTools(socket) lists the eleven tools over the socket and calls one, no bearer", async () => {
  reset(() => ({ status: 404 }));
  const tools = await loadMcpTools(createToolsSocket(socketPath));
  assert.deepEqual(Object.keys(tools).sort(), [...ELEVEN].sort());
  const out = await tools.list_groups!.execute!({}, {} as never);
  assert.equal(out, "called list_groups");
  assert.ok(seen.length >= 2 && seen.every((s) => s.method === "POST" && s.url === "/mcp"));
  assert.ok(seen.every((s) => s.authorization === undefined), "the socket is the gate: no Authorization header");
});

test("an unreachable socket is a transient socket_unreachable error", async () => {
  const tools = createToolsSocket(join(dir, "absent.sock"));
  const err = await tools.skills().catch((e: unknown) => e);
  assert.ok(err instanceof ToolsSocketError);
  assert.deepEqual([err.status, err.code, err.permanent], [0, "socket_unreachable", false]);
  // MCP discovery stays best-effort: an absent socket is no tools, not a throw.
  assert.deepEqual(await loadMcpTools(tools), {});
});

test("a socket that never answers times out as a transient error", async () => {
  const hang = createServer(() => {
    /* accept and never answer */
  });
  const hangPath = join(dir, "hang.sock");
  await new Promise<void>((resolve) => hang.listen(hangPath, resolve));
  try {
    const err = await createToolsSocket(hangPath, { requestTimeoutMs: 100 })
      .skills()
      .catch((e: unknown) => e);
    assert.ok(err instanceof ToolsSocketError);
    assert.deepEqual([err.code, err.permanent], ["timeout", false]);
  } finally {
    hang.closeAllConnections();
    await new Promise<void>((resolve) => hang.close(() => resolve()));
  }
});

test("fake: loadMcpTools lists the eleven tools; tool calls and usage are recorded", async () => {
  assert.deepEqual([...DESIGN_TOOL_NAMES].sort(), [...ELEVEN].sort());
  const fake = new FakeToolsSocket({
    projects: { greeter: { headSha: "h1", skillsSha: "s1", references: ["a.md"] } },
    skillsSha: "s1",
  });
  const tools = await loadMcpTools(fake);
  assert.deepEqual(Object.keys(tools).sort(), [...ELEVEN].sort());
  await tools.fetch_openapi_spec!.execute!({ url: "https://x" }, {} as never);
  assert.deepEqual(fake.toolCalls, [{ name: "fetch_openapi_spec", arguments: { url: "https://x" } }]);

  assert.deepEqual(await fake.lookup("greeter"), { headSha: "h1", skillsSha: "s1", references: ["a.md"] });
  assert.equal(await fake.lookup("nope"), null);
  assert.deepEqual(await fake.skills(), { skillsSha: "s1" });
  assert.equal(typeof (await fake.roomToken()), "string");

  await fake.postUsage(RECORD);
  assert.deepEqual(fake.usage, [RECORD]);
});

test("fake: lookup(project, at?) records each call; a missing ref throws ref_not_found, apart from an unknown project", async () => {
  const fake = new FakeToolsSocket({
    projects: { greeter: { headSha: "h1", skillsSha: "s1", references: [] } },
    missingRefs: ["deadbeef"],
  });
  assert.deepEqual(await fake.lookup("greeter", "abc1234"), { headSha: "h1", skillsSha: "s1", references: [] });
  assert.equal(await fake.lookup("nope"), null);
  assert.equal(await fake.lookup("nope", "deadbeef"), null, "an unknown project wins over a missing ref");
  const err = await fake.lookup("greeter", "deadbeef").catch((e: unknown) => e);
  assert.ok(err instanceof ToolsSocketError);
  assert.deepEqual([err.status, err.code, err.permanent], [404, "ref_not_found", true]);
  assert.deepEqual(fake.lookups, [
    { project: "greeter", at: "abc1234" },
    { project: "nope" },
    { project: "nope", at: "deadbeef" },
    { project: "greeter", at: "deadbeef" },
  ]);
});

test("fake: failUsage makes the next postUsage calls fail, transient by default", async () => {
  const fake = new FakeToolsSocket();
  fake.failUsage(1);
  const err = await fake.postUsage(RECORD).catch((e: unknown) => e);
  assert.ok(err instanceof ToolsSocketError);
  assert.equal(err.permanent, false);
  await fake.postUsage(RECORD);
  assert.equal(fake.usage.length, 1);
});
