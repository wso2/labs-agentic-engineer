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
 * The Turn socket (`packages/contracts/sockets/ae-studio/turn/`): the
 * server-started turns ae-studio-tools relays. A Plan turn's ok Task results
 * become `task-op` lines and nothing else does; a kickoff joins the project's
 * Room on ae-collab's Room socket as the credited user; a retry with the
 * same `turnId` reattaches; the lines are the golden streams ae-studio-tools
 * is tested against. Driven over the real Unix socket.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { Server, type Document } from "@hocuspocus/server";
import { readDocFile, setDocFile } from "@aep/collab-doc";
import type { LanguageModel } from "ai";
import { PLAN_TASK, UPDATE_TASK } from "@aep/agent-stream";
import { mockModel } from "../src/shared/mock-model.js";
import { frameLine, parseTurnRequest, resultOf, taskOpOf, type TaskOpFrame, type TurnSocketFrame } from "../src/edge/turn-socket.js";
import { creditParameter } from "../src/collab/local-room.js";
import { SEED_FILES } from "./seed-files.js";
import { PROJECT, call, postTurnSocket, startEdge, streamOf, type Edge } from "./helpers/edge.js";

const GOLDEN = join(import.meta.dirname, "../../../../../../packages/contracts/sockets/ae-studio/turn/golden");
const goldenLines = (name: string): string[] => readFileSync(join(GOLDEN, name), "utf8").split("\n").filter((l) => l !== "");

const NOBODY = { userId: "", name: "", email: "" };
const ANN = { userId: "u-ann", name: "Ann", email: "ann@x" };

const TASK_PLANNING_MD = `---
name: task-planning
description: Plan the implementation Tasks.
metadata:
  aep:
    kind: platform
---

One Task per component.
`;

async function withEdge(opts: Parameters<typeof startEdge>[0], body: (edge: Edge) => Promise<void>): Promise<void> {
  const edge = await startEdge(opts);
  try {
    await body(edge);
  } finally {
    await edge.close();
  }
}

