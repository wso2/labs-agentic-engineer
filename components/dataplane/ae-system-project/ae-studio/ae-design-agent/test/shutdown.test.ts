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
 * SIGTERM inside the grace (07 §10, Review Focus 6): new turns are refused
 * first (`/v1` and the Turn socket answer 503), every running turn ends at
 * once (`turn-failed {reason: shutdown}` on the SSE stream, `result {status:
 * failed, code: shutdown}` on the Turn socket), its record reaches the tools
 * socket before the process exits, and the outbox drain is bounded. The last
 * test drives the real `main.ts` with a real SIGTERM.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createServer as createHttpServer, type IncomingMessage } from "node:http";
import { createServer as createNetServer, type Socket } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { mockModel } from "../src/shared/mock-model.js";
import { SHUTDOWN_HANDOVER_MS, shutdown, type ShutdownLogLine } from "../src/pod/shutdown.js";
import type { TurnRecord } from "../src/tools-socket/client.js";
import { PROJECT, call, postTurnSocket, readSse, startEdge } from "./helpers/edge.js";

const GOLDEN = join(import.meta.dirname, "../../../../../../packages/contracts/sockets/ae-studio/turn/golden");
const SHUTDOWN_RESULT = readFileSync(join(GOLDEN, "shutdown.ndjson"), "utf8").split("\n").filter((l) => l !== "").at(-1)!;
const ANN = { userId: "u-ann", name: "Ann", email: "ann@x" };

