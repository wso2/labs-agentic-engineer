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
 * StreamingDocWriter unit tests — deterministic, no model. We hand-craft the
 * `tool-input-*` / `tool-result` parts (chunking the JSON args exactly like a
 * provider would) and assert what lands on a fake RoomPeer.
 */
import { test } from "node:test";
import assert from "node:assert/strict";
import { FileBundle, type StreamPart } from "@aep/agent-stream";
import type { RoomPeer } from "../src/collab/room-peer.js";
import { StreamingDocWriter } from "../src/collab/streaming-add.js";

class FakePeer implements RoomPeer {
  sets: { path: string; content: string; mark: boolean }[] = [];
  deletes: string[] = [];
  files(): Record<string, string> {
    return {};
  }
  set(path: string, content: string, mark: boolean): void {
    this.sets.push({ path, content, mark });
  }
  delete(path: string): void {
    this.deletes.push(path);
  }
  leave(): void {}
}

/** Chunk an addFile's JSON args into `tool-input-delta`s (start..deltas..end). */
function addFileParts(id: string, path: string, content: string, chunk = 7): StreamPart[] {
  const wire = JSON.stringify({ path, content });
  const parts: StreamPart[] = [{ type: "tool-input-start", id, toolName: "addFile" }];
  for (let i = 0; i < wire.length; i += chunk) {
    parts.push({ type: "tool-input-delta", id, delta: wire.slice(i, i + chunk) });
  }
  parts.push({ type: "tool-input-end", id });
  return parts;
}

function toolResult(id: string, ok: boolean, path: string): StreamPart {
  return {
    type: "tool-result",
    toolCallId: id,
    toolName: "addFile",
    input: { path },
    output: { ok, path, op: "add", status: ok ? "applied" : undefined },
  };
}

async function feed(writer: StreamingDocWriter, parts: StreamPart[]): Promise<void> {
  for (const p of parts) writer.observe(p);
  await writer.drain();
}

const MD_PATH = "specs/requirements/requirements.md";
const MD = "# Todo App\n\n## Overview\nManage todo items.\n\n## Rules\n- title\n- done flag\n";

test("streams markdown addFile incrementally, line-by-line, converging to full content", async () => {
  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({}));

  await feed(writer, addFileParts("c1", MD_PATH, MD));
  writer.observe(toolResult("c1", true, MD_PATH));
  await writer.drain();

  assert.ok(peer.sets.length >= 2, `expected incremental writes, got ${peer.sets.length}`);
  assert.ok(peer.sets.every((s) => s.path === MD_PATH), "all writes target the same path");
  // Streaming previews a newly-created file (addFile) → every write is UNMARKED,
  // so the highlighted tail is never re-rendered per flush (the flicker fix).
  assert.ok(
    peer.sets.every((s) => s.mark === false),
    "streamed addFile writes must be unmarked",
  );

  // Monotonic prefix growth: each write extends the previous.
  for (let i = 1; i < peer.sets.length; i++) {
    assert.ok(
      peer.sets[i]!.content.startsWith(peer.sets[i - 1]!.content),
      `write ${i} must extend write ${i - 1}`,
    );
  }
  // At least one intermediate write ends on a line boundary (proves line flushing).
  assert.ok(
    peer.sets.slice(0, -1).some((s) => s.content.endsWith("\n")),
    "expected a line-boundary intermediate write",
  );
  // Final write is the whole body; nothing rolled back.
  assert.equal(peer.sets.at(-1)!.content, MD);
  assert.deepEqual(peer.deletes, []);
  assert.deepEqual(writer.rollbackDangling(), []);
});

const CELL_PATH = "specs/design/design.cell";
const CELL =
  "title Shop\n\ncomponent web as \"Web\" web-application\ncomponent api as \"API\" service\n\nweb -> api\n";

test("streams a .cell addFile incrementally, line-by-line, converging to full content", async () => {
  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({}));

  await feed(writer, addFileParts("c1", CELL_PATH, CELL));
  writer.observe(toolResult("c1", true, CELL_PATH));
  await writer.drain();

  assert.ok(peer.sets.length >= 2, `expected incremental writes, got ${peer.sets.length}`);
  assert.ok(peer.sets.every((s) => s.path === CELL_PATH), "all writes target design.cell");
  for (let i = 1; i < peer.sets.length; i++) {
    assert.ok(
      peer.sets[i]!.content.startsWith(peer.sets[i - 1]!.content),
      `write ${i} must extend write ${i - 1}`,
    );
  }
  assert.equal(peer.sets.at(-1)!.content, CELL, "final write is the whole DSL body");
  assert.deepEqual(peer.deletes, []);
  assert.deepEqual(writer.rollbackDangling(), []);
});

const DSL_PATH = "specs/design/components/shop-webapp/wireframes.dsl";
const DSL =
  'screen Catalog "Shoppers browse products"\n  navbar "Shop"\n  heading "Browse"\n  button "View cart" primary -> Cart\n\nscreen Cart "Review and check out"\n  heading "Your cart"\n';

