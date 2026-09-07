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
 * Frame ORDER within one step that issues several tool calls — a contract the
 * console depends on, not an SDK detail.
 *
 * THE SDK'S BEHAVIOUR (first suite). Tools are not executed when their call
 * arrives: every call of a step is queued and run at `model-call-end`, i.e.
 * after the whole assistant message has streamed. So a raw `tool-result` says
 * "the step's work is done", never "this call's work is done" — and for a design
 * turn batching five `addFile`s, file 1's verdict waits on file 5's body.
 *
 * WHAT THIS SERVICE EMITS (second suite). `tapWrites` + the `WriteLedger` close
 * that gap: for a file tool the arguments ARE the body, so the op runs at that
 * call's `tool-input-end` and its `tool-result` rides that call's own
 * `tool-call`. Consumers that refuse to claim a write before the bundle has
 * ruled on it (the console's tick, the spec rail's per-file status) therefore
 * settle each file as it lands, and a rejected write reports mid-batch.
 *
 * Both suites are pinned because the second only makes sense while the first is
 * still true: if an SDK upgrade starts flushing results per call, the ledger's
 * suppression is what keeps exactly one result per call on the wire.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { MockLanguageModelV4, simulateReadableStream } from "ai/test";
import type { ModelMessage } from "ai";
import { FileBundle, type OpResult, type StreamPart } from "@aep/agent-stream";
import { runTurn } from "../src/agents/main/run-turn.js";
import { buildFileToolSet } from "../src/agents/main/tools/files.js";
import { runConversationTurn, TurnGuard } from "../src/conversation/run-conversation-turn.js";
import { InMemoryConversationStore } from "../src/store/memory-store.js";

const FILES: ReadonlyArray<readonly [string, string]> = [
  ["specs/design/glossary.md", "# Glossary\n"],
  ["specs/design/notes.md", "# Notes\n"],
  ["specs/design/components/web/wireframes.dsl", "screen Home\n"],
];

const USAGE = {
  inputTokens: { total: 10, noCache: 10, cacheRead: 0, cacheWrite: 0 },
  outputTokens: { total: 5, text: 5, reasoning: 0 },
};

/**
 * One step issuing N `addFile` calls, each streaming its arguments to completion
 * before the next begins — how a provider serialises parallel tool calls.
 *
 * The chunks are spread over wall-clock time on purpose: delivered in one
 * synchronous gulp, every `execute()` would resolve after the stream drained and
 * the result ordering below would be an artifact of the fixture rather than of
 * the SDK.
 */
function batchedStep(): { stream: ReadableStream<unknown> } {
  const parts: unknown[] = [{ type: "stream-start", warnings: [] }];
  FILES.forEach(([path, content], i) => {
    const id = `call-${i}`;
    const input = JSON.stringify({ path, content });
    parts.push({ type: "tool-input-start", id, toolName: "addFile" });
    for (let c = 0; c < input.length; c += 24) {
      parts.push({ type: "tool-input-delta", id, delta: input.slice(c, c + 24) });
    }
    parts.push({ type: "tool-input-end", id });
    parts.push({ type: "tool-call", toolCallId: id, toolName: "addFile", input });
  });
  parts.push({ type: "finish", finishReason: { unified: "tool-calls", raw: "tool-calls" }, usage: USAGE });
  return { stream: simulateReadableStream({ chunks: parts as never[], initialDelayInMs: 0, chunkDelayInMs: 1 }) };
}

function closingStep(): { stream: ReadableStream<unknown> } {
  return {
    stream: simulateReadableStream({
      chunks: [
        { type: "stream-start", warnings: [] },
        { type: "text-start", id: "t" },
        { type: "text-delta", id: "t", delta: "done" },
        { type: "text-end", id: "t" },
        { type: "finish", finishReason: { unified: "stop", raw: "stop" }, usage: USAGE },
      ] as never[],
      initialDelayInMs: 0,
      chunkDelayInMs: 0,
    }),
  };
}

