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
import { spawn } from "node:child_process";
import fs from "node:fs";
import { readModelConnection } from "./model_connection.js";
import { AEP_WEB_SERVER, AEP_WEB_TOOL, aepWebCommand, mountAepWeb } from "./aep_web.js";
import { WEBSEARCH_DENIAL_MESSAGE } from "./websearch_dlp.js";

const KEY = "ollama-key-0123456789abcdef";
const STAGED = "orders-db-password-0123456789";

const ollama = readModelConnection("m", {
  AEP_MODEL_FORMAT: "openai-compatible",
  AEP_MODEL_BASE_URL: "https://ollama.com/v1",
  AEP_MODEL_AUTH_SCHEME: "bearer",
  AEP_MODEL_WEB_SEARCH: "ollama-api",
});

const inputs = {
  connection: ollama,
  env: { AEP_MODEL_API_KEY: KEY },
  deniedValues: [KEY, STAGED],
  denialMessage: WEBSEARCH_DENIAL_MESSAGE,
};

test("mountAepWeb: nothing to mount unless the strategy is ollama-api", () => {
  assert.deepEqual(mountAepWeb({ ...inputs, connection: readModelConnection("m", {}) }), {});
  const none = readModelConnection("m", { AEP_MODEL_FORMAT: "openai-compatible", AEP_MODEL_BASE_URL: "https://llm.example.com/v1", AEP_MODEL_WEB_SEARCH: "none" });
  assert.deepEqual(mountAepWeb({ ...inputs, connection: none }), {});
});

test("mountAepWeb: a strategy it cannot serve says why, rather than mounting nothing in silence", () => {
  assert.match(mountAepWeb({ ...inputs, env: {} }).reason ?? "", /no connection key/);
  assert.match(mountAepWeb({ ...inputs, command: undefined }).reason ?? "", /not installed/);
});

test("mountAepWeb: the key and the staged values go in a 0600 file named by argv, never in argv itself", () => {
  const { mount } = mountAepWeb({ ...inputs, command: { command: "/usr/bin/node", args: ["/opt/aep/web-search/aep-web.mjs"] } });
  assert.ok(mount);
  try {
    const { server } = mount;
    assert.equal(server.name, AEP_WEB_SERVER);
    assert.equal(server.tool, AEP_WEB_TOOL);
    assert.equal(server.command, "/usr/bin/node");
    assert.equal(server.args[0], "/opt/aep/web-search/aep-web.mjs");
    const configFile = server.args[1] ?? "";
    assert.doesNotMatch(JSON.stringify(server), new RegExp(KEY), "the key is on the server's command line");
    assert.equal(fs.statSync(configFile).mode & 0o777, 0o600);
    assert.deepEqual(JSON.parse(fs.readFileSync(configFile, "utf8")), {
      baseURL: "https://ollama.com/v1",
      apiKey: KEY,
      deniedValues: [KEY, STAGED],
      denialMessage: WEBSEARCH_DENIAL_MESSAGE,
    });
    mount.close();
    assert.equal(fs.existsSync(configFile), false, "close removes the private file");
  } finally {
    mount.close();
  }
});

/** One MCP exchange with a spawned server: send each request, collect the answers by id. */
async function exchange(command: string, args: string[], requests: object[]): Promise<Map<unknown, { result?: unknown; error?: unknown }>> {
  const child = spawn(command, args, { stdio: ["pipe", "pipe", "pipe"] });
  let out = "";
  child.stdout.on("data", (chunk: Buffer) => (out += chunk.toString("utf8")));
  for (const r of requests) child.stdin.write(JSON.stringify(r) + "\n");
  child.stdin.end();
  await new Promise<void>((resolve, reject) => {
    child.on("error", reject);
    child.on("close", () => resolve());
  });
  const answers = new Map<unknown, { result?: unknown; error?: unknown }>();
  for (const line of out.trim().split("\n").filter(Boolean)) {
    const msg = JSON.parse(line) as { id: unknown; result?: unknown; error?: unknown };
    answers.set(msg.id, msg);
  }
  return answers;
}

// The real server, from the monorepo's source, through the contract this module
// writes: a query holding a staged secret is refused before anything is sent —
// the staged value here is a dependency secret, not the connection's metadata.
test("aep-web: the spawned server refuses a query holding a staged secret, in the WebSearch rule's words", async () => {
  const command = aepWebCommand({});
  assert.ok(command, "the monorepo's aep-web source is resolvable");
  const { mount } = mountAepWeb({ ...inputs, command });
  assert.ok(mount);
  try {
    const answers = await exchange(mount.server.command, [...mount.server.args], [
      { jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "t", version: "0" } } },
      { jsonrpc: "2.0", method: "notifications/initialized" },
      { jsonrpc: "2.0", id: 2, method: "tools/list" },
      { jsonrpc: "2.0", id: 3, method: "tools/call", params: { name: AEP_WEB_TOOL, arguments: { query: `connect with ${STAGED}` } } },
    ]);
    const tools = (answers.get(2)?.result as { tools: { name: string }[] }).tools.map((t) => t.name);
    assert.deepEqual(tools, [AEP_WEB_TOOL]);
    assert.deepEqual(answers.get(3)?.result, { content: [{ type: "text", text: WEBSEARCH_DENIAL_MESSAGE }], isError: true });
  } finally {
    mount.close();
  }
});
