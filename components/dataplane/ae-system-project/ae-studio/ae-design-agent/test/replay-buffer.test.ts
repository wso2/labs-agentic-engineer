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
import {
  ReplayBuffer,
  ReplayTruncatedError,
  REPLAY_MAX_BYTES,
  REPLAY_MAX_PARTS,
  REPLAY_RETENTION_MS,
  type ReplayFrame,
} from "../src/turns/replay-buffer.js";

const text = (i: number) => ({ type: "text-delta", id: "t", delta: `d${i}` });

/** Read up to `n` frames (all when omitted) and stop the iteration. */
async function take(it: AsyncIterable<ReplayFrame>, n = Infinity): Promise<ReplayFrame[]> {
  const out: ReplayFrame[] = [];
  if (n <= 0) return out;
  for await (const f of it) {
    out.push(f);
    if (out.length >= n) break;
  }
  return out;
}

const ids = (frames: ReplayFrame[]) => frames.map((f) => f.id);

test("caps and retention are the turn_stream.go values", () => {
  assert.equal(REPLAY_RETENTION_MS, 120_000);
  assert.equal(REPLAY_MAX_PARTS, 16_384);
  assert.equal(REPLAY_MAX_BYTES, 16 * 1024 * 1024);
});

test("end appends the terminal part and [DONE] after the stored frames", async () => {
  const buf = new ReplayBuffer();
  buf.append(text(0));
  buf.append(text(1));
  buf.end({ type: "turn-failed", reason: "stream-died", message: "cap" });
  const frames = await take(buf.attach(0));
  assert.deepEqual(frames, [
    { id: 0, part: text(0) },
    { id: 1, part: text(1) },
    { id: 2, part: { type: "turn-failed", reason: "stream-died", message: "cap" } },
    { id: 3, part: "[DONE]" },
  ]);
  assert.equal(buf.ended, true);
});

test("appends after the end are dropped; a second end is a no-op", async () => {
  const buf = new ReplayBuffer();
  buf.append(text(0));
  buf.end({ type: "turn-completed" });
  buf.append(text(1));
  buf.end({ type: "turn-failed", reason: "internal" });
  assert.deepEqual(ids(await take(buf.attach(0))), [0, 1, 2]);
});

test("attach from 0, read 3, re-attach from 3: frames 3.. exactly once", async () => {
  const buf = new ReplayBuffer();
  for (let i = 0; i < 6; i++) buf.append(text(i));
  const first = await take(buf.attach(0), 3);
  assert.deepEqual(ids(first), [0, 1, 2]);
  buf.end({ type: "turn-completed" });
  const resumed = await take(buf.attach(3));
  assert.deepEqual(ids(resumed), [3, 4, 5, 6, 7]);
  const seen = [...ids(first), ...ids(resumed)];
  assert.equal(new Set(seen).size, seen.length, "no frame twice");
});

test("a live attacher tails new frames from `from` and ends after [DONE]", async () => {
  const buf = new ReplayBuffer();
  buf.append(text(0));
  buf.append(text(1));
  const reading = take(buf.attach(1));
  buf.append(text(2));
  await new Promise((r) => setImmediate(r));
  buf.append(text(3));
  buf.end({ type: "turn-completed" });
  assert.deepEqual(ids(await reading), [1, 2, 3, 4, 5]);
});

test("a live attacher from beyond the head skips frames below `from`", async () => {
  const buf = new ReplayBuffer();
  buf.append(text(0));
  const reading = take(buf.attach(3));
  buf.append(text(1));
  buf.append(text(2));
  buf.append(text(3));
  buf.end({ type: "turn-completed" });
  assert.deepEqual(ids(await reading), [3, 4, 5]);
});

test("a negative `from` is read as 0", async () => {
  const buf = new ReplayBuffer();
  buf.append(text(0));
  buf.end({ type: "turn-completed" });
  assert.deepEqual(ids(await take(buf.attach(-4))), [0, 1, 2]);
});

test("breaking out of a live attach detaches it", async () => {
  const buf = new ReplayBuffer();
  buf.append(text(0));
  const it = buf.attach(0)[Symbol.asyncIterator]();
  assert.deepEqual((await it.next()).value, { id: 0, part: text(0) });
  await it.return?.();
  buf.append(text(1));
  assert.equal((await it.next()).done, true);
});

test("overflow past the part cap: live tailers keep tailing, new attachers are refused until the end", async () => {
  const buf = new ReplayBuffer({ maxParts: 3 });
  buf.append(text(0));
  const tail = take(buf.attach(0));
  for (let i = 1; i < 5; i++) buf.append(text(i)); // frames 3 and 4 overflow
  assert.throws(() => buf.attach(0), ReplayTruncatedError);
  buf.end({ type: "turn-failed", reason: "agent-error" });
  assert.deepEqual(ids(await tail), [0, 1, 2, 3, 4, 5, 6]);
  // After the end only the terminal frames replay (the stored head has a gap).
  assert.deepEqual(ids(await take(buf.attach(0))), [5, 6]);
  assert.deepEqual(ids(await take(buf.attach(6))), [6]);
});

test("overflow past the byte cap truncates the same way", () => {
  const buf = new ReplayBuffer({ maxBytes: 60 });
  buf.append(text(0));
  buf.append({ type: "text-delta", id: "t", delta: "x".repeat(80) });
  assert.throws(() => buf.attach(0), ReplayTruncatedError);
});

test("a tailer that falls 1024 frames behind is dropped without a terminal", async () => {
  const buf = new ReplayBuffer();
  const it = buf.attach(0)[Symbol.asyncIterator]();
  for (let i = 0; i < 1025; i++) buf.append(text(i));
  const got: ReplayFrame[] = [];
  for (let r = await it.next(); !r.done; r = await it.next()) got.push(r.value);
  assert.ok(got.length <= 1024, `dropped tailer got ${got.length} frames`);
  assert.notEqual(got.at(-1)?.part, "[DONE]");
});

test("dispose releases the stored frames", async () => {
  const buf = new ReplayBuffer();
  buf.append(text(0));
  buf.end({ type: "turn-completed" });
  buf.dispose();
  assert.deepEqual(await take(buf.attach(0)), []);
});

test("dispose ends live tailers", async () => {
  const buf = new ReplayBuffer();
  buf.append(text(0));
  const reading = take(buf.attach(0));
  buf.dispose();
  assert.deepEqual(ids(await reading), [0]);
});