/** The RAW SDK frames: tools only, nothing tapping the stream. */
async function collectFrames(): Promise<StreamPart[]> {
  const model = new MockLanguageModelV4({ doStream: [batchedStep(), closingStep()] as never });
  const frames: StreamPart[] = [];
  await runTurn({
    model: model as never,
    instructions: "batch",
    prompt: "write the files",
    messages: [],
    tools: buildFileToolSet(new FileBundle({})).tools,
    onEvent: (p) => frames.push(p),
  });
  return frames;
}

/**
 * What the SSE route actually forwards — driven through `runConversationTurn`,
 * not a hand-rolled tap, so this also pins that the ledger IS wired into the
 * turn orchestration (a tap wired only in the test would prove nothing).
 * `messages` comes back holding the turn's transcript, so a caller can check
 * what the MODEL was told each write did; `manifest` reports what actually
 * landed in the bundle.
 */
async function collectWire(files: Record<string, string> = {}): Promise<{
  frames: StreamPart[];
  messages: ModelMessage[];
  manifest: StreamPart | undefined;
}> {
  const model = new MockLanguageModelV4({ doStream: [batchedStep(), closingStep()] as never });
  const frames: StreamPart[] = [];
  const conv = await runConversationTurn({
    id: "batched-turn",
    instruction: "write the files",
    files,
    model: model as never,
    store: new InMemoryConversationStore(),
    guard: new TurnGuard(),
    onEvent: (p) => frames.push(p),
  });
  return {
    frames,
    messages: conv.messages,
    manifest: frames.find((f) => f.type === "manifest"),
  };
}

/** The verdicts the MODEL read, in transcript order (`{ type: "json", value }`). */
function verdictsSeenByModel(messages: ModelMessage[]): OpResult[] {
  const out: OpResult[] = [];
  for (const m of messages) {
    if (m.role !== "tool" || !Array.isArray(m.content)) continue;
    for (const part of m.content) {
      if (part.type !== "tool-result") continue;
      const output = part.output as { type?: string; value?: unknown };
      out.push(output.value as OpResult);
    }
  }
  return out;
}

const idOf = (p: StreamPart): string =>
  (p as { id?: string; toolCallId?: string }).id ?? (p as { toolCallId?: string }).toolCallId ?? "";

test("raw SDK: a batched step emits every tool-input-end before any tool-result", async () => {
  const frames = await collectFrames();
  const order = frames.map((f) => f.type);

  const lastInputEnd = order.lastIndexOf("tool-input-end");
  const firstResult = order.indexOf("tool-result");
  assert.ok(lastInputEnd >= 0, "the SDK must still emit tool-input-end");
  assert.ok(firstResult >= 0, "the step must produce results");
  assert.ok(
    lastInputEnd < firstResult,
    `every input-end must precede the first result (input-end@${lastInputEnd}, result@${firstResult})`,
  );
});

test("raw SDK: results flush as a GROUP after the last call — so they cannot mark one call done", async () => {
  const frames = await collectFrames();
  const order = frames.map((f) => f.type);
  // This is the defect the ledger exists to close: the FIRST result lands only
  // after the LAST call, so file 1's verdict waits on file N's body.
  assert.ok(
    order.indexOf("tool-result") > order.lastIndexOf("tool-call"),
    "if results ever interleave with calls, the console could key off tool-result again",
  );
  assert.equal(order.filter((t) => t === "tool-result").length, FILES.length);
});

test("raw SDK: each tool-input-end carries the id its own tool-call uses, so a card updates in place", async () => {
  const frames = await collectFrames();
  const endIds = frames.filter((f) => f.type === "tool-input-end").map(idOf);
  const callIds = frames.filter((f) => f.type === "tool-call").map(idOf);
  const resultIds = frames.filter((f) => f.type === "tool-result").map(idOf);
  assert.deepEqual(endIds, callIds, "input-end and tool-call must share the id (and its order)");
  assert.deepEqual([...resultIds].sort(), [...callIds].sort(), "every call settles exactly once");
  assert.equal(new Set(endIds).size, FILES.length, "ids must be distinct per call");
});