async function until(cond: () => boolean, what: string, ms = 5_000): Promise<void> {
  const deadline = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > deadline) assert.fail(`timed out waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 10));
  }
}

test("SIGTERM during a turn: refuse, end it (SSE and Turn socket), hand its record over, then close", async () => {
  // A model that would answer in a minute: the turn is running when the signal comes.
  const edge = await startEdge({ models: [mockModel([{ kind: "text", text: "late" }], { delayMs: 60_000 })], keepAliveMs: 50 });
  try {
    const turnId = randomUUID();
    const socket = await postTurnSocket(edge.turnSocket, { turnId, project: PROJECT, kind: "start", credit: ANN });
    assert.equal(socket.status, 200);
    assert.deepEqual(await socket.next(), { type: "keep-alive" });
    const tok = await edge.token();
    const sse = call(edge, `/v1/projects/${PROJECT}/turns/${turnId}/stream?from=0`, tok).then(readSse);

    const hold = edge.tools.holdUsage();
    const logs: ShutdownLogLine[] = [];
    const atExit: { usage?: TurnRecord[] } = {};
    const done = shutdown({ turns: edge.turns, desk: edge.desk, outbox: edge.outbox, listeners: edge.pod, log: (l) => logs.push(l) }).then(() => {
      atExit.usage = [...edge.tools.usage];
    });

    // The socket stream ends with the shutdown result, byte for byte the golden line.
    const rest = await socket.rest();
    assert.equal(rest.at(-1), SHUTDOWN_RESULT);
    assert.ok(rest.slice(0, -1).every((l) => l === '{"type":"keep-alive"}'));
    const { frames } = await sse;
    assert.deepEqual(frames.at(-2)?.data, { type: "turn-failed", reason: "shutdown" });
    assert.equal(frames.at(-1)?.raw, "[DONE]");

    // While the records drain, new turns are refused on both doors (D-6).
    const conv = (await (await call(edge, `/v1/projects/${PROJECT}/conversations/current`, tok)).json()) as { conversationId: string };
    const browser = await call(edge, `/v1/projects/${PROJECT}/conversations/${conv.conversationId}/turns`, tok, { json: { instruction: "hi" } });
    assert.equal(browser.status, 503);
    assert.equal(((await browser.json()) as { code: string }).code, "shutting_down");
    const server = await postTurnSocket(edge.turnSocket, { turnId: randomUUID(), project: PROJECT, kind: "plan", credit: ANN });
    assert.equal(server.status, 503);
    assert.equal((await server.json()).code, "shutting_down");
    assert.equal(atExit.usage, undefined, "the shutdown waits for the record");

    hold.release();
    await done;
    // Read through a cast: the check above narrowed the field to undefined.
    const delivered = atExit.usage as TurnRecord[] | undefined;
    assert.equal(delivered?.length, 1, "the record reached the tools socket before the listeners closed");
    assert.equal(delivered![0]!.turnId, turnId);
    assert.equal(delivered![0]!.status, "failed");
    assert.equal(delivered![0]!.reason, "shutdown");
    assert.deepEqual(logs.map((l) => l.msg), ["pod_shutdown_started"]);
    assert.equal(existsSync(edge.turnSocket), false, "the Turn socket is gone");
    await assert.rejects(fetch(`${edge.healthUrl}/readyz`));
  } finally {
    await edge.close();
  }
});

test("a start whose lookup is held open across SIGTERM never launches a turn (R2-I1)", async () => {
  const edge = await startEdge({ models: [mockModel([{ kind: "text", text: "late" }], { delayMs: 60_000 })] });
  try {
    const tok = await edge.token();
    const conv = (await (await call(edge, `/v1/projects/${PROJECT}/conversations/current`, tok)).json()) as { conversationId: string };
    const held = edge.tools.holdLookups();
    const lookupsBefore = edge.tools.lookups.length;
    const server = postTurnSocket(edge.turnSocket, { turnId: randomUUID(), project: PROJECT, kind: "plan", credit: ANN });
    const browser = call(edge, `/v1/projects/${PROJECT}/conversations/${conv.conversationId}/turns`, tok, { json: { instruction: "hi" } });
    await until(() => edge.tools.lookups.length === lookupsBefore + 2, "both starts to reach their lookup");

    edge.turns.refuse();
    await edge.desk.abortAll("shutdown");
    held.release();

    const s = await server;
    assert.equal(s.status, 503);
    assert.equal((await s.json()).code, "shutting_down");
    const b = await browser;
    assert.equal(b.status, 503);
    assert.equal(((await b.json()) as { code: string }).code, "shutting_down");
    assert.equal(edge.desk.active({ kind: "project", project: PROJECT }), null, "no turn runs past the shutdown");
  } finally {
    await edge.close();
  }
});

test("shutdown order: refuse, abort, drain, close; abort + drain end ≤ 8 s after the start; an incomplete drain is logged", async () => {
  const steps: string[] = [];
  const logs: ShutdownLogLine[] = [];
  let clock = 1_000;
  await shutdown({
    now: () => clock,
    turns: { refuse: () => void steps.push("refuse") },
    desk: {
      abortAll: async (reason) => {
        steps.push(`abortAll:${reason}`);
        clock += 2_000; // the desk's whole abort grace
      },
    },
    outbox: {
      drain: async (ms) => {
        steps.push(`drain:${ms}`);
        return false;
      },
    },
    listeners: {
      close: async () => {
        steps.push("close");
      },
    },
    log: (l) => logs.push(l),
  });
  // The drain gets what the abort left of the handover: 8 - 2 = 6 s.
  assert.deepEqual(steps, ["refuse", "abortAll:shutdown", "drain:6000", "close"]);
  assert.ok(SHUTDOWN_HANDOVER_MS <= 8_000, "ae-studio-tools' 10 s socket window needs the handover to end ≤ 8 s after SIGTERM");
  assert.deepEqual(logs.map((l) => l.msg), ["pod_shutdown_started", "usage_drain_incomplete"]);
});

test("an abort that spends the whole handover leaves the drain no time, never a negative bound", async () => {
  let clock = 0;
  let drained: number | undefined;
  await shutdown({
    now: () => clock,
    turns: { refuse: () => {} },
    desk: { abortAll: async () => void (clock += 9_000) },
    outbox: { drain: async (ms) => ((drained = ms), true) },
    listeners: { close: async () => {} },
    log: () => {},
  });
  assert.equal(drained, 0);
});

/** A stand-in for ae-studio-tools' MCP socket: one project, room tokens, and the usage records it receives. */
async function fakeToolsSocket(path: string, head: string, skills: string) {
  const usage: Array<{ at: number; record: TurnRecord }> = [];
  const body = (req: IncomingMessage) =>
    new Promise<string>((resolve) => {
      let raw = "";
      req.on("data", (c: Buffer) => (raw += c.toString()));
      req.on("end", () => resolve(raw));
    });
  const server = createHttpServer(async (req, res) => {
    const json = (status: number, v: unknown) => res.writeHead(status, { "content-type": "application/json" }).end(JSON.stringify(v));
    if (req.method === "GET" && req.url === `/projects/${PROJECT}`) return json(200, { known: true, headSha: head, skillsSha: skills, references: [] });
    if (req.method === "POST" && req.url === "/room-token") return json(200, { token: "room-token", expiresAt: new Date(Date.now() + 600_000).toISOString() });
    if (req.method === "POST" && req.url === "/turn-usage") {
      usage.push({ at: Date.now(), record: JSON.parse(await body(req)) as TurnRecord });
      return res.writeHead(202).end();
    }
    return json(404, { type: "about:blank", title: "Not Found", status: 404, code: "not_found" });
  });
  await new Promise<void>((resolve) => server.listen(path, resolve));
  return { usage, close: () => new Promise<void>((resolve) => server.close(() => resolve())) };
}

test("main.ts on a real SIGTERM: the Turn socket's turn ends with the shutdown result and its record is delivered before exit", async () => {
  const root = mkdtempSync(join(tmpdir(), "ae-sigterm-"));
  const head = "c".repeat(40);
  const skills = "d".repeat(40);
  mkdirSync(join(root, "snapshots", "projects", PROJECT, head, "specs", "requirements"), { recursive: true });
  writeFileSync(join(root, "snapshots", "projects", PROJECT, head, "specs", "requirements", "prd.md"), "# PRD\n");
  mkdirSync(join(root, "snapshots", "skills", skills), { recursive: true });
  const tools = await fakeToolsSocket(join(root, "mcp.sock"), head, skills);
  // A collab listener that accepts and never answers: the kickoff's Room join
  // hangs, so the turn is running when the signal comes (and ignores its abort).
  const held: Socket[] = [];
  const collab = createNetServer((s) => void held.push(s));
  await new Promise<void>((resolve) => collab.listen(0, "127.0.0.1", resolve));
  const collabPort = (collab.address() as { port: number }).port;
  const turnSocket = join(root, "turn.sock");

  const child = spawn(process.execPath, ["--import", "tsx", "src/main.ts"], {
    cwd: join(import.meta.dirname, ".."),
    env: {
      PATH: process.env.PATH ?? "",
      AE_ORG_ID: "ou-acme",
      AE_ORG_HANDLE: "acme",
      AE_IDP_ISSUER: "http://thunder.test",
      AE_IDP_JWKS_URL: "http://127.0.0.1:1/jwks",
      AE_USER_AUDIENCES: "aep-console-client",
      AE_MCP_SOCKET: join(root, "mcp.sock"),
      AE_TURN_SOCKET: turnSocket,
      AE_COLLAB_LOCAL_URL: `ws://127.0.0.1:${collabPort}`,
      AE_SNAPSHOTS_DIR: join(root, "snapshots"),
      AE_LISTEN_PORT: "0",
      AE_HEALTH_PORT: "0",
      AE_MODEL_CONNECTION:
        '{"format":"anthropic","baseURL":"https://api.anthropic.com/v1","authScheme":"x-api-key","capabilities":{"claudeCode":true,"claudeSubscription":true,"promptCache":true,"generatedAgents":true,"nativePdf":true,"webSearch":"anthropic-server-tool","imageInput":"yes"},"model":"claude-sonnet-4-6"}',
      ANTHROPIC_API_KEY: "test-key-not-a-secret",
      AGENT_DEVTOOLS: "false",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (c: Buffer) => (stdout += c.toString()));
  child.stderr.on("data", (c: Buffer) => (stderr += c.toString()));
  const exited = new Promise<{ code: number | null; at: number }>((resolve) => child.once("exit", (code) => resolve({ code, at: Date.now() })));
  try {
    await until(() => stdout.includes('"pod_turn_socket_listening"'), `the pod to listen (stderr: ${stderr})`, 30_000);
    const turnId = randomUUID();
    const res = await postTurnSocket(turnSocket, { turnId, project: PROJECT, kind: "start", credit: ANN });
    assert.equal(res.status, 200);
    await until(() => held.length > 0, "the Room join to reach the collab listener");

    child.kill("SIGTERM");
    const lines = await res.rest();
    assert.equal(lines.at(-1), SHUTDOWN_RESULT);
    const { code, at } = await exited;
    assert.equal(code, 0, stderr);
    assert.equal(tools.usage.length, 1, "the record reached the tools socket");
    assert.equal(tools.usage[0]!.record.turnId, turnId);
    assert.equal(tools.usage[0]!.record.kind, "kickoff");
    assert.equal(tools.usage[0]!.record.reason, "shutdown");
    assert.ok(tools.usage[0]!.at <= at, "before exit");
    assert.equal(existsSync(turnSocket), false, "the Turn socket file is removed");
  } finally {
    if (child.exitCode === null) child.kill("SIGKILL");
    for (const s of held) s.destroy();
    collab.close();
    await tools.close();
    rmSync(root, { recursive: true, force: true });
  }
});
