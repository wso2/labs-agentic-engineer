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
import { pumpEvents } from "./event_queue.js";
import { createStreamCloser, sessionStream } from "./settle.js";

const ev = (type: string, properties: Record<string, unknown> = {}) => ({ type, properties });
const created = (id: string, parentID?: string) => ev("session.created", { info: { id, ...(parentID ? { parentID } : {}) } });
const status = (sessionID: string, type: string) => ev("session.status", { sessionID, status: { type } });
const idle = (sessionID: string) => ev("session.idle", { sessionID });

test("close rule: the root's idle, with nothing else busy and nothing asked, closes the stream", () => {
  const closer = createStreamCloser();
  assert.equal(closer.observe(created("root")), false);
  assert.equal(closer.observe(status("root", "busy")), false);
  // The status half of the idle pair does NOT close: the classifier ends the
  // turn on `session.idle`, which follows it.
  assert.equal(closer.observe(status("root", "idle")), false);
  assert.equal(closer.observe(idle("root")), true);
});

test("close rule: a busy child, or a pending permission, holds the stream open", () => {
  const closer = createStreamCloser();
  closer.observe(created("root"));
  closer.observe(status("root", "busy"));
  closer.observe(created("kid", "root"));
  closer.observe(status("kid", "busy"));
  closer.observe(ev("permission.asked", { id: "per_1", sessionID: "kid" }));
  assert.equal(closer.observe(idle("root")), false, "a child is still busy");
  assert.deepEqual(closer.busySessions(), ["kid"]);
  assert.equal(closer.observe(idle("kid")), false, "a permission is still unanswered");
  assert.equal(closer.observe(ev("permission.replied", { sessionID: "kid", requestID: "per_1", reply: "reject" })), true);
});

test("close rule: a retrying root is still working", () => {
  const closer = createStreamCloser();
  closer.observe(created("root"));
  closer.observe(status("root", "retry"));
  assert.deepEqual(closer.busySessions(), ["root"]);
});

test("sessionStream: declares the skills first, rejects asks, yields the closing event, then ends", async () => {
  const asked: unknown[] = [];
  async function* bus() {
    yield created("root");
    yield ev("permission.asked", { id: "per_9", sessionID: "root" });
    yield ev("permission.replied", { sessionID: "root", requestID: "per_9" });
    yield status("root", "busy");
    yield idle("root");
    yield ev("server.heartbeat");
  }
  const out: unknown[] = [];
  for await (const m of sessionStream(bus(), createStreamCloser(), { skills: ["aep"], onPermissionAsked: (p) => asked.push(p.id) })) {
    out.push(m);
  }
  assert.deepEqual(out[0], { type: "aep.skills", skills: ["aep"] });
  assert.deepEqual(out.at(-1), idle("root"), "the root's idle is read before the stream ends");
  assert.equal(out.length, 6);
  assert.deepEqual(asked, ["per_9"]);
});

test("pumpEvents: reads eagerly, resolves on the first event, merges the clock, ends with the bus", async () => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => (release = resolve));
  let pulled = 0;
  async function* bus() {
    pulled++;
    yield ev("server.connected");
    await gate;
    yield ev("session.created");
  }
  const q = pumpEvents(bus(), { tickMs: 5 });
  // Nobody has read the queue yet, and the bus is already being read.
  await q.connected;
  assert.equal(pulled, 1);
  await new Promise((r) => setTimeout(r, 30));
  release();
  const types: string[] = [];
  for await (const m of q.messages) types.push((m as { type: string }).type);
  assert.equal(types[0], "server.connected");
  assert.ok(types.includes("aep.tick"), "the clock ticked while the bus was quiet");
  assert.equal(types.at(-1), "session.created");
});

test("pumpEvents: a bus that fails is a failed stream, and a bus that never connects rejects `connected`", async () => {
  async function* broken(): AsyncGenerator<unknown> {
    yield ev("server.connected");
    throw new Error("socket hang up");
  }
  const q = pumpEvents(broken(), { tickMs: 0 });
  await q.connected;
  await assert.rejects(
    (async () => {
      for await (const m of q.messages) void m;
    })(),
    /socket hang up/,
  );
  async function* empty(): AsyncGenerator<unknown> {}
  await assert.rejects(pumpEvents(empty(), { tickMs: 0 }).connected, /ended before it connected/);
});