async function until(cond: () => boolean, what: string, ms = 5_000): Promise<void> {
  const deadline = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > deadline) assert.fail(`timed out waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 10));
  }
}

const withoutKeepAlives = (lines: string[]) => lines.map((l) => JSON.parse(l) as Record<string, unknown>).filter((f) => f.type !== "keep-alive");

test("taskOpOf: only an ok planTask/updateTask result is a Task operation", () => {
  const ok = { ok: true, op: "plan", component: "hello-api", title: "T" };
  assert.deepEqual(taskOpOf({ type: "tool-result", toolName: "planTask", output: ok }), { op: "plan", output: ok });
  const upd = { ok: true, op: "update", ref: { title: "T" }, set: { body: "b" } };
  assert.deepEqual(taskOpOf({ type: "tool-result", toolName: "updateTask", output: upd }), { op: "update", output: upd });
  assert.equal(taskOpOf({ type: "tool-result", toolName: "planTask", output: { ok: false, op: "plan", code: "UNKNOWN_COMPONENT" } }), null);
  assert.equal(taskOpOf({ type: "tool-result", toolName: "loadSkill", output: { ok: true, op: "plan" } }), null);
  assert.equal(taskOpOf({ type: "tool-call", toolName: "planTask", input: ok }), null);
  assert.equal(taskOpOf({ type: "tool-result", toolName: "planTask", output: { ok: true, op: "add" } }), null);
  assert.equal(taskOpOf("[DONE]"), null);
});

test("the lines are the golden streams' lines, byte for byte", () => {
  for (const name of ["completed.ndjson", "shutdown.ndjson", "provider_limit.ndjson"]) {
    for (const line of goldenLines(name)) {
      assert.equal(frameLine(JSON.parse(line) as TurnSocketFrame), `${line}\n`, `${name}: ${line}`);
    }
  }
});

test("a provider_limit result carries the provider's reset time when it stated one, and omits it otherwise", () => {
  const message = "api.anthropic.com's usage limit is reached. Try again after 2026-10-04T10:15:00.000Z.";
  const resetAt = "2026-10-04T10:15:00.000Z";
  const limited = { type: "turn-failed", reason: "agent-error", code: "provider_limit", host: "api.anthropic.com", message } as const;
  assert.equal(frameLine(resultOf({ ...limited, resetAt })), `${goldenLines("provider_limit.ndjson").at(-1)}\n`);
  assert.deepEqual(resultOf({ ...limited, message: "later" }), { type: "result", status: "failed", code: "provider_limit", message: "later" });
  // resetAt belongs to the provider limit: no other ending carries one.
  assert.equal("resetAt" in resultOf({ type: "turn-failed", reason: "agent-error", code: "output_truncated", resetAt }), false);
});

test("the golden task-op lines carry the tool output the agent projects (ok and op included)", () => {
  const ops = goldenLines("completed.ndjson")
    .map((l) => JSON.parse(l) as TurnSocketFrame)
    .filter((f): f is TaskOpFrame => f.type === "task-op");
  assert.equal(ops.length, 2);
  for (const frame of ops) {
    const toolName = frame.op === "plan" ? PLAN_TASK : UPDATE_TASK;
    assert.deepEqual(taskOpOf({ type: "tool-result", toolName, output: frame.output }), { op: frame.op, output: frame.output });
  }
});

test("parseTurnRequest refuses what the contract does not declare", () => {
  const ok = { turnId: randomUUID(), project: PROJECT, kind: "plan", credit: NOBODY };
  assert.equal(typeof parseTurnRequest(ok), "object");
  for (const bad of [
    null,
    { ...ok, turnId: "t1" },
    { ...ok, kind: "chat" },
    { ...ok, credit: { userId: "u" } },
    { ...ok, credit: { ...NOBODY, extra: "x" } },
    { ...ok, collab: {} },
    { ...ok, text: 1 },
    { ...ok, scope: { tag: "v1", stories: [{ number: "1", covered: false }] } },
    { ...ok, taskContext: [{ path: "tasks/1.md" }] },
  ]) {
    assert.equal(typeof parseTurnRequest(bad), "string", JSON.stringify(bad));
  }
});

test("a Plan turn: two ok planTask results are two task-op lines, then result completed", () =>
  withEdge(
    {
      files: SEED_FILES,
      skillFiles: { "skills/task-planning/SKILL.md": TASK_PLANNING_MD },
      models: [
        mockModel([
          { kind: "toolCall", toolCallId: "p1", toolName: "planTask", input: { component: "hello-api", title: "Build hello-api", dependsOn: [], rationale: "core." } },
          { kind: "toolCall", toolCallId: "p2", toolName: "planTask", input: { component: "nope", title: "Ghost", dependsOn: [], rationale: "wrong." } },
          { kind: "toolCall", toolCallId: "s1", toolName: "loadSkill", input: { name: "task-planning" } },
          { kind: "toolCall", toolCallId: "p3", toolName: "planTask", input: { component: "hello-api", title: "Test hello-api", dependsOn: [], rationale: "proof." } },
          { kind: "text", text: "planned" },
        ]),
      ],
      room: () => assert.fail("a Plan turn never joins the Room"),
    },
    async (edge) => {
      const turnId = randomUUID();
      const res = await postTurnSocket(edge.turnSocket, {
        turnId,
        project: PROJECT,
        kind: "plan",
        credit: NOBODY,
        scope: { tag: "v1.0.0", stories: [{ id: "F1.1", title: "Say hello", covered: false }] },
        taskContext: [{ path: "tasks/7.md", body: "# Existing Task seven" }],
      });
      assert.equal(res.status, 200);
      assert.equal(res.contentType, "application/x-ndjson");
      const frames = withoutKeepAlives(await res.rest());
      assert.deepEqual(frames.map((f) => [f.type, f.op ?? f.status]), [
        ["task-op", "plan"],
        ["task-op", "plan"],
        ["result", "completed"],
      ]);
      assert.deepEqual(frames.map((f) => (f.output as { title?: string } | undefined)?.title), ["Build hello-api", "Test hello-api", undefined]);
      assert.equal((frames[0]!.output as { ok: boolean }).ok, true, "the whole ok result, as the plan tap decodes it");
      assert.deepEqual(frames[2], { type: "result", status: "completed" });

      const status = edge.desk.status(turnId)!;
      assert.equal(status.kind, "plan");
      assert.equal(status.project, PROJECT);
      assert.equal(status.flow, "");
      assert.notEqual(status.conversationId, edge.threads.current(PROJECT).conversationId, "a throwaway conversation");
      assert.equal(await edge.store.get(status.conversationId), null, "dropped once the turn ended");

      await until(() => edge.tools.usage.length === 1, "the usage record");
      const rec = edge.tools.usage[0]!;
      assert.equal(rec.turnId, turnId);
      assert.equal(rec.kind, "plan");
      assert.equal(rec.status, "completed");
      assert.equal(rec.author, undefined, "a credit that names no one");
    },
  ));

test("a Plan turn runs the task-plan toolset on the scope and the existing Tasks", async () => {
  const model = mockModel([{ kind: "text", text: "nothing to plan" }]);
  await withEdge({ files: SEED_FILES, models: [model] }, async (edge) => {
    const res = await postTurnSocket(edge.turnSocket, {
      turnId: randomUUID(),
      project: PROJECT,
      kind: "plan",
      credit: ANN,
      scope: {
        tag: "v1.0.0",
        stories: [{ id: "F1.1", title: "Say hello", covered: true }],
        features: [{ id: "F1", name: "Greeting", needs: [] }],
        productWide: [{ id: "P1", text: "Every answer is JSON", appliesTo: ["all"] }],
      },
      taskContext: [{ path: "tasks/7.md", body: "# Existing Task seven" }],
    });
    assert.deepEqual(withoutKeepAlives(await res.rest()), [{ type: "result", status: "completed" }]);
    const call0 = model.doStreamCalls[0]!;
    const tools = (call0.tools ?? []).map((t) => t.name);
    assert.ok(tools.includes("planTask") && tools.includes("updateTask"));
    assert.equal(tools.includes("addFile"), false);
    const prompt = JSON.stringify(call0.prompt);
    assert.match(prompt, /Existing Task seven/);
    assert.match(prompt, /v1\.0\.0/);
    assert.match(prompt, /Story F1\.1: Say hello/);
    assert.match(prompt, /one Task per feature per component that serves it:\\n\\n- F1 Greeting/);
    assert.match(prompt, /- P1: Every answer is JSON \(applies to all\)/);
    assert.deepEqual(edge.tools.lookups, [{ project: PROJECT }], "no at: the default-branch tip");
  });
});

const SHA = "c".repeat(40);

test("parseTurnRequest: at is a 40-hex commit sha, on a plan turn only", () => {
  const plan = { turnId: randomUUID(), project: PROJECT, kind: "plan", credit: NOBODY };
  assert.equal((parseTurnRequest({ ...plan, at: SHA }) as { at?: string }).at, SHA);
  assert.equal("at" in (parseTurnRequest(plan) as object), false);
  assert.equal(parseTurnRequest({ ...plan, kind: "start", at: SHA }), "at is only for a plan turn");
  for (const at of ["tags/v1.0.0", "C".repeat(40), "c".repeat(39), "c".repeat(64), 7]) {
    assert.equal(parseTurnRequest({ ...plan, at }), "at must be a 40-hex commit sha", String(at));
  }
});

test("a plan turn reads the repository at its pinned sha; a start turn carrying at is 400 invalid_turn", async () => {
  const model = mockModel([{ kind: "text", text: "nothing to plan" }]);
  await withEdge({ files: SEED_FILES, models: [model] }, async (edge) => {
    const plan = await postTurnSocket(edge.turnSocket, { turnId: randomUUID(), project: PROJECT, kind: "plan", credit: NOBODY, at: SHA });
    assert.deepEqual(withoutKeepAlives(await plan.rest()), [{ type: "result", status: "completed" }]);
    assert.deepEqual(edge.tools.lookups, [{ project: PROJECT, at: SHA }]);

    const start = await postTurnSocket(edge.turnSocket, { turnId: randomUUID(), project: PROJECT, kind: "start", credit: ANN, at: SHA });
    assert.equal(start.status, 400);
    assert.equal((await start.json()).code, "invalid_turn");
    assert.equal(edge.tools.lookups.length, 1, "a refused start resolves nothing");
  });
});

test("a pinned sha that names no commit of the repository is 400 invalid_turn, not a tools outage", async () => {
  await withEdge({ files: SEED_FILES, missingRefs: [SHA] }, async (edge) => {
    const res = await postTurnSocket(edge.turnSocket, { turnId: randomUUID(), project: PROJECT, kind: "plan", credit: NOBODY, at: SHA });
    assert.equal(res.status, 400);
    const problem = await res.json();
    assert.equal(problem.code, "invalid_turn");
    assert.match(String(problem.detail), /names no commit/);
  });
});

test("ref_not_found on a plan with no pin is the sidecar's 503 tools_unavailable; only a pinned sha is the request's fault", async () => {
  await withEdge({ files: SEED_FILES }, async (edge) => {
    edge.tools.failNextLookup("ref_not_found");
    const res = await postTurnSocket(edge.turnSocket, { turnId: randomUUID(), project: PROJECT, kind: "plan", credit: NOBODY });
    assert.equal(res.status, 503);
    assert.equal((await res.json()).code, "tools_unavailable");
  });
});

test("a kickoff joins spec-acme-greeter on the Room socket as the credited user, with no token, on the current thread", async () => {
  const dir = mkdtempSync(join(tmpdir(), "ae-room-"));
  const roomSocket = join(dir, "room.sock");
  const joins: Array<{ room: string; token: string; credit: string | null }> = [];
  let doc: Document | undefined;
  const collab = new Server({
    quiet: true,
    onAuthenticate: ({ documentName, token, requestParameters }) => {
      joins.push({ room: documentName, token, credit: requestParameters.get("credit") });
      return Promise.resolve();
    },
    onLoadDocument: ({ document }) => {
      doc = document;
      setDocFile(document, "specs/requirements/prd.md", "# PRD\n\nSeeded.");
      return Promise.resolve(document);
    },
  });
  // ae-collab's Room socket: the Hocuspocus server's own HTTP server, bound to a Unix socket.
  await new Promise<void>((resolve) => collab.httpServer.listen(roomSocket, resolve));
  const model = mockModel([
    { kind: "toolCall", toolCallId: "a1", toolName: "addFile", input: { path: "specs/requirements/notes.md", content: "n\n" } },
    { kind: "text", text: "Tell me about your idea." },
  ]);
  try {
    await withEdge({ models: [model], roomSocket }, async (edge) => {
      const turnId = randomUUID();
      const res = await postTurnSocket(edge.turnSocket, { turnId, project: PROJECT, kind: "start", credit: ANN, text: "a greeting service" });
      assert.equal(res.status, 200);
      assert.deepEqual(withoutKeepAlives(await res.rest()), [{ type: "result", status: "completed" }]);

      // The socket is the agent's identity: the auth message carries no token.
      assert.deepEqual(joins, [{ room: "spec-acme-greeter", token: "", credit: '{"name":"Ann","email":"ann@x"}' }]);
      assert.equal(readDocFile(doc!, "specs/requirements/notes.md")?.trim(), "n", "the kickoff wrote through the Room");

      const status = edge.desk.status(turnId)!;
      assert.equal(status.kind, "kickoff");
      assert.equal(status.flow, "start");
      assert.equal(status.instruction, "/start a greeting service");
      assert.equal(status.authorId, "u-ann");
      assert.equal(status.conversationId, edge.threads.current(PROJECT).conversationId, "the project's current thread");
      assert.match(JSON.stringify(model.doStreamCalls[0]!.prompt), /a greeting service/);

      // Browsers can watch it: the same turn on /v1.
      const frames = await streamOf(edge, await edge.token(), turnId);
      assert.equal(frames.at(-2)?.data.type, "turn-completed");

      await until(() => edge.tools.usage.length === 1, "the usage record");
      assert.deepEqual(edge.tools.usage[0]!.author, { id: "u-ann", name: "Ann" });
      assert.equal(edge.tools.usage[0]!.kind, "kickoff");
    });
  } finally {
    await collab.destroy();
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a turn whose Room writes did not all land ends failed room_unavailable, not completed", () =>
  withEdge(
    {
      models: [
        mockModel([
          { kind: "toolCall", toolCallId: "a1", toolName: "addFile", input: { path: "specs/requirements/notes.md", content: "n\n" } },
          { kind: "text", text: "done" },
        ]),
      ],
      // The Room dropped mid-turn and the rejoin gave up: one write is not in it.
      room: async () => ({
        files: () => ({ "specs/requirements/prd.md": "# PRD\n" }),
        set: () => {},
        delete: () => {},
        leave: async () => 1,
      }),
    },
    async (edge) => {
      const turnId = randomUUID();
      const res = await postTurnSocket(edge.turnSocket, { turnId, project: PROJECT, kind: "start", credit: ANN });
      const end = withoutKeepAlives(await res.rest()).at(-1)!;
      assert.equal(end.status, "failed");
      assert.equal(end.code, "room_unavailable");
      assert.match(String(end.message), /1 file\(s\) written this turn are not in it/);
      const frames = await streamOf(edge, await edge.token(), turnId);
      assert.deepEqual(
        { type: frames.at(-2)?.data.type, reason: frames.at(-2)?.data.reason, code: frames.at(-2)?.data.code },
        { type: "turn-failed", reason: "agent-error", code: "room_unavailable" },
      );
      await until(() => edge.tools.usage.length === 1, "the usage record");
      assert.deepEqual([edge.tools.usage[0]!.status, edge.tools.usage[0]!.reason, edge.tools.usage[0]!.code], ["failed", "agent-error", "room_unavailable"]);
    },
  ));

test("the credit parameter names the user id when the credit has no name (the Room socket needs a name)", () => {
  assert.equal(creditParameter({ userId: "u-ann", name: "", email: "" }), '{"name":"u-ann","email":""}');
  assert.equal(creditParameter({ userId: "u-ann", name: "  ", email: "a@x" }), '{"name":"u-ann","email":"a@x"}');
  assert.equal(creditParameter(ANN), '{"name":"Ann","email":"ann@x"}');
});

test("the same turnId reattaches (one turn), a different one is 409, and a finished turn answers its result again", () => {
  // The turn stays running until the test releases it, so the 409 below is
  // asserted against a running turn however slow the host is.
  let release!: () => void;
  const hold = new Promise<void>((resolve) => (release = resolve));
  const models: LanguageModel[] = [mockModel([{ kind: "text", text: "slow" }], { hold })];
  let built = 0;
  return withEdge(
    {
      buildModel: () => {
        built++;
        const m = models.shift();
        if (!m) throw new Error("a second turn was started");
        return m;
      },
    },
    async (edge) => {
      const turnId = randomUUID();
      const body = { turnId, project: PROJECT, kind: "start", credit: ANN };
      const first = await postTurnSocket(edge.turnSocket, body);
      const second = await postTurnSocket(edge.turnSocket, body);
      assert.deepEqual([first.status, second.status], [200, 200]);

      const other = await postTurnSocket(edge.turnSocket, { ...body, turnId: randomUUID() });
      assert.equal(other.status, 409);
      assert.equal(other.contentType?.startsWith("application/json"), true);
      assert.deepEqual(await other.json(), { code: "turn_in_progress", activeTurnId: turnId });

      release();
      const [a, b] = await Promise.all([first.rest(), second.rest()]);
      assert.deepEqual(withoutKeepAlives(a), [{ type: "result", status: "completed" }]);
      assert.deepEqual(withoutKeepAlives(b), [{ type: "result", status: "completed" }]);
      assert.equal(built, 1, "one turn");

      // After the end: the retry gets the final result, and starts nothing.
      const again = await postTurnSocket(edge.turnSocket, body);
      assert.deepEqual(withoutKeepAlives(await again.rest()), [{ type: "result", status: "completed" }]);
      assert.equal(built, 1);

      // The turn id is the project's: another project cannot reattach to it.
      const foreign = await postTurnSocket(edge.turnSocket, { ...body, project: "other" });
      assert.equal(foreign.status, 400);
    },
  );
});

test("a caller that goes away leaves the turn running", () =>
  withEdge({ models: [mockModel([{ kind: "text", text: "done" }], { delayMs: 200 })] }, async (edge) => {
    const turnId = randomUUID();
    const controller = new AbortController();
    const { Agent, fetch } = await import("undici");
    const res = await fetch("http://turn.sock/turns", {
      method: "POST",
      dispatcher: new Agent({ connect: { socketPath: edge.turnSocket } }),
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ turnId, project: PROJECT, kind: "start", credit: ANN }),
      signal: controller.signal,
    });
    assert.equal(res.status, 200);
    controller.abort();
    await until(() => edge.desk.status(turnId)?.status === "completed", "the turn to complete without its caller");
  }));

test("keep-alive lines while the turn is quiet", () =>
  withEdge({ models: [mockModel([{ kind: "text", text: "late" }], { delayMs: 200 })], keepAliveMs: 20 }, async (edge) => {
    const res = await postTurnSocket(edge.turnSocket, { turnId: randomUUID(), project: PROJECT, kind: "start", credit: ANN });
    const lines = (await res.rest()).map((l) => JSON.parse(l) as Record<string, unknown>);
    assert.ok(lines.filter((f) => f.type === "keep-alive").length >= 2, "keep-alives");
    assert.deepEqual(lines.at(-1), { type: "result", status: "completed" });
    assert.ok(lines.slice(0, -1).every((f) => f.type === "keep-alive"));
  }));

test("refusals: invalid body 400, unknown project 404, no key 409, shutting down 503", async () => {
  await withEdge({}, async (edge) => {
    const bad = await postTurnSocket(edge.turnSocket, { turnId: "x" });
    assert.equal(bad.status, 400);
    assert.equal((await bad.json()).code, "invalid_turn");
    const notJson = await postTurnSocket(edge.turnSocket, "{");
    assert.equal(notJson.status, 400);
    assert.equal((await notJson.json()).code, "invalid_turn");
    const unknown = await postTurnSocket(edge.turnSocket, { turnId: randomUUID(), project: "nope", kind: "plan", credit: NOBODY });
    assert.equal(unknown.status, 404);
    assert.equal((await unknown.json()).code, "project_unknown");
    edge.turns.refuse();
    const late = await postTurnSocket(edge.turnSocket, { turnId: randomUUID(), project: PROJECT, kind: "plan", credit: NOBODY });
    assert.equal(late.status, 503);
    assert.equal((await late.json()).code, "shutting_down");
    // The edge refuses alike.
    const tok = await edge.token();
    const conv = (await (await call(edge, `/v1/projects/${PROJECT}/conversations/current`, tok)).json()) as { conversationId: string };
    assert.equal((await call(edge, `/v1/projects/${PROJECT}/conversations/${conv.conversationId}/turns`, tok, { json: { instruction: "hi" } })).status, 503);
  });
  await withEdge({ connection: null }, async (edge) => {
    const noKey = await postTurnSocket(edge.turnSocket, { turnId: randomUUID(), project: PROJECT, kind: "start", credit: ANN });
    assert.equal(noKey.status, 409);
    assert.equal(noKey.contentType, "application/problem+json");
    assert.equal((await noKey.json()).code, "no_default_key");
  });
});

test("the Turn socket is 0660, a stale socket file is replaced, any other file refuses the start", async () => {
  const dir = mkdtempSync(join(tmpdir(), "ae-turnsock-"));
  const path = join(dir, "turn.sock");
  try {
    // A socket file a killed process left behind (the emptyDir outlives a container restart).
    spawnSync(process.execPath, ["-e", `require("net").createServer().listen(${JSON.stringify(path)}, () => process.kill(process.pid, "SIGKILL"))`]);
    assert.ok(statSync(path).isSocket(), "a stale socket file");
    await withEdge({ turnSocket: path }, async (edge) => {
      assert.equal(statSync(path).mode & 0o777, 0o660);
      assert.equal((await fetch(`${edge.healthUrl}/readyz`)).status, 200);
      assert.ok(edge.logs.some((l) => l.msg === "pod_turn_socket_listening"));
      const bad = await postTurnSocket(path, {});
      assert.equal(bad.status, 400, "served on the replaced socket");
    });
    writeFileSync(path, "not a socket");
    await assert.rejects(startEdge({ turnSocket: path }), /exists and is not a socket/);
    assert.equal(readFileSync(path, "utf8"), "not a socket", "never removed");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
