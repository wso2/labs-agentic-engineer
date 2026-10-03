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

import { afterEach, beforeEach, mock, test } from "node:test";
import assert from "node:assert/strict";
import type { ReplayFrame } from "../src/turns/replay-buffer.js";
import {
  TURN_CAP_MS,
  TurnDesk,
  TurnInProgressError,
  type Scope,
  type TurnMeta,
  type TurnOutcome,
  type TurnRecord,
  type TurnRun,
} from "../src/turns/turn-desk.js";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const KICKOFF_ID = "3f0e8a4c-1d2b-5c6d-8e9f-0a1b2c3d4e5f";
const proj = { kind: "project", project: "greeter" } as const;

// Node's mock timers drive both setTimeout (cap, retention) and Date (now).
beforeEach(() => mock.timers.enable({ apis: ["setTimeout", "Date"], now: Date.parse("2026-10-03T10:00:00Z") }));
afterEach(() => mock.timers.reset());

function fakeClock() {
  return { now: () => Date.now(), advance: (ms: number) => mock.timers.tick(ms) };
}

/** Let promise chains and setImmediate callbacks run. */
async function settle(): Promise<void> {
  for (let i = 0; i < 5; i++) await new Promise((r) => setImmediate(r));
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

function meta(over: Partial<TurnMeta> = {}): TurnMeta {
  return {
    conversationId: "6a2b1c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
    kind: "browser",
    flow: "",
    instruction: "add a health endpoint",
    author: { id: "ann@x", name: "Ann" },
    model: "claude-sonnet-5",
    modelHost: "api.anthropic.com",
    ...over,
  };
}

function done(over: Partial<TurnOutcome> = {}): TurnOutcome {
  return { status: "completed", baseRef: "base1", skillsRef: "skills1", ...over };
}

const text = (i: number) => ({ type: "text-delta", id: "t", delta: `d${i}` });
const never: TurnRun = () => new Promise(() => {});
const untilAborted: TurnRun = (_emit, signal) =>
  new Promise((_, rej) => signal.addEventListener("abort", () => rej(new Error("aborted"))));

async function frames(it: AsyncIterable<ReplayFrame>): Promise<ReplayFrame[]> {
  const out: ReplayFrame[] = [];
  for await (const f of it) out.push(f);
  return out;
}
const ids = async (it: AsyncIterable<ReplayFrame>) => (await frames(it)).map((f) => f.id);

test("the cap is 30 minutes", () => {
  assert.equal(TURN_CAP_MS, 30 * 60_000);
});

test("lock is per project; a throwing run releases it; same turnId reattaches", async () => {
  const desk = new TurnDesk({ onFinished: () => {} });
  const p = { kind: "project", project: "greeter" } as const;
  const hold = deferred<TurnOutcome>();
  const a = desk.start(p, meta(), async () => hold.promise, KICKOFF_ID);
  assert.equal(a.turnId, KICKOFF_ID, "a given turn id is used as is");
  assert.equal(a.reattached, false);
  assert.throws(
    () => desk.start(p, meta(), async () => done()),
    (e) => e instanceof TurnInProgressError && e.activeTurnId === a.turnId,
  );
  assert.equal(desk.start(p, meta(), async () => done(), KICKOFF_ID).reattached, true);
  assert.doesNotThrow(() => desk.start({ kind: "project", project: "other" }, meta(), async () => done()));
  hold.resolve(done());
  await settle();
  const b = desk.start(p, meta(), async () => {
    throw new Error("model init failed");
  });
  assert.match(b.turnId, UUID, "a desk-made turn id is a uuid (the ledger id column)");
  await settle();
  assert.equal(desk.status(b.turnId)?.status, "failed");
  assert.equal(desk.status(b.turnId)?.reason, "internal");
  assert.equal(desk.active(p), null);
});

test("the marketplace lock is per conversation", () => {
  const desk = new TurnDesk({ onFinished: () => {} });
  const c1: Scope = { kind: "marketplace", conversationId: "c1" };
  desk.start(c1, meta({ conversationId: "c1" }), never);
  assert.throws(() => desk.start(c1, meta({ conversationId: "c1" }), never), TurnInProgressError);
  assert.doesNotThrow(() =>
    desk.start({ kind: "marketplace", conversationId: "c2" }, meta({ conversationId: "c2" }), never),
  );
  // A project named like a conversation id is a different scope.
  assert.doesNotThrow(() => desk.start({ kind: "project", project: "c1" }, meta(), never));
});

test("lock release: a runner that throws synchronously", async () => {
  const desk = new TurnDesk({ onFinished: () => {} });
  const t = desk.start(proj, meta(), (() => {
    throw new Error("snapshot read failed");
  }) as TurnRun);
  await settle();
  assert.equal(desk.active(proj), null);
  assert.equal(desk.status(t.turnId)?.status, "failed");
  assert.doesNotThrow(() => desk.start(proj, meta(), async () => done()));
});

test("lock release: a runner that rejects before the first frame", async () => {
  const desk = new TurnDesk({ onFinished: () => {} });
  const t = desk.start(proj, meta(), () => Promise.reject(new Error("room join failed")));
  await settle();
  assert.equal(desk.active(proj), null);
  assert.equal(desk.status(t.turnId)?.reason, "internal");
  assert.deepEqual((await frames(desk.attach(t.turnId, 0)!)).map((f) => f.part), [
    { type: "turn-failed", reason: "internal", message: "room join failed" },
    "[DONE]",
  ]);
  assert.doesNotThrow(() => desk.start(proj, meta(), async () => done()));
});

test("30-minute cap fails the turn with stream-died and frees the lock", async () => {
  const clock = fakeClock();
  const desk = new TurnDesk({ now: clock.now, capMs: 30 * 60_000, onFinished: () => {} });
  const t = desk.start(proj, meta(), untilAborted);
  clock.advance(30 * 60_000 - 1);
  await settle();
  assert.equal(desk.active(proj)?.turnId, t.turnId, "still running just under the cap");
  clock.advance(2);
  await settle();
  assert.equal(desk.status(t.turnId)?.reason, "stream-died");
  assert.equal(desk.active(proj), null);
  assert.doesNotThrow(() => desk.start(proj, meta(), async () => done()));
});

test("the cap frees the lock even when the runner ignores the abort", async () => {
  const clock = fakeClock();
  let aborted = false;
  const desk = new TurnDesk({ now: clock.now, onFinished: () => {} });
  const t = desk.start(proj, meta(), (_e, signal) => {
    signal.addEventListener("abort", () => (aborted = true));
    return new Promise(() => {});
  });
  clock.advance(TURN_CAP_MS + 1);
  await settle();
  assert.equal(aborted, true, "the run's signal is aborted");
  assert.equal(desk.status(t.turnId)?.status, "failed");
  assert.equal(desk.status(t.turnId)?.reason, "stream-died");
  assert.equal(desk.active(proj), null);
});

test("attach from N yields frames N.. exactly once; after retention it is gone", async () => {
  const clock = fakeClock();
  const desk = new TurnDesk({ now: clock.now, onFinished: () => {} });
  const t = desk.start(proj, meta(), async (emit) => {
    for (let i = 0; i < 5; i++) emit(text(i));
    return done();
  });
  await settle();
  assert.deepEqual(await ids(desk.attach(t.turnId, 0)!), [0, 1, 2, 3, 4, 5, 6]); // 5 frames + turn-completed + [DONE]
  assert.deepEqual(await ids(desk.attach(t.turnId, 3)!), [3, 4, 5, 6]);
  const parts = (await frames(desk.attach(t.turnId, 5)!)).map((f) => f.part);
  assert.deepEqual(parts, [{ type: "turn-completed" }, "[DONE]"]);
  clock.advance(120_000 - 1);
  assert.notEqual(desk.attach(t.turnId, 0), null, "still attachable inside the 120 s");
  clock.advance(2);
  assert.equal(desk.attach(t.turnId, 0), null);
  assert.equal(desk.attach("4b4e1f1a-0000-4000-8000-000000000000", 0), null, "unknown turn");
});

test("a watcher attached mid-turn tails live frames and ends with the terminal", async () => {
  const desk = new TurnDesk({ onFinished: () => {} });
  const gate = deferred<void>();
  const t = desk.start(proj, meta(), async (emit) => {
    emit(text(0));
    await gate.promise;
    emit(text(1));
    return done({ status: "failed", reason: "agent-error", code: "provider_limit", message: "limit", host: "api.anthropic.com", resetAt: "2026-10-03T11:00:00.000Z" });
  });
  await settle();
  const reading = frames(desk.attach(t.turnId, 0)!);
  gate.resolve();
  const got = await reading;
  assert.deepEqual(got.map((f) => f.id), [0, 1, 2, 3]);
  assert.deepEqual(got[2]!.part, {
    type: "turn-failed",
    reason: "agent-error",
    code: "provider_limit",
    message: "limit",
    host: "api.anthropic.com",
    resetAt: "2026-10-03T11:00:00.000Z",
  });
});

test("frames a capped run emits after the terminal are dropped", async () => {
  const clock = fakeClock();
  let emitLate!: () => void;
  const desk = new TurnDesk({ now: clock.now, onFinished: () => {} });
  const t = desk.start(proj, meta(), (emit) => {
    emitLate = () => emit(text(9));
    return new Promise(() => {});
  });
  await settle();
  clock.advance(TURN_CAP_MS + 1);
  emitLate();
  const parts = (await frames(desk.attach(t.turnId, 0)!)).map((f) => f.part);
  assert.deepEqual(parts, [
    { type: "turn-failed", reason: "stream-died", message: "the turn ran past the 30-minute cap" },
    "[DONE]",
  ]);
});

test("status and active project the TurnStatus shape", async () => {
  const clock = fakeClock();
  const desk = new TurnDesk({ now: clock.now, onFinished: () => {} });
  const hold = deferred<TurnOutcome>();
  const t = desk.start(proj, meta({ kind: "kickoff", flow: "start" }), () => hold.promise);
  const running = {
    turnId: t.turnId,
    project: "greeter",
    conversationId: "6a2b1c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
    kind: "kickoff",
    flow: "start",
    status: "running",
    instruction: "add a health endpoint",
    authorId: "ann@x",
    authorDisplayName: "Ann",
    createdAt: "2026-10-03T10:00:00.000Z",
  };
  assert.deepEqual(desk.active(proj), running);
  assert.deepEqual(desk.status(t.turnId), running);
  clock.advance(5_000);
  hold.resolve(done());
  await settle();
  assert.deepEqual(desk.status(t.turnId), { ...running, status: "completed", finishedAt: "2026-10-03T10:00:05.000Z" });
  assert.equal(desk.active(proj), null);
});

test("a turn with no author reports empty author fields; marketplace has no project", async () => {
  const desk = new TurnDesk({ onFinished: () => {} });
  const scope: Scope = { kind: "marketplace", conversationId: "c1" };
  const t = desk.start(scope, meta({ author: undefined, conversationId: "c1" }), never);
  const s = desk.status(t.turnId)!;
  assert.equal(s.authorId, "");
  assert.equal(s.authorDisplayName, "");
  assert.equal("project" in s, false);
  assert.equal(desk.active(scope)?.turnId, t.turnId);
});

test("TurnStatus retention: the last 20 per scope", async () => {
  const desk = new TurnDesk({ onFinished: () => {} });
  const made: string[] = [];
  for (let i = 0; i < 21; i++) {
    made.push(desk.start(proj, meta(), async () => done()).turnId);
    await settle();
  }
  const other = desk.start({ kind: "project", project: "other" }, meta(), async () => done()).turnId;
  await settle();
  assert.equal(desk.status(made[0]!), null, "the 21st turn pushes out the oldest");
  for (const id of made.slice(1)) assert.equal(desk.status(id)?.status, "completed");
  assert.equal(desk.status(other)?.status, "completed", "another scope keeps its own 20");
});

test("TurnStatus retention: 1 h after the terminal; a running turn is never dropped", async () => {
  const clock = fakeClock();
  const desk = new TurnDesk({ now: clock.now, capMs: 2 * 60 * 60_000, onFinished: () => {} });
  const finished = desk.start(proj, meta(), async () => done()).turnId;
  await settle();
  const running = desk.start({ kind: "project", project: "other" }, meta(), never).turnId;
  clock.advance(60 * 60_000 - 1);
  assert.equal(desk.status(finished)?.status, "completed");
  clock.advance(2);
  assert.equal(desk.status(finished), null);
  assert.equal(desk.status(running)?.status, "running");
});

test("a retained turn id reattaches after the turn finished; it does not start again", async () => {
  const desk = new TurnDesk({ onFinished: () => {} });
  let runs = 0;
  const run: TurnRun = async () => {
    runs++;
    return done();
  };
  desk.start(proj, meta({ kind: "kickoff" }), run, KICKOFF_ID);
  await settle();
  assert.deepEqual(desk.start(proj, meta({ kind: "kickoff" }), run, KICKOFF_ID), { turnId: KICKOFF_ID, reattached: true });
  await settle();
  assert.equal(runs, 1);
});

test("onFinished gets the whole TurnRecord, usage included on failure", async () => {
  const clock = fakeClock();
  const records: TurnRecord[] = [];
  const desk = new TurnDesk({ now: clock.now, onFinished: (r) => records.push(r) });
  const hold = deferred<TurnOutcome>();
  const t = desk.start(proj, meta({ kind: "plan", flow: "plan" }), () => hold.promise);
  clock.advance(3_000);
  hold.resolve(
    done({
      status: "failed",
      reason: "agent-error",
      code: "output_truncated",
      message: "cut off",
      usage: { inputTokens: 10, outputTokens: 20, cacheReadTokens: 30, cacheCreationTokens: 40, model: "claude-sonnet-5" },
      contextTokens: 1234,
    }),
  );
  await settle();
  assert.deepEqual(records, [
    {
      turnId: t.turnId,
      project: "greeter",
      conversationId: "6a2b1c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
      kind: "plan",
      flow: "plan",
      status: "failed",
      reason: "agent-error",
      code: "output_truncated",
      baseRef: "base1",
      skillsRef: "skills1",
      startedAt: "2026-10-03T10:00:00.000Z",
      finishedAt: "2026-10-03T10:00:03.000Z",
      author: { id: "ann@x", name: "Ann" },
      model: "claude-sonnet-5",
      modelHost: "api.anthropic.com",
      inputTokens: 10,
      outputTokens: 20,
      cacheReadTokens: 30,
      cacheCreationTokens: 40,
      contextTokens: 1234,
    },
  ]);
});

test("a thrown run still hands over a record with zero usage and no refs", async () => {
  const records: TurnRecord[] = [];
  const desk = new TurnDesk({ onFinished: (r) => records.push(r) });
  desk.start({ kind: "marketplace", conversationId: "c1" }, meta({ conversationId: "c1", author: undefined }), async () => {
    throw new Error("boom");
  });
  await settle();
  assert.equal(records.length, 1);
  const r = records[0]!;
  assert.equal(r.status, "failed");
  assert.equal(r.reason, "internal");
  assert.equal("project" in r, false);
  assert.equal("author" in r, false);
  assert.deepEqual([r.baseRef, r.skillsRef, r.inputTokens, r.outputTokens], ["", "", 0, 0]);
});

test("onFinished runs once per turn even when the run settles after the cap", async () => {
  const clock = fakeClock();
  const records: TurnRecord[] = [];
  const desk = new TurnDesk({ now: clock.now, onFinished: (r) => records.push(r) });
  const hold = deferred<TurnOutcome>();
  desk.start(proj, meta(), () => hold.promise);
  clock.advance(TURN_CAP_MS + 1);
  hold.resolve(done());
  await settle();
  assert.equal(records.length, 1);
  assert.equal(records[0]!.reason, "stream-died");
});

test("a throwing onFinished does not keep the lock", async () => {
  const desk = new TurnDesk({
    onFinished: () => {
      throw new Error("outbox full");
    },
  });
  desk.start(proj, meta(), async () => done());
  await settle();
  assert.equal(desk.active(proj), null);
});

test("lastTerminal holds the last finished turn's status and baseRef per scope", async () => {
  const desk = new TurnDesk({ onFinished: () => {} });
  assert.equal(desk.lastTerminal(proj), null);
  desk.start(proj, meta(), async () => done({ baseRef: "sha-a" }));
  await settle();
  assert.deepEqual(desk.lastTerminal(proj), { status: "completed", baseRef: "sha-a" });
  desk.start(proj, meta(), async () => done({ status: "failed", reason: "stream-died", baseRef: "sha-b" }));
  await settle();
  assert.deepEqual(desk.lastTerminal(proj), { status: "failed", baseRef: "sha-b" });
  assert.equal(desk.lastTerminal({ kind: "project", project: "other" }), null);
});

test("lastTerminal outlives the TurnStatus retention", async () => {
  const clock = fakeClock();
  const desk = new TurnDesk({ now: clock.now, onFinished: () => {} });
  desk.start(proj, meta(), async () => done({ baseRef: "sha-a" }));
  await settle();
  clock.advance(2 * 60 * 60_000);
  assert.deepEqual(desk.lastTerminal(proj), { status: "completed", baseRef: "sha-a" });
});

test("abortAll ends every running turn with shutdown, hands over records, frees the locks", async () => {
  const records: TurnRecord[] = [];
  const desk = new TurnDesk({ onFinished: (r) => records.push(r) });
  const a = desk.start(proj, meta(), untilAborted);
  const b = desk.start({ kind: "marketplace", conversationId: "c1" }, meta({ conversationId: "c1" }), untilAborted);
  await desk.abortAll("shutdown");
  for (const id of [a.turnId, b.turnId]) {
    assert.equal(desk.status(id)?.reason, "shutdown");
    const parts = (await frames(desk.attach(id, 0)!)).map((f) => f.part);
    assert.deepEqual(parts, [{ type: "turn-failed", reason: "shutdown" }, "[DONE]"]);
  }
  assert.deepEqual(records.map((r) => r.reason), ["shutdown", "shutdown"]);
  assert.equal(desk.active(proj), null);
});

test("abortAll does not wait for a runner that ignores the abort", async () => {
  const desk = new TurnDesk({ onFinished: () => {} });
  desk.start(proj, meta(), never);
  const finished = desk.abortAll("shutdown");
  mock.timers.tick(2_000);
  await finished;
  assert.equal(desk.active(proj), null);
});
