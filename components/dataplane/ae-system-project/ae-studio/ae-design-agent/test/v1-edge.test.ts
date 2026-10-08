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
 * The `/v1` edge on the pod's public listener: a turn starts
 * with 202 and runs detached, the stream attaches with `?from`, a busy
 * project is 409 `turn_in_progress`, credit comes from the verified user
 * token, marketplace conversations belong to their creator, and the body
 * caps hold. Driven over HTTP (listen(0) + fetch) through the real pod gate.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { rmSync } from "node:fs";
import { join } from "node:path";
import { mockModel } from "../src/shared/mock-model.js";
import type { RoomPeer } from "../src/collab/room-peer.js";
import type { Credit } from "../src/turns/start-turn.js";
import { CONNECTION, HEAD, PROJECT, call, readSse, startEdge, startTurn, streamOf, type Edge } from "./helpers/edge.js";

const text = (t: string) => mockModel([{ kind: "text", text: t }]);
const slow = () => mockModel([{ kind: "text", text: "slow" }], { delayMs: 400 });

async function until(cond: () => boolean, what: string): Promise<void> {
  for (let i = 0; i < 200; i++) {
    if (cond()) return;
    await new Promise((r) => setTimeout(r, 10));
  }
  assert.fail(`timed out waiting for ${what}`);
}

async function json<T = Record<string, unknown>>(res: Response): Promise<T> {
  return (await res.json()) as T;
}

async function withEdge(opts: Parameters<typeof startEdge>[0], body: (edge: Edge) => Promise<void>): Promise<void> {
  const edge = await startEdge(opts);
  try {
    await body(edge);
  } finally {
    await edge.close();
  }
}

test("POST turns returns 202, runs detached, and the stream attaches with ?from", () =>
  withEdge({ models: [text("hello")] }, async (edge) => {
    const tok = await edge.token({ sub: "u1", name: "Ann", email: "ann@x" });
    const res = await startTurn(edge, tok, { instruction: "hi" });
    assert.equal(res.status, 202);
    const { turnId } = await json<{ turnId: string }>(res);
    const frames = await streamOf(edge, tok, turnId, 0);
    assert.equal(frames.at(-2)?.data.type, "turn-completed");
    assert.equal(frames.at(-1)?.raw, "[DONE]");
    assert.ok(frames.every((f, i) => f.id === i), "frame ids are the buffer indices, no gap");
    assert.ok(frames.some((f) => f.data?.type === "text-delta"));
    assert.equal(frames.some((f) => f.data?.type === "manifest"), false, "no manifest frame on the wire");
    await until(() => edge.tools.usage.length === 1, "the usage record");
    const rec = edge.tools.usage[0]!;
    assert.deepEqual(rec.author, { id: "u1", name: "Ann" });
    assert.equal(rec.kind, "browser");
    assert.equal(rec.project, PROJECT);
    assert.equal(rec.status, "completed");
    assert.equal(rec.model, "claude-sonnet-5");
    assert.equal(rec.modelHost, "api.anthropic.com");
    assert.equal(rec.baseRef, "a".repeat(40));
    assert.equal(rec.contextTokens, 15, "the last finish-step's prompt + output");
    assert.deepEqual([rec.inputTokens, rec.outputTokens], [10, 5]);

    // Resume: from=3 yields frames 3.. exactly once, the same frames.
    const resumed = await streamOf(edge, tok, turnId, 3);
    assert.deepEqual(resumed.map((f) => f.id), frames.slice(3).map((f) => f.id));
    assert.deepEqual(resumed.map((f) => f.raw), frames.slice(3).map((f) => f.raw));
    // Last-Event-ID names the last frame seen; ?from wins over it.
    const byHeader = await call(edge, `/v1/projects/${PROJECT}/turns/${turnId}/stream`, tok, { headers: { "last-event-id": "4" } });
    assert.equal((await readSse(byHeader)).frames[0]?.id, 5);

    const status = await json(await call(edge, `/v1/projects/${PROJECT}/turns/${turnId}`, tok));
    assert.equal(status.status, "completed");
    assert.equal(status.authorId, "u1");
    assert.equal(status.authorDisplayName, "Ann");
    assert.equal(status.instruction, "hi");
    assert.equal(status.flow, "");
    assert.equal(status.kind, "browser");
    assert.equal((await call(edge, `/v1/projects/${PROJECT}/turns/active`, tok)).status, 204);
  }));

