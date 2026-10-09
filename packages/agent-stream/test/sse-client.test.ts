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
import { createServer, type IncomingMessage } from "node:http";
import type { AddressInfo } from "node:net";
import { parseSseStream, startAndStreamTurn, TurnRefusedError, type SseStreamEnd } from "../src/sse-client.js";
import type { StreamPart } from "../src/stream-types.js";

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

async function collect(
  body: ReadableStream<Uint8Array>,
): Promise<{ parts: StreamPart[]; end: SseStreamEnd }> {
  const it = parseSseStream(body)[Symbol.asyncIterator]();
  const parts: StreamPart[] = [];
  while (true) {
    const r = await it.next();
    if (r.done) return { parts, end: r.value };
    parts.push(r.value);
  }
}

test("returns 'done' when the [DONE] sentinel arrives", async () => {
  const { parts, end } = await collect(
    byteStream(['data: {"type":"text-delta","text":"hi"}\n\n', "data: [DONE]\n\n"]),
  );
  assert.equal(end, "done");
  assert.equal(parts.length, 1);
  assert.equal(parts[0]?.type, "text-delta");
});

test("returns 'eof' when the stream ends without [DONE]", async () => {
  const { parts, end } = await collect(
    byteStream([
      'data: {"type":"text-delta","text":"hi"}\n\n',
      'data: {"type":"tool-input-delta","id":"t1","delta":"{"}\n\n',
      // …connection dies mid-turn: no error frame, no [DONE].
    ]),
  );
  assert.equal(end, "eof");
  assert.equal(parts.length, 2);
});

test("frames split across chunk boundaries still parse; keep-alives are skipped", async () => {
  const { parts, end } = await collect(
    byteStream([
      'data: {"type":"text-',
      'delta","text":"a"}\n',
      "\n: keep-alive\n\ndata: [DO",
      "NE]\n\n",
    ]),
  );
  assert.equal(end, "done");
  assert.deepEqual(parts, [{ type: "text-delta", text: "a" }]);
});

test("a truncated frame terminated by an injected blank line is skipped, not thrown", async () => {
  // The BFF's upstream-failure path: a frame cut mid-JSON, then a blank line,
  // then a synthetic error frame + [DONE]. The remnant must not crash the fold.
  const { parts, end } = await collect(
    byteStream([
      'data: {"type":"text-delta","text":"a"}\n\n',
      'data: {"type":"tool-inp', // upstream died mid-frame…
      '\n\ndata: {"type":"error","error":"upstream stream failed"}\n\ndata: [DONE]\n\n',
    ]),
  );
  assert.equal(end, "done");
  assert.deepEqual(
    parts.map((p) => p.type),
    ["text-delta", "error"],
  );
});

test("the BFF's upstream-failure marker (comment, no [DONE]) reads as a truncated stream", async () => {
  // genai_huma.go passThrough: partial frame → blank line + `: upstream-error`
  // comment, then the response ends. The remnant is skipped, the comment is
  // invisible, and the missing [DONE] surfaces as "eof" — the truncation signal.
  const { parts, end } = await collect(
    byteStream([
      'data: {"type":"text-delta","text":"a"}\n\n',
      'data: {"type":"tool-inp', // upstream died mid-frame…
      "\n\n: upstream-error\n\n",
    ]),
  );
  assert.equal(end, "eof");
  assert.deepEqual(
    parts.map((p) => p.type),
    ["text-delta"],
  );
});

test("BFF `id:`-prefixed frames parse — the data line is not hidden by the id line", async () => {
  // The committed-truth turn stream (genai_huma.go streamSubscription) writes
  // every part as `id: <index>\ndata: <json>\n\n` for Last-Event-ID resume. The
  // parser must read the `data:` line within the multi-line frame, not require
  // the frame to START with `data:` (that dropped every part → dead live-preview).
  const { parts, end } = await collect(
    byteStream([
      'id: 0\ndata: {"type":"tool-input-start","id":"t1","toolName":"addFile"}\n\n',
      'id: 1\ndata: {"type":"tool-call","toolCallId":"t1","toolName":"addFile","input":{"path":"specs/requirements/prd.md","content":"# Reqs\\n"}}\n\n',
      'id: 2\ndata: {"type":"turn-committed","commitSha":"abc123"}\n\n',
      "data: [DONE]\n\n", // the [DONE] sentinel is written WITHOUT an id
    ]),
  );
  assert.equal(end, "done");
  assert.deepEqual(
    parts.map((p) => p.type),
    ["tool-input-start", "tool-call", "turn-committed"],
  );
});

