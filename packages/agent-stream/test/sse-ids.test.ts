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

// The frame ids a resuming reader needs: the design agent writes each part as
// `id: <index>\ndata: <json>`, where the index is the part's place in the
// turn's replay buffer, and a reader resumes with `?from=<last id + 1>`.

import { test } from "node:test";
import assert from "node:assert/strict";
import { parseSseFrames, type SseFrame, type SseStreamEnd } from "../src/sse-client.js";

function byteStream(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  let i = 0;
  return new ReadableStream<Uint8Array>({
    pull(controller) {
      if (i < chunks.length) controller.enqueue(encoder.encode(chunks[i++]));
      else controller.close();
    },
  });
}

async function collect(body: ReadableStream<Uint8Array>): Promise<{ frames: SseFrame[]; end: SseStreamEnd }> {
  const it = parseSseFrames(body)[Symbol.asyncIterator]();
  const frames: SseFrame[] = [];
  while (true) {
    const r = await it.next();
    if (r.done) return { frames, end: r.value };
    frames.push(r.value);
  }
}

test("yields each frame's id beside its part", async () => {
  const { frames, end } = await collect(
    byteStream([
      'id: 0\ndata: {"type":"text-delta","text":"a"}\n\n',
      'id: 1\ndata: {"type":"text-delta","text":"b"}\n\n',
      'id: 2\ndata: {"type":"turn-completed"}\n\n',
      "data: [DONE]\n\n",
    ]),
  );
  assert.equal(end, "done");
  assert.deepEqual(frames, [
    { id: 0, part: { type: "text-delta", text: "a" } },
    { id: 1, part: { type: "text-delta", text: "b" } },
    { id: 2, part: { type: "turn-completed" } },
  ]);
});

test("a stream cut after ids 0-4 ends 'eof' with 4 as the last id read", async () => {
  const chunks = [0, 1, 2, 3, 4].map((i) => `id: ${i}\ndata: {"type":"text-delta","text":"${i}"}\n\n`);
  const { frames, end } = await collect(byteStream(chunks));
  assert.equal(end, "eof");
  assert.deepEqual(
    frames.map((f) => f.id),
    [0, 1, 2, 3, 4],
  );
});

test("a frame without an id line carries no id", async () => {
  const { frames } = await collect(byteStream(['data: {"type":"text-delta","text":"a"}\n\n', "data: [DONE]\n\n"]));
  assert.deepEqual(frames, [{ part: { type: "text-delta", text: "a" } }]);
});

test("an id that is not a non-negative integer is no id", async () => {
  const { frames } = await collect(
    byteStream([
      'id: abc\ndata: {"type":"text-delta","text":"a"}\n\n',
      'id: -1\ndata: {"type":"text-delta","text":"b"}\n\n',
      'id: 1.5\ndata: {"type":"text-delta","text":"c"}\n\n',
      'id:3\ndata: {"type":"text-delta","text":"d"}\n\n',
    ]),
  );
  assert.deepEqual(
    frames.map((f) => f.id),
    [undefined, undefined, undefined, 3],
  );
});

test("an id line split from its data line across chunks still pairs with it", async () => {
  const { frames } = await collect(byteStream(["id: 7\n", 'data: {"type":"text-delta","text":"a"}\n\n']));
  assert.deepEqual(frames, [{ id: 7, part: { type: "text-delta", text: "a" } }]);
});

test("keep-alive comments carry neither a part nor an id", async () => {
  const { frames, end } = await collect(
    byteStream([": keep-alive\n\n", 'id: 0\ndata: {"type":"text-delta","text":"a"}\n\n', ": keep-alive\n\n"]),
  );
  assert.equal(end, "eof");
  assert.deepEqual(frames, [{ id: 0, part: { type: "text-delta", text: "a" } }]);
});
