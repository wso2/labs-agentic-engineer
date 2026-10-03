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

// Resume (Review Focus 2): a stream that dies before its terminal re-attaches
// with `?from=<last id + 1>`, so every activity step renders exactly once. The
// real SSE parser and the real chat store run here; only the pod is faked.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mockOpenTurnStream = vi.fn();
const mockGetTurn = vi.fn();
const mockReadTurnStatus = vi.fn();
vi.mock("./api/turns.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./api/turns.js")>();
  return {
    ...actual,
    openTurnStream: (...args: unknown[]) => mockOpenTurnStream(...args),
    getTurn: (...args: unknown[]) => mockGetTurn(...args),
    readTurnStatus: (...args: unknown[]) => mockReadTurnStatus(...args),
  };
});

import { attachAndFoldTurn } from "./runTurn";
import { TurnStreamAttachError } from "./api/turns.js";
import { projectScope } from "./chatScope.js";
import { addMessage, getMessages, replaceMessages, type ChatMessage } from "./chatStore.js";
import { clearPlan } from "./planStore.js";

const KEY = "aep.chat.v1.acme.resume";
const TURN = "t-resume";

/** The pod's wire: one SSE frame per part, numbered by its buffer index. */
function sse(frames: { id: number; part: object }[], done = false): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  const text =
    frames.map(({ id, part }) => `id: ${id}\ndata: ${JSON.stringify(part)}\n\n`).join("") +
    (done ? "data: [DONE]\n\n" : "");
  return new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode(text));
      controller.close();
    },
  });
}

// One turn's buffer: a narration, then a file step (input stream + verdict),
// a second narration, then the terminal.
const BUFFER: object[] = [
  { type: "text-delta", delta: "Writing the PRD. " },
  { type: "tool-input-start", id: "c1", toolName: "addFile" },
  { type: "tool-input-delta", id: "c1", delta: '{"path":"specs/requirements/prd.md","content":"# PRD"}' },
  { type: "tool-input-end", id: "c1" },
  {
    type: "tool-result",
    toolCallId: "c1",
    toolName: "addFile",
    input: { path: "specs/requirements/prd.md" },
    output: { ok: true },
  },
  { type: "text-delta", delta: "Done." },
  { type: "turn-completed" },
];

/** Frames `from..to` (inclusive) of the buffer, with their ids. */
function frames(from: number, to: number): { id: number; part: object }[] {
  return BUFFER.slice(from, to + 1).map((part, i) => ({ id: from + i, part }));
}

const toolRows = (): Extract<ChatMessage, { role: "tool" }>[] =>
  getMessages(KEY).filter((m): m is Extract<ChatMessage, { role: "tool" }> => m.role === "tool");
const narration = (): string =>
  getMessages(KEY)
    .filter((m): m is Extract<ChatMessage, { role: "assistant" }> => m.role === "assistant")
    .map((m) => m.content)
    .join("");
const userRow = () => getMessages(KEY).find((m) => m.role === "user");

