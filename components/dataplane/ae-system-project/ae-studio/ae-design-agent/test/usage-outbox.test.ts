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
 * The usage outbox (07 §7): a finished turn's record is kept in a small
 * bounded outbox and retried until the tools socket takes it. Driven against
 * the in-process FakeToolsSocket.
 */

import { afterEach, test, mock } from "node:test";
import assert from "node:assert/strict";
import { FakeToolsSocket } from "../src/tools-socket/fake.js";
import {
  OUTBOX_CAP,
  OUTBOX_RETRY_MS,
  UsageOutbox,
  type OutboxLogLine,
} from "../src/usage/outbox.js";
import type { TurnRecord } from "../src/tools-socket/client.js";

afterEach(() => mock.timers.reset());

function rec(i: number): TurnRecord {
  return {
    turnId: `00000000-0000-4000-8000-${String(i).padStart(12, "0")}`,
    project: "greeter",
    conversationId: "6a2b1c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
    kind: "browser",
    flow: "",
    status: "completed",
    baseRef: "base1",
    skillsRef: "skills1",
    startedAt: "2026-10-03T10:00:00.000Z",
    finishedAt: "2026-10-03T10:00:05.000Z",
    model: "claude-sonnet-5",
    modelHost: "api.anthropic.com",
    inputTokens: i,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
  };
}

const ids = (rs: TurnRecord[]) => rs.map((r) => r.inputTokens);

/** Let promise chains and setImmediate callbacks run. */
async function settle(): Promise<void> {
  for (let i = 0; i < 10; i++) await new Promise((r) => setImmediate(r));
}

function quietOutbox(tools: FakeToolsSocket) {
  const lines: OutboxLogLine[] = [];
  const outbox = new UsageOutbox(tools, { log: (l) => lines.push(l) });
  return { outbox, lines };
}

test("exact values: cap 200, retry every 2 s", () => {
  assert.equal(OUTBOX_CAP, 200);
  assert.equal(OUTBOX_RETRY_MS, 2_000);
});

test("run delivers pushed records in order", async () => {
  const tools = new FakeToolsSocket();
  const { outbox } = quietOutbox(tools);
  outbox.push(rec(1));
  outbox.push(rec(2));
  assert.deepEqual(tools.usage, [], "nothing is sent before run()");
  outbox.run();
  outbox.push(rec(3));
  await settle();
  assert.deepEqual(ids(tools.usage), [1, 2, 3]);
  assert.equal(outbox.pending, 0);
});

test("a failed postUsage is retried every 2 s and order is kept", async () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  const tools = new FakeToolsSocket();
  const { outbox } = quietOutbox(tools);
  tools.failUsage(2);
  outbox.push(rec(1));
  outbox.push(rec(2));
  outbox.run();
  await settle();
  assert.equal(tools.usageAttempts, 1);
  assert.deepEqual(tools.usage, []);

  mock.timers.tick(1_999);
  await settle();
  assert.equal(tools.usageAttempts, 1, "no retry before 2 s");

  mock.timers.tick(1);
  await settle();
  assert.equal(tools.usageAttempts, 2, "second attempt at 2 s, fails again");
  assert.deepEqual(tools.usage, []);

  outbox.push(rec(3)); // a push while waiting does not jump the queue or the wait
  await settle();
  assert.equal(tools.usageAttempts, 2);

  mock.timers.tick(2_000);
  await settle();
  assert.deepEqual(ids(tools.usage), [1, 2, 3]);
});

test("beyond 200 records the oldest is dropped, logged without values", async () => {
  const tools = new FakeToolsSocket();
  const { outbox, lines } = quietOutbox(tools);
  for (let i = 0; i < OUTBOX_CAP + 5; i++) outbox.push(rec(i));
  assert.equal(outbox.pending, OUTBOX_CAP);
  assert.equal(lines.filter((l) => l.msg === "usage.dropped" && l.reason === "overflow").length, 5);
  assert.deepEqual(lines[0], { msg: "usage.dropped", source: "ae-design-agent", reason: "overflow", turnId: rec(0).turnId });
  outbox.run();
  await settle();
  const delivered = ids(tools.usage);
  assert.equal(delivered.length, OUTBOX_CAP);
  assert.deepEqual(delivered.slice(0, 2), [5, 6]);
  assert.equal(delivered.at(-1), OUTBOX_CAP + 4);
});

test("overflow never drops the record in flight: the next oldest goes", async () => {
  const tools = new FakeToolsSocket();
  const { outbox, lines } = quietOutbox(tools);
  const gate = tools.holdUsage();
  outbox.push(rec(0));
  outbox.run();
  await settle(); // rec(0) is in flight
  for (let i = 1; i <= OUTBOX_CAP; i++) outbox.push(rec(i)); // 201 queued: one must go
  assert.equal(outbox.pending, OUTBOX_CAP);
  assert.deepEqual(
    lines.map((l) => l.turnId),
    [rec(1).turnId],
  );
  gate.release();
  await settle();
  const delivered = ids(tools.usage);
  assert.equal(delivered.length, OUTBOX_CAP);
  assert.deepEqual(delivered.slice(0, 2), [0, 2]);
  assert.equal(new Set(delivered).size, delivered.length);
});

test("a permanent refusal drops the record and moves on", async () => {
  const tools = new FakeToolsSocket();
  const { outbox, lines } = quietOutbox(tools);
  tools.failUsage(1, { permanent: true });
  outbox.push(rec(1));
  outbox.push(rec(2));
  outbox.run();
  await settle();
  assert.deepEqual(ids(tools.usage), [2]);
  assert.deepEqual(lines, [
    { msg: "usage.dropped", source: "ae-design-agent", reason: "rejected", turnId: rec(1).turnId, status: 400 },
  ]);
});

test("drain resolves true once the outbox is empty, retrying at once rather than after the wait", async () => {
  const tools = new FakeToolsSocket();
  const { outbox } = quietOutbox(tools);
  tools.failUsage(1);
  outbox.push(rec(1));
  outbox.run();
  await settle();
  assert.deepEqual(tools.usage, [], "first attempt failed; the retry waits 2 s");
  const started = Date.now();
  assert.equal(await outbox.drain(1_000), true);
  assert.ok(Date.now() - started < 1_000);
  assert.deepEqual(ids(tools.usage), [1]);
});

test("drain without run still delivers", async () => {
  const tools = new FakeToolsSocket();
  const { outbox } = quietOutbox(tools);
  outbox.push(rec(1));
  assert.equal(await outbox.drain(1_000), true);
  assert.deepEqual(ids(tools.usage), [1]);
});

test("drain is bounded: a socket that keeps failing resolves false at the timeout", async () => {
  const tools = new FakeToolsSocket();
  const { outbox } = quietOutbox(tools);
  tools.failUsage(Number.POSITIVE_INFINITY);
  outbox.push(rec(1));
  const started = Date.now();
  assert.equal(await outbox.drain(150), false);
  const took = Date.now() - started;
  assert.ok(took >= 140 && took < 1_000, `took ${took} ms`);
  assert.equal(outbox.pending, 1);
});

test("drain is bounded when a send never answers", async () => {
  const tools = new FakeToolsSocket();
  const { outbox } = quietOutbox(tools);
  tools.holdUsage();
  outbox.push(rec(1));
  assert.equal(await outbox.drain(100), false);
});

test("drain on an empty outbox resolves true at once", async () => {
  const { outbox } = quietOutbox(new FakeToolsSocket());
  assert.equal(await outbox.drain(1_000), true);
});