test("409 turn_in_progress with activeTurnId; wrong org 403; M2M token 401; marketplace sub must match", () =>
  withEdge({ models: [slow()] }, async (edge) => {
    const ann = await edge.token({ sub: "u1" });
    const first = await startTurn(edge, ann);
    assert.equal(first.status, 202);
    const { turnId } = await json<{ turnId: string }>(first);
    const second = await startTurn(edge, ann);
    assert.equal(second.status, 409);
    assert.equal(second.headers.get("content-type")?.startsWith("application/json"), true);
    assert.deepEqual(await json(second), { code: "turn_in_progress", activeTurnId: turnId });
    const active = await call(edge, `/v1/projects/${PROJECT}/turns/active`, ann);
    assert.equal(active.status, 200);
    assert.equal((await json(active)).turnId, turnId);
    // A rotation under the running turn waits for it.
    const rotate = await call(edge, `/v1/projects/${PROJECT}/conversations`, ann, { method: "POST" });
    assert.equal(rotate.status, 409);
    assert.deepEqual(await json(rotate), { code: "turn_in_progress", activeTurnId: turnId });

    assert.equal((await startTurn(edge, await edge.token({ ouHandle: "evil" }))).status, 403);
    assert.equal((await startTurn(edge, await edge.m2m("ae-internal"))).status, 401);
    assert.equal((await startTurn(edge, await edge.m2m("publisher"))).status, 401);

    const conv = await json<{ conversationId: string }>(await call(edge, "/v1/marketplace/conversations", ann, { method: "POST" }));
    const bob = await edge.token({ sub: "u2" });
    assert.equal((await call(edge, `/v1/marketplace/conversations/${conv.conversationId}/messages`, bob)).status, 404);
    assert.equal((await call(edge, `/v1/marketplace/conversations/${conv.conversationId}/turns`, bob, { json: { instruction: "x" } })).status, 404);
    const own = await call(edge, `/v1/marketplace/conversations/${conv.conversationId}/messages`, ann);
    assert.deepEqual(await json(own), { messages: [] });
  }));

test("no key: turn POST answers problem no_default_key", () =>
  withEdge({ connection: null }, async (edge) => {
    const res = await startTurn(edge, await edge.token());
    assert.equal(res.status, 409);
    assert.equal(res.headers.get("content-type"), "application/problem+json");
    assert.equal((await json(res)).code, "no_default_key");
    assert.equal(edge.tools.lookups.length, 0, "refused before the lookup");
  }));

test("ref_not_found from a lookup the chat turn sent without `at` is the sidecar's 503 tools_unavailable, not a 400 about `at`", () =>
  withEdge({}, async (edge) => {
    edge.tools.failNextLookup("ref_not_found");
    const res = await startTurn(edge, await edge.token());
    assert.equal(res.status, 503);
    assert.equal((await json(res)).code, "tools_unavailable");
    assert.deepEqual(edge.tools.lookups, [{ project: PROJECT }], "the chat turn sent no at");
  }));

test("a snapshot that will not read is 500 internal with a fixed detail; the log names the class only (R2-M6)", () =>
  withEdge({}, async (edge) => {
    rmSync(join(edge.snapshotsDir, "projects", PROJECT, HEAD), { recursive: true, force: true });
    const res = await startTurn(edge, await edge.token());
    assert.equal(res.status, 500);
    const body = await json(res);
    assert.equal(body.code, "internal");
    assert.equal(body.detail, "the turn's files could not be read");
    assert.equal(JSON.stringify(edge.turnLogs).includes(edge.snapshotsDir), false, "no path in the log");
    assert.deepEqual(edge.turnLogs, [{ msg: "turn_material_unreadable", source: "ae-design-agent", errorClass: "SnapshotPathError" }]);
  }));

test("a client that leaves the stream does not end the turn", () =>
  withEdge({ models: [slow()] }, async (edge) => {
    const tok = await edge.token();
    const { turnId } = await json<{ turnId: string }>(await startTurn(edge, tok));
    const ac = new AbortController();
    const res = await fetch(`${edge.base}/v1/projects/${PROJECT}/turns/${turnId}/stream?from=0`, {
      headers: { authorization: `Bearer ${tok}` },
      signal: ac.signal,
    });
    assert.equal(res.status, 200);
    ac.abort();
    await until(() => edge.desk.status(turnId)?.status === "completed", "the detached turn to complete");
  }));