test("streams a wireframes .dsl addFile incrementally, line-by-line, converging to full content", async () => {
  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({}));

  await feed(writer, addFileParts("c1", DSL_PATH, DSL));
  writer.observe(toolResult("c1", true, DSL_PATH));
  await writer.drain();

  assert.ok(peer.sets.length >= 2, `expected incremental writes, got ${peer.sets.length}`);
  assert.ok(peer.sets.every((s) => s.path === DSL_PATH), "all writes target wireframes.dsl");
  for (let i = 1; i < peer.sets.length; i++) {
    assert.ok(
      peer.sets[i]!.content.startsWith(peer.sets[i - 1]!.content),
      `write ${i} must extend write ${i - 1}`,
    );
  }
  assert.equal(peer.sets.at(-1)!.content, DSL, "final write is the whole DSL body");
  assert.deepEqual(peer.deletes, []);
  assert.deepEqual(writer.rollbackDangling(), []);
});

const OA_PATH = "specs/design/components/shop-api/openapi.yaml";
const OA =
  'openapi: 3.0.3\ninfo:\n  title: Shop API\n  version: 1.0.0\npaths:\n  /products:\n    get:\n      summary: List products\n      responses:\n        "200":\n          description: OK\n';

test("streams an openapi.yaml addFile incrementally, line-by-line, converging to full content", async () => {
  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({}));

  await feed(writer, addFileParts("c1", OA_PATH, OA));
  writer.observe(toolResult("c1", true, OA_PATH));
  await writer.drain();

  assert.ok(peer.sets.length >= 2, `expected incremental writes, got ${peer.sets.length}`);
  assert.ok(peer.sets.every((s) => s.path === OA_PATH), "all writes target openapi.yaml");
  for (let i = 1; i < peer.sets.length; i++) {
    assert.ok(
      peer.sets[i]!.content.startsWith(peer.sets[i - 1]!.content),
      `write ${i} must extend write ${i - 1}`,
    );
  }
  assert.equal(peer.sets.at(-1)!.content, OA, "final write is the whole YAML body");
  assert.deepEqual(peer.deletes, []);
  assert.deepEqual(writer.rollbackDangling(), []);
});

test("rolls back a truncated .cell stream", async () => {

  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({}));
  const parts = addFileParts("c1", CELL_PATH, CELL);
  await feed(writer, parts.slice(0, Math.floor(parts.length * 0.6)));
  assert.ok(peer.sets.length >= 1, "should have optimistically written a partial preview");
  assert.deepEqual(writer.rollbackDangling(), [CELL_PATH]);
  assert.deepEqual(peer.deletes, [CELL_PATH]);
});

test("path resolves before any content is written", async () => {
  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({}));
  await feed(writer, addFileParts("c1", MD_PATH, MD));
  // The very first write already knows the correct path (path streamed first).
  assert.ok(peer.sets.length > 0);
  assert.equal(peer.sets[0]!.path, MD_PATH);
});

test("skips non-streamable addFile (execute owns design.json — no preview)", async () => {
  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({}));
  await feed(writer, addFileParts("c1", "specs/design/components/x/design.json", '{"name":"x"}\n'));
  writer.observe(toolResult("c1", true, "specs/design/components/x/design.json"));
  await writer.drain();
  assert.deepEqual(peer.sets, []);
  assert.deepEqual(peer.deletes, []);
});

test("skips addFile onto an already-existing path", async () => {
  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({ [MD_PATH]: "old" }));
  await feed(writer, addFileParts("c1", MD_PATH, MD));
  assert.deepEqual(peer.sets, []);
});

test("rolls back a truncated stream (no tool-input-end, no tool-result)", async () => {
  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({}));

  const parts = addFileParts("c1", MD_PATH, MD);
  // Drop the tool-input-end and everything is left dangling mid-body.
  const truncated = parts.slice(0, Math.floor(parts.length * 0.6));
  await feed(writer, truncated);

  assert.ok(peer.sets.length >= 1, "should have optimistically written a partial preview");
  const dropped = writer.rollbackDangling();
  assert.deepEqual(dropped, [MD_PATH]);
  assert.deepEqual(peer.deletes, [MD_PATH]);
});

test("rolls back when execute rejects the op (tool-result ok:false)", async () => {
  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({}));
  await feed(writer, addFileParts("c1", MD_PATH, MD));
  writer.observe(toolResult("c1", false, MD_PATH));
  await writer.drain();

  assert.deepEqual(peer.deletes, [MD_PATH], "rejected op must undo the optimistic preview");
  assert.deepEqual(writer.rollbackDangling(), [], "already finalized — no double drop");
});

test("handles two concurrent addFile calls independently (keyed by id)", async () => {
  const peer = new FakePeer();
  const writer = new StreamingDocWriter(peer, new FileBundle({}));
  const p1 = addFileParts("c1", "specs/requirements/a.md", "# A\nalpha\n");
  const p2 = addFileParts("c2", "specs/requirements/b.md", "# B\nbeta\n");
  // Interleave the two streams.
  const merged: StreamPart[] = [];
  for (let i = 0; i < Math.max(p1.length, p2.length); i++) {
    if (p1[i]) merged.push(p1[i]!);
    if (p2[i]) merged.push(p2[i]!);
  }
  await feed(writer, merged);
  writer.observe(toolResult("c1", true, "specs/requirements/a.md"));
  writer.observe(toolResult("c2", true, "specs/requirements/b.md"));
  await writer.drain();

  assert.equal(peer.sets.filter((s) => s.path === "specs/requirements/a.md").at(-1)!.content, "# A\nalpha\n");
  assert.equal(peer.sets.filter((s) => s.path === "specs/requirements/b.md").at(-1)!.content, "# B\nbeta\n");
  assert.deepEqual(peer.deletes, []);
});
