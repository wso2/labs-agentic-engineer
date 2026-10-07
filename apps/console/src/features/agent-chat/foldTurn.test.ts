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

import { describe, expect, it, vi } from "vitest";
import type { StreamPart } from "@aep/agent-stream";
import { TurnStreamAttachError } from "./api/turns";
import { foldTurn, type TurnSink, type TurnStreamApi } from "./foldTurn";

// After the old console's runTurn.test.ts: the frames the fold turns into chat
// rows, and how a turn's end is observed (its terminal frame, or the status
// read that settles a stream cut short). Streams are real SSE bytes.

function sse(parts: StreamPart[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(c) {
      for (const p of parts) c.enqueue(encoder.encode(`id: 0\ndata: ${JSON.stringify(p)}\n\n`));
      c.enqueue(encoder.encode("data: [DONE]\n\n"));
      c.close();
    },
  });
}

function recordingSink() {
  const calls: [string, unknown][] = [];
  const sink: TurnSink = {
    text: (d) => calls.push(["text", d]),
    activity: (a) => calls.push(["activity", a]),
    question: (q) => calls.push(["question", q]),
    withdrawQuestion: (id) => calls.push(["withdraw", id]),
    wrote: (p) => calls.push(["wrote", p.toolCallId]),
    error: (t) => calls.push(["error", t]),
    ended: (o) => calls.push(["ended", o]),
  };
  return { sink, calls, of: (kind: string) => calls.filter(([k]) => k === kind).map(([, v]) => v) };
}

async function fold(parts: StreamPart[], api: Partial<TurnStreamApi> = {}) {
  const rec = recordingSink();
  await foldTurn({
    api: { openStream: async () => sse(parts), turn: async () => null, ...api },
    projectName: "acme",
    turnId: "t1",
    signal: new AbortController().signal,
    sink: rec.sink,
  });
  return rec;
}

const PATH = "specs/requirements/features/F4-spending-reports.md";

describe("foldTurn", () => {
  it("shows a file as it is written, then its verdict, and hands the accepted write on", async () => {
    const input = JSON.stringify({ path: PATH, oldString: "a", newString: "b" });
    const { of } = await fold([
      { type: "tool-input-start", id: "w1", toolName: "editFile" },
      { type: "tool-input-delta", id: "w1", delta: input.slice(0, 20) },
      { type: "tool-input-delta", id: "w1", delta: input.slice(20) },
      { type: "tool-input-end", id: "w1" },
      { type: "tool-result", toolName: "editFile", toolCallId: "w1", input: JSON.parse(input), output: { ok: true, op: "edit", path: PATH } },
      { type: "turn-committed" },
    ]);
    expect(of("activity")).toEqual([
      { toolCallId: "w1", op: "edit", path: "specs/requirements/features/F4-spending-reports.md", state: "writing" },
      { toolCallId: "w1", op: "edit", path: "specs/requirements/features/F4-spending-reports.md", state: "done" },
    ]);
    expect(of("wrote")).toEqual(["w1"]);
    expect(of("ended")).toEqual(["completed"]);
  });

  it("marks a write the bundle refused, and does not hand it on", async () => {
    const { of } = await fold([
      {
        type: "tool-result",
        toolName: "editFile",
        toolCallId: "w1",
        input: { path: PATH },
        output: { ok: false, op: "edit", path: PATH, code: "NOT_FOUND", message: "oldString did not match" },
      },
    ]);
    expect(of("activity")).toEqual([
      { toolCallId: "w1", op: "edit", path: "specs/requirements/features/F4-spending-reports.md", state: "failed", errorText: "oldString did not match" },
    ]);
    expect(of("wrote")).toEqual([]);
  });

  it("shows a batch's questions as they close, then the complete call as answerable", async () => {
    const input = { questions: [{ question: "A?", options: [] }, { question: "B?", options: [] }] };
    const json = JSON.stringify(input);
    const firstClose = json.indexOf("},{") + 1;
    const { of } = await fold([
      { type: "tool-input-start", id: "q1", toolName: "ask_questions" },
      { type: "tool-input-delta", id: "q1", delta: json.slice(0, firstClose) },
      { type: "tool-input-delta", id: "q1", delta: json.slice(firstClose) },
      { type: "tool-call", toolCallId: "q1", toolName: "ask_questions", input },
      { type: "turn-committed" },
    ]);
    expect(of("question")).toEqual([
      { toolCallId: "q1", questions: [{ question: "A?", options: [] }], streaming: true },
      { toolCallId: "q1", questions: input.questions, streaming: true },
      { toolCallId: "q1", questions: input.questions, streaming: false },
    ]);
  });

  it("withdraws a question the SDK rejected", async () => {
    const { of } = await fold([
      { type: "tool-call", toolCallId: "q1", toolName: "ask_question", input: { nope: true }, invalid: true },
    ]);
    expect(of("withdraw")).toEqual(["q1"]);
    expect(of("question")).toEqual([]);
  });

  it("says a failure once: a coded error is said by the turn-failed after it", async () => {
    const { of } = await fold([
      { type: "error", error: "429", code: "provider_limit" } as StreamPart,
      { type: "turn-failed", code: "provider_limit", host: "ollama.com" } as StreamPart,
    ]);
    expect(of("error")).toEqual(["ollama.com's usage limit is reached. Try again later."]);
    expect(of("ended")).toEqual(["failed"]);
  });

  it("settles a stream cut short by reading the turn's status", async () => {
    const { of } = await fold([{ type: "text-delta", delta: "Half" }], {
      turn: async () => ({ status: "completed" }) as never,
    });
    expect(of("ended")).toEqual(["completed"]);
  });

  it("leaves a turn still running when its stream is cut short, without ending it", async () => {
    const { of } = await fold([], { turn: async () => ({ status: "running" }) as never });
    expect(of("ended")).toEqual([]);
  });

  it("retries a stream not there yet, and settles from the status when the turn already ended", async () => {
    vi.useFakeTimers();
    try {
      const openStream = vi
        .fn<TurnStreamApi["openStream"]>()
        .mockRejectedValueOnce(new TurnStreamAttachError(404))
        .mockResolvedValueOnce(sse([{ type: "turn-committed" }]));
      const done = fold([], { openStream });
      await vi.runAllTimersAsync();
      const { of } = await done;
      expect(openStream).toHaveBeenCalledTimes(2);
      expect(of("ended")).toEqual(["completed"]);
    } finally {
      vi.useRealTimers();
    }
  });
});