test("conversation ids: a demoted id is 409 conversation_rotated and takes no lock; messages and rotation", () =>
  withEdge({ models: [text("one")] }, async (edge) => {
    const tok = await edge.token({ sub: "u1", name: "Ann" });
    const cur = await json<{ conversationId: string; createdBy: string; current: boolean }>(
      await call(edge, `/v1/projects/${PROJECT}/conversations/current`, tok),
    );
    assert.equal(cur.createdBy, "Ann");
    assert.equal(cur.current, true);
    assert.deepEqual(await json(await call(edge, `/v1/projects/${PROJECT}/conversations/${cur.conversationId}/messages`, tok)), {
      messages: [],
    });
    const { turnId } = await json<{ turnId: string }>(
      await call(edge, `/v1/projects/${PROJECT}/conversations/${cur.conversationId}/turns`, tok, { json: { instruction: "first" } }),
    );
    await streamOf(edge, tok, turnId);
    const { messages } = await json<{ messages: Array<{ role: string; author?: unknown; content?: unknown }> }>(
      await call(edge, `/v1/projects/${PROJECT}/conversations/${cur.conversationId}/messages`, tok),
    );
    assert.deepEqual(messages[0], { role: "user", content: "first", author: { id: "u1", displayName: "Ann" } });

    const rotated = await call(edge, `/v1/projects/${PROJECT}/conversations`, tok, { method: "POST" });
    assert.equal(rotated.status, 201);
    const fresh = await json<{ conversationId: string }>(rotated);
    assert.notEqual(fresh.conversationId, cur.conversationId);
    assert.equal((await call(edge, `/v1/projects/${PROJECT}/conversations/${cur.conversationId}/messages`, tok)).status, 404);
    const stale = await call(edge, `/v1/projects/${PROJECT}/conversations/${cur.conversationId}/turns`, tok, { json: { instruction: "x" } });
    assert.equal(stale.status, 409);
    assert.deepEqual(await json(stale), { code: "conversation_rotated" });
    assert.equal(edge.desk.active({ kind: "project", project: PROJECT }), null);
  }));

test("auto-rotation: a send to a thread past 80% of the window is refused conversation_rotated, with no turn left running", () =>
  // The mock reports 10 in + 5 out per step: a 15-token context, past 80 % of 18.
  withEdge({ models: [text("one")], connection: { ...CONNECTION, contextWindow: 18 } }, async (edge) => {
    const tok = await edge.token();
    const { turnId } = await json<{ turnId: string }>(await startTurn(edge, tok));
    await streamOf(edge, tok, turnId);
    const again = await startTurn(edge, tok);
    assert.equal(again.status, 409);
    assert.deepEqual(await json(again), { code: "conversation_rotated" });
    assert.equal(edge.desk.active({ kind: "project", project: PROJECT }), null);
    assert.equal((await call(edge, `/v1/projects/${PROJECT}/turns/active`, tok)).status, 204);
  }));

test("turn status and stream answer only under the turn's own project; unknown projects and turns are 404", () =>
  withEdge({ models: [text("x")] }, async (edge) => {
    const tok = await edge.token();
    const { turnId } = await json<{ turnId: string }>(await startTurn(edge, tok));
    await streamOf(edge, tok, turnId);
    for (const path of [`/v1/projects/other/turns/${turnId}`, `/v1/projects/other/turns/${turnId}/stream`, `/v1/projects/${PROJECT}/turns/00000000-0000-4000-8000-000000000000`]) {
      const res = await call(edge, path, tok);
      assert.equal(res.status, 404, path);
      assert.equal((await json(res)).code, "turn_unknown");
    }
    const unknown = await startTurn(edge, tok, { instruction: "hi" }, "nope");
    assert.equal(unknown.status, 404);
    assert.equal((await json(unknown)).code, "project_unknown");
    const bad = await call(edge, "/v1/projects/Not_A_Label/conversations/current", tok);
    assert.equal(bad.status, 404);
    assert.equal((await json(bad)).code, "project_unknown");
    assert.equal((await call(edge, "/v1/unknown", tok)).status, 404);
  }));

test("turn input: bad bodies are 400, and an instruction past 64 KiB is 413; none starts a turn", () =>
  withEdge({}, async (edge) => {
    const tok = await edge.token();
    const cases: Array<[unknown, number, string]> = [
      [{ instruction: "  " }, 400, "invalid_turn"],
      [{ instruction: "hi", collab: true }, 400, "invalid_turn"],
      [{ instruction: "hi", turn: { kind: "chat" } }, 400, "invalid_turn"],
      [{ instruction: 1 }, 400, "invalid_turn"],
      [{ instruction: "hi", intent: "change" }, 400, "invalid_turn"],
      [{ instruction: "hi", anchor: { file: "a.md", nodes: [] }, intent: "change" }, 400, "invalid_turn"],
      [{ instruction: "x".repeat((64 << 10) + 1) }, 413, "payload_too_large"],
    ];
    for (const [body, status, code] of cases) {
      const res = await startTurn(edge, tok, body);
      assert.equal(res.status, status, JSON.stringify(body).slice(0, 80));
      assert.equal((await json(res)).code, code);
    }
    assert.equal(edge.desk.active({ kind: "project", project: PROJECT }), null);
  }));