describe("attachAndFoldTurn — resume with ?from", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
    clearPlan(KEY);
    addMessage(KEY, { role: "user", content: "write it", turnId: TURN, status: "in_flight" });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("re-requests from=5 after ids 0-4 and renders every step once", async () => {
    mockOpenTurnStream
      .mockResolvedValueOnce(sse(frames(0, 4))) // the connection dies after id 4
      .mockResolvedValueOnce(sse(frames(5, 6), true));
    const onCompleted = vi.fn();

    await attachAndFoldTurn(KEY, projectScope("p"), TURN, new AbortController().signal, onCompleted);

    expect(mockOpenTurnStream.mock.calls.map((c) => c[2])).toEqual([0, 5]);
    expect(toolRows()).toHaveLength(1);
    expect(toolRows()[0]).toMatchObject({ toolCallId: "c1", status: "done", ok: true });
    expect(narration()).toBe("Writing the PRD. Done.");
    expect(userRow()).toMatchObject({ status: "completed" });
    expect(onCompleted).toHaveBeenCalledTimes(1);
    expect(mockGetTurn).not.toHaveBeenCalled();
    expect(mockReadTurnStatus).not.toHaveBeenCalled();
  });

  it("never folds a frame twice when the re-attach replays one already read", async () => {
    mockOpenTurnStream
      .mockResolvedValueOnce(sse(frames(0, 5)))
      .mockResolvedValueOnce(sse(frames(4, 6), true)); // overlaps ids 4 and 5

    await attachAndFoldTurn(KEY, projectScope("p"), TURN, new AbortController().signal);

    expect(mockOpenTurnStream.mock.calls.map((c) => c[2])).toEqual([0, 6]);
    expect(toolRows()).toHaveLength(1);
    expect(narration()).toBe("Writing the PRD. Done.");
  });

  it("keeps resuming across several drops", async () => {
    mockOpenTurnStream
      .mockResolvedValueOnce(sse(frames(0, 1)))
      .mockResolvedValueOnce(sse(frames(2, 3)))
      .mockResolvedValueOnce(sse(frames(4, 6), true));

    await attachAndFoldTurn(KEY, projectScope("p"), TURN, new AbortController().signal);

    expect(mockOpenTurnStream.mock.calls.map((c) => c[2])).toEqual([0, 2, 4]);
    expect(toolRows()).toHaveLength(1);
    expect(narration()).toBe("Writing the PRD. Done.");
    expect(userRow()).toMatchObject({ status: "completed" });
  });

  it("settles from GET turns/{t} when the re-attach answers 404", async () => {
    mockOpenTurnStream
      .mockResolvedValueOnce(sse(frames(0, 2)))
      .mockRejectedValue(new TurnStreamAttachError(404)); // past the 120 s retention
    mockReadTurnStatus.mockResolvedValue({ kind: "status", status: { status: "completed" } });
    const onCompleted = vi.fn();

    await expect(attachAndFoldTurn(KEY, projectScope("p"), TURN, new AbortController().signal, onCompleted)).resolves.toBe(true);

    expect(mockOpenTurnStream.mock.calls.map((c) => c[2])).toEqual([0, 3]);
    expect(mockReadTurnStatus).toHaveBeenCalledWith(projectScope("p"), TURN);
    expect(userRow()).toMatchObject({ status: "completed" });
    expect(onCompleted).toHaveBeenCalledTimes(1);
  });

  it("settles a failed turn from its status after a 404 re-attach", async () => {
    mockOpenTurnStream
      .mockResolvedValueOnce(sse(frames(0, 2)))
      .mockRejectedValue(new TurnStreamAttachError(404));
    mockReadTurnStatus.mockResolvedValue({
      kind: "status",
      status: { status: "failed", reason: "agent-error", message: "boom" },
    });

    await attachAndFoldTurn(KEY, projectScope("p"), TURN, new AbortController().signal);

    expect(userRow()).toMatchObject({ status: "failed" });
    expect(getMessages(KEY).filter((m) => m.role === "error")).toHaveLength(1);
  });

  it("stops at a turn the pod no longer knows, without a second read", async () => {
    mockOpenTurnStream
      .mockResolvedValueOnce(sse(frames(0, 2)))
      .mockRejectedValue(new TurnStreamAttachError(404));
    mockReadTurnStatus.mockResolvedValue({ kind: "gone" });

    await expect(attachAndFoldTurn(KEY, projectScope("p"), TURN, new AbortController().signal)).resolves.toBe(false);

    expect(mockOpenTurnStream).toHaveBeenCalledTimes(2);
    expect(mockReadTurnStatus).toHaveBeenCalledTimes(1);
    expect(mockGetTurn).not.toHaveBeenCalled();
  });

  it("re-attaches after a pod outage instead of giving up on the turn", async () => {
    vi.useFakeTimers();
    mockOpenTurnStream
      .mockResolvedValueOnce(sse(frames(0, 4)))
      .mockRejectedValueOnce(new TurnStreamAttachError(503))
      .mockResolvedValueOnce(sse(frames(5, 6), true));

    const done = attachAndFoldTurn(KEY, projectScope("p"), TURN, new AbortController().signal);
    await vi.runAllTimersAsync();
    await done;

    expect(mockOpenTurnStream.mock.calls.map((c) => c[2])).toEqual([0, 5, 5]);
    expect(toolRows()).toHaveLength(1);
    expect(userRow()).toMatchObject({ status: "completed" });
  });

  it("falls to one status read when the stream stays unreachable", async () => {
    vi.useFakeTimers();
    mockOpenTurnStream
      .mockResolvedValueOnce(sse(frames(0, 2)))
      .mockRejectedValue(new TurnStreamAttachError(503));
    mockGetTurn.mockResolvedValue({ status: "running" });

    const done = attachAndFoldTurn(KEY, projectScope("p"), TURN, new AbortController().signal);
    await vi.runAllTimersAsync();
    // Unsettled: the caller learns its log holds a turn that has not ended.
    await expect(done).resolves.toBe(false);

    // Bounded: the turn is left running for the active-turn watch to re-attach.
    expect(mockOpenTurnStream.mock.calls.length).toBeLessThanOrEqual(5);
    expect(mockGetTurn).toHaveBeenCalledTimes(1);
    expect(userRow()).toMatchObject({ status: "in_flight" });
  });
});