test("an `id:`-prefixed frame split across chunk boundaries (id line, then data line) parses", async () => {
  const { parts, end } = await collect(
    byteStream([
      "id: 7\n", // the id line arrives before the data line's chunk
      'data: {"type":"text-delta","text":"a"}\n\n',
      "data: [DONE]\n\n",
    ]),
  );
  assert.equal(end, "done");
  assert.deepEqual(parts, [{ type: "text-delta", text: "a" }]);
});

test("for-await consumers are unaffected by the return value", async () => {
  const parts: StreamPart[] = [];
  for await (const part of parseSseStream(
    byteStream(['data: {"type":"finish","finishReason":"stop"}\n\n', "data: [DONE]\n\n"]),
  )) {
    parts.push(part);
  }
  assert.equal(parts.length, 1);
});

interface Seen {
  method: string;
  url: string;
  auth: string | undefined;
  body: string;
}

/** A /v1 edge stand-in: answers the start with `start`, then streams `frames`. */
async function fakeEdge(
  start: { status: number; body: unknown },
  frames: string[],
): Promise<{ base: string; seen: Seen[]; close: () => Promise<void> }> {
  const seen: Seen[] = [];
  const read = async (req: IncomingMessage): Promise<string> => {
    let body = "";
    for await (const chunk of req) body += String(chunk);
    return body;
  };
  const server = createServer((req, res) => {
    void read(req).then((body) => {
      seen.push({ method: req.method ?? "", url: req.url ?? "", auth: req.headers.authorization, body });
      if (req.method === "POST") {
        res.writeHead(start.status, { "content-type": "application/json" }).end(JSON.stringify(start.body));
        return;
      }
      res.writeHead(200, { "content-type": "text/event-stream" });
      res.end(frames.join(""));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  return {
    base: `http://127.0.0.1:${port}`,
    seen,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}

test("startAndStreamTurn posts the turn, then streams it from frame 0", async () => {
  const edge = await fakeEdge({ status: 202, body: { turnId: "t-1" } }, [
    'id: 0\ndata: {"type":"text-delta","text":"hi"}\n\n',
    'id: 1\ndata: {"type":"turn-completed"}\n\n',
    "id: 2\ndata: [DONE]\n\n",
  ]);
  try {
    const parts: StreamPart[] = [];
    for await (const part of startAndStreamTurn(edge.base, "greeter", "c-1", { instruction: "/design" }, { Authorization: "Bearer x" })) {
      parts.push(part);
    }
    assert.deepEqual(
      parts.map((p) => p.type),
      ["text-delta", "turn-completed"],
    );
    assert.deepEqual(
      edge.seen.map((s) => `${s.method} ${s.url}`),
      ["POST /v1/projects/greeter/conversations/c-1/turns", "GET /v1/projects/greeter/turns/t-1/stream?from=0"],
    );
    assert.deepEqual(JSON.parse(edge.seen[0]!.body), { instruction: "/design" });
    assert.ok(edge.seen.every((s) => s.auth === "Bearer x"), "both requests carry the caller's headers");
  } finally {
    await edge.close();
  }
});

test("startAndStreamTurn throws TurnRefusedError with the refusal's status and code", async () => {
  const edge = await fakeEdge({ status: 409, body: { code: "conversation_rotated", message: "rotated" } }, []);
  try {
    await assert.rejects(
      () => startAndStreamTurn(edge.base, "greeter", "c-1", { instruction: "hi" }).next(),
      (err: unknown) => err instanceof TurnRefusedError && err.status === 409 && err.code === "conversation_rotated",
    );
    assert.equal(edge.seen.length, 1, "no stream is opened for a refused turn");
  } finally {
    await edge.close();
  }
});