/** A multipart turn body with `files` (name → bytes). */
function multipart(instruction: string, files: Record<string, Buffer | string>, extra: Record<string, string> = {}): FormData {
  const form = new FormData();
  form.set("instruction", instruction);
  for (const [k, v] of Object.entries(extra)) form.set(k, v);
  for (const [name, bytes] of Object.entries(files)) form.append("files", new Blob([typeof bytes === "string" ? bytes : new Uint8Array(bytes)]), name);
  return form;
}

async function postForm(edge: Edge, tok: string, form: FormData): Promise<Response> {
  const conv = await json<{ conversationId: string }>(await call(edge, `/v1/projects/${PROJECT}/conversations/current`, tok));
  return call(edge, `/v1/projects/${PROJECT}/conversations/${conv.conversationId}/turns`, tok, { body: form });
}

test("multipart: an attachment reaches the model and the journal; the caps answer 413, the rules 400", () =>
  withEdge({ models: [text("read it")] }, async (edge) => {
    const tok = await edge.token({ sub: "u1", name: "Ann" });
    const anchor = JSON.stringify({ file: "specs/requirements/prd.md", nodes: [{ name: "Goals", kind: "heading" }] });
    const ok = await postForm(edge, tok, multipart("see notes", { "notes.md": "# Notes\n" }, { anchor, intent: "discuss" }));
    assert.equal(ok.status, 202);
    const { turnId } = await json<{ turnId: string }>(ok);
    await streamOf(edge, tok, turnId);
    const conv = edge.threads.current(PROJECT);
    const stored = await edge.store.get(conv.conversationId);
    assert.deepEqual(stored?.turns[0]?.attachments, ["notes.md"]);
    assert.deepEqual(stored?.turns[0]?.anchor, JSON.parse(anchor));

    const MiB = 1 << 20;
    const big = Buffer.alloc(5 * MiB + 1, 97);
    const four = Buffer.alloc(4 * MiB, 97);
    const cases: Array<[FormData, number, string]> = [
      [multipart("x", { "a.md": big }), 413, "payload_too_large"],
      [multipart("x", { "a.md": four, "b.md": four, "c.md": four, "d.md": four }), 413, "payload_too_large"],
      [multipart("x", { "a.docx": "x" }), 400, "attachment_rejected"],
      [multipart("x", { "a.md": "x", "dir/a.md": "y" }), 400, "attachment_rejected"],
      [multipart("x", Object.fromEntries(Array.from({ length: 11 }, (_, i) => [`f${i}.md`, "x"]))), 400, "attachment_rejected"],
      [multipart("", {}), 400, "invalid_turn"],
    ];
    for (const [form, status, code] of cases) {
      const res = await postForm(edge, tok, form);
      assert.equal(res.status, status, code);
      assert.equal((await json(res)).code, code);
    }
  }));

test("after shutdown begins a turn start is 503 shutting_down", () =>
  withEdge({}, async (edge) => {
    edge.turns.refuse();
    const res = await startTurn(edge, await edge.token());
    assert.equal(res.status, 503);
    assert.equal(res.headers.get("retry-after"), "5");
    assert.equal((await json(res)).code, "shutting_down");
  }));

test("a project turn joins the Room as the credited user, gets the MCP tools, and leaves", async () => {
  const joins: Array<{ project: string; credit: Credit }> = [];
  let left = 0;
  const written: Record<string, string> = {};
  const room = async (project: string, credit: Credit): Promise<RoomPeer> => {
    joins.push({ project, credit });
    return {
      files: () => ({ "specs/requirements/prd.md": "# PRD from the Room\n" }),
      set: (path, content) => void (written[path] = content),
      delete: () => {},
      leave: async () => {
        left++;
        return 0;
      },
    };
  };
  const model = mockModel([
    { kind: "toolCall", toolCallId: "a1", toolName: "addFile", input: { path: "specs/requirements/notes.md", content: "n\n" } },
    { kind: "text", text: "done" },
  ]);
  await withEdge({ models: [model], room }, async (edge) => {
    const tok = await edge.token({ sub: "u1", given_name: "Ann", family_name: "User", email: "ann@x" } as never);
    const { turnId } = await json<{ turnId: string }>(await startTurn(edge, tok));
    const frames = await streamOf(edge, tok, turnId);
    assert.equal(frames.at(-2)?.data.type, "turn-completed");
    assert.deepEqual(joins, [{ project: PROJECT, credit: { userId: "u1", name: "Ann", email: "ann@x" } }]);
    assert.equal(left, 1);
    assert.equal(written["specs/requirements/notes.md"], "n\n", "the write went to the Room");
    const tools = (model.doStreamCalls[0]!.tools ?? []).map((t) => t.name);
    assert.ok(tools.includes("list_external_resources"), "a Room turn gets the catalog tools");
    assert.ok(tools.includes("web_search"), "and web search");
    const prompt = JSON.stringify(model.doStreamCalls[0]!.prompt);
    assert.ok(prompt.includes("# PRD from the Room"), "the Room's doc is the turn's file source");
  });
});