test("raw SDK: every file lands in the bundle — batching changes ordering, not outcomes", async () => {
  const bundle = new FileBundle({});
  const model = new MockLanguageModelV4({ doStream: [batchedStep(), closingStep()] as never });
  await runTurn({
    model: model as never,
    instructions: "batch",
    prompt: "write the files",
    messages: [],
    tools: buildFileToolSet(bundle).tools,
    onEvent: () => {},
  });
  for (const [path, content] of FILES) {
    assert.equal(bundle.read(path), content, `${path} must be present with its streamed body`);
  }
});

// --- What this service forwards (tapWrites + WriteLedger) --------------------

test("a write's verdict rides its OWN call — the next file has not started streaming", async () => {
  const { frames } = await collectWire();

  FILES.forEach((_file, i) => {
    const id = `call-${i}`;
    const call = frames.findIndex((f) => f.type === "tool-call" && idOf(f) === id);
    const result = frames.findIndex((f) => f.type === "tool-result" && idOf(f) === id);
    assert.ok(call >= 0, `${id}: its tool-call must be forwarded`);
    assert.equal(result, call + 1, `${id}: its verdict must immediately follow its own call`);

    const nextStart = frames.findIndex(
      (f) => f.type === "tool-input-start" && idOf(f) === `call-${i + 1}`,
    );
    if (nextStart >= 0) {
      assert.ok(
        result < nextStart,
        `${id} must be settled before file ${i + 2} starts streaming — that gap is the whole point`,
      );
    }
  });
});

test("exactly one tool-result per call reaches the wire (the SDK's copy is suppressed)", async () => {
  const { frames } = await collectWire();
  const resultIds = frames.filter((f) => f.type === "tool-result").map(idOf);
  const callIds = frames.filter((f) => f.type === "tool-call").map(idOf);
  assert.deepEqual(resultIds, callIds, "one result per call, in call order, no duplicates");
});

test("the op runs ONCE: the model reads the applied verdict, not ALREADY_EXISTS", async () => {
  const { frames, manifest, messages } = await collectWire();

  for (const [path] of FILES) {
    assert.ok(manifest?.files?.[path], `${path} must be in the turn's manifest`);
  }
  // The wire and the transcript must agree — a second apply would answer the
  // model ALREADY_EXISTS for a write that succeeded.
  const onWire = frames.filter((f) => f.type === "tool-result").map((f) => f.output as OpResult);
  const seenByModel = verdictsSeenByModel(messages);
  assert.deepEqual(seenByModel, onWire, "the model reads exactly the verdict the wire carried");
  assert.deepEqual(
    onWire.map((v) => v.ok && v.status),
    FILES.map(() => "applied"),
    "every write applied exactly once",
  );
});

test("a REJECTED write reports mid-batch, at its own call", async () => {
  // FILES[0] already exists with different content → addFile answers ALREADY_EXISTS.
  const [[taken]] = [FILES];
  const { frames, messages } = await collectWire({ [taken![0]]: "# something else\n" });

  const rejected = frames.find((f) => f.type === "tool-result" && idOf(f) === "call-0");
  const verdict = rejected?.output as OpResult;
  assert.equal(verdict.ok, false);
  assert.equal(verdict.ok === false && verdict.code, "ALREADY_EXISTS");

  const nextStart = frames.findIndex((f) => f.type === "tool-input-start" && idOf(f) === "call-1");
  const at = frames.findIndex((f) => f.type === "tool-result" && idOf(f) === "call-0");
  assert.ok(at < nextStart, "the failure is reported while the batch is still streaming");
  assert.equal(verdictsSeenByModel(messages)[0]?.ok, false, "and the model is told the same");
});