test("marketplace: the owner's turn runs on the skills snapshot with the draft tool, no project, no Room", async () => {
  const joins: string[] = [];
  const model = mockModel([{ kind: "text", text: "draft" }]);
  await withEdge(
    { models: [model], room: async (p) => (joins.push(p), assert.fail("no Room for the marketplace")) },
    async (edge) => {
      const ann = await edge.token({ sub: "u1", name: "Ann" });
      const bob = await edge.token({ sub: "u2" });
      const created = await call(edge, "/v1/marketplace/conversations", ann, { method: "POST" });
      assert.equal(created.status, 201);
      const { conversationId } = await json<{ conversationId: string }>(created);
      const res = await call(edge, `/v1/marketplace/conversations/${conversationId}/turns`, ann, { json: { instruction: "register stripe" } });
      assert.equal(res.status, 202);
      const { turnId } = await json<{ turnId: string }>(res);
      const stream = await call(edge, `/v1/marketplace/turns/${turnId}/stream?from=0`, ann);
      const { frames } = await readSse(stream);
      assert.equal(frames.at(-2)?.data.type, "turn-completed");
      const status = await json(await call(edge, `/v1/marketplace/turns/${turnId}`, ann));
      assert.equal(status.conversationId, conversationId);
      assert.equal("project" in status, false);
      assert.equal((await call(edge, `/v1/marketplace/turns/${turnId}`, bob)).status, 404);
      assert.equal((await call(edge, `/v1/marketplace/turns/${turnId}/stream`, bob)).status, 404);
      assert.equal((await call(edge, `/v1/projects/${PROJECT}/turns/${turnId}`, ann)).status, 404);
      assert.deepEqual(joins, []);
      assert.equal(edge.tools.lookups.length, 0, "no project lookup");
      assert.ok((model.doStreamCalls[0]!.tools ?? []).some((t) => t.name === "draftExternalResource"));
      await until(() => edge.tools.usage.length === 1, "the usage record");
      assert.equal(edge.tools.usage[0]!.project, undefined);
      assert.equal(edge.tools.usage[0]!.baseRef, "b".repeat(40));
      const { messages } = await json<{ messages: unknown[] }>(await call(edge, `/v1/marketplace/conversations/${conversationId}/messages`, ann));
      assert.ok(messages.length >= 2);
    },
  );
});

test("an idle stream gets keep-alives, and closing the pod ends an attached stream", () =>
  withEdge({ models: [mockModel([{ kind: "text", text: "late" }], { delayMs: 60_000 })], keepAliveMs: 20 }, async (edge) => {
    const tok = await edge.token();
    const { turnId } = await json<{ turnId: string }>(await startTurn(edge, tok));
    const res = await call(edge, `/v1/projects/${PROJECT}/turns/${turnId}/stream?from=0`, tok);
    const reader = res.body!.getReader();
    let seen = "";
    while (!seen.includes(": keep-alive")) seen += new TextDecoder().decode((await reader.read()).value);
    // The pod's close() must not wait for the turn: it ends the connection.
    const closed = edge.close();
    for (;;) {
      try {
        if ((await reader.read()).done) break;
      } catch {
        break;
      }
    }
    await closed;
  }));

test("a design turn's usage record names the features its /design line scoped; a bare /design or another flow names none", async () => {
  const edge = await startEdge({ models: [1, 2, 3].map(() => mockModel([{ kind: "text", text: "ok" }])) });
  try {
    const tok = await edge.token();
    for (const instruction of ["/design F2 F1 F2", "/design", "/refine F2"]) {
      const res = await startTurn(edge, tok, { instruction });
      await streamOf(edge, tok, ((await res.json()) as { turnId: string }).turnId);
    }
    await until(() => edge.tools.usage.length === 3, "three usage records");
    assert.deepEqual(
      edge.tools.usage.map((r) => [r.flow, r.designFeatures]),
      [
        ["design", ["F2", "F1"]],
        ["design", undefined],
        ["refine", undefined],
      ],
    );
  } finally {
    await edge.close();
  }
});
