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

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StreamPart } from "@aep/agent-stream";

// --- api/turns.js: openTurnStream/getTurn are the only calls runTurn makes.
// Pass through TurnStreamAttachError / isTurnStreamNotFound so attach tests
// exercise the real discriminator.
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

// --- @aep/agent-stream: parseSseStream is mocked to yield the parts a test
// queues, bypassing real SSE byte parsing (irrelevant to this unit).
// `readToolInputPath` is controllable so a test can make a path resolve mid
// tool-input and exercise the file-card lifecycle; it returns null by default,
// which is "no path yet" — no card.
let queuedParts: StreamPart[] = [];
const mockReadToolInputPath = vi.fn<(buf: string) => string | null>(() => null);
vi.mock("@aep/agent-stream", () => ({
  parseSseStream: async function* () {
    for (const part of queuedParts) yield part;
  },
  // `path` is read off the frame when a test supplies one, so a test can settle
  // a SPECIFIC file; the fixed default keeps the older card tests unchanged.
  toChange: (part: { toolCallId?: string; result?: unknown; path?: string }) => ({
    op: "add",
    path: part.path ?? "specs/design/components/checkout-api/design.json",
    result: part.result,
  }),
  opForTool: () => "add",
  readToolInputPath: (buf: string) => mockReadToolInputPath(buf),
  // questionCards.ts reads these through this same module; a frame carrying a
  // tool name reaches isQuestionTool, so the mock has to carry them.
  ASK_QUESTION_TOOL: "ask_question",
  ASK_QUESTIONS_TOOL: "ask_questions",
  isQuestionTool: (name?: string) => name === "ask_question" || name === "ask_questions",
  DECLARE_PLAN_TOOL: "declare_plan",
  buildAnswerInstruction: () => "",
  buildAnswersInstruction: () => "",
}));

const notified: { key: string; status: string }[] = [];
vi.mock("./chatStore.js", () => ({
  appendAssistantText: vi.fn(),
  addMessage: vi.fn(),
  upsertToolMessage: vi.fn(),
  upsertQuestionMessage: vi.fn(),
  dropQuestionMessage: vi.fn(),
  upsertPlanMessage: vi.fn(),
  setTurnStatus: vi.fn(),
  notifyTurnEnd: (key: string, status: string) => notified.push({ key, status }),
}));

// providerWait.js: record the wait edges in order, so a test can see the
// label set by a provider-wait frame and cleared by the next frame.
const waitOps: string[] = [];
vi.mock("./providerWait.js", () => ({
  setProviderWait: (_key: string, host: string) => waitOps.push(`set:${host}`),
  clearProviderWait: () => waitOps.push("clear"),
}));

import { attachAndFoldTurn } from "./runTurn";
import { TurnStreamAttachError } from "./api/turns.js";
import { addMessage, dropQuestionMessage, upsertQuestionMessage, upsertToolMessage } from "./chatStore.js";
import { clearRegisterDraft, peekRegisterDraft } from "./registerDraftStore.js";
import { upsertPlanMessage } from "./chatStore.js";
import { clearPlan, peekPlan } from "./planStore.js";

const KEY = "aep.chat.v1.acme.proj1";

describe("attachAndFoldTurn — turn-end notification (#252 Task 5)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queuedParts = [];
    notified.length = 0;
    mockOpenTurnStream.mockResolvedValue(new ReadableStream());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("notifies turn-end with 'completed' on a turn-completed terminal frame", async () => {
    queuedParts = [{ type: "turn-completed" } as StreamPart];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(notified).toEqual([{ key: KEY, status: "completed" }]);
  });

  it("calls onCompleted exactly once on a turn-completed terminal frame", async () => {
    queuedParts = [{ type: "text-delta", delta: "done" }, { type: "turn-completed" } as StreamPart];
    const onCompleted = vi.fn();
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal, onCompleted);
    expect(onCompleted).toHaveBeenCalledTimes(1);
    expect(mockGetTurn).not.toHaveBeenCalled(); // the frame is the terminal; no fallback poll
  });

  it("never calls onCompleted for a failed turn", async () => {
    queuedParts = [{ type: "turn-failed", reason: "agent-error", message: "boom" } as StreamPart];
    const onCompleted = vi.fn();
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal, onCompleted);
    expect(onCompleted).not.toHaveBeenCalled();
  });

  it("notifies turn-end with 'failed' on a turn-failed terminal frame", async () => {
    queuedParts = [{ type: "turn-failed", message: "boom" } as StreamPart];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(notified).toEqual([{ key: KEY, status: "failed" }]);
  });

  it("notifies turn-end via the poll fallback when the stream is severed with no terminal frame", async () => {
    queuedParts = []; // stream ends with nothing — severed before a terminal
    mockGetTurn.mockResolvedValue({ status: "completed" });
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(notified).toEqual([{ key: KEY, status: "completed" }]);
  });

  it("notifies turn-end 'failed' via the poll fallback when the authoritative poll says failed", async () => {
    queuedParts = [];
    mockGetTurn.mockResolvedValue({ status: "failed", message: "oops" });
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(notified).toEqual([{ key: KEY, status: "failed" }]);
  });

  it("does NOT notify turn-end when the signal is aborted (detach, not a terminal)", async () => {
    const ac = new AbortController();
    queuedParts = []; // aborted before any frame arrives
    ac.abort();
    await attachAndFoldTurn(KEY, "proj1", "t1", ac.signal);
    expect(notified).toEqual([]);
    expect(mockGetTurn).not.toHaveBeenCalled();
  });
});

describe("attachAndFoldTurn — pre-stream 404 re-attach (#3)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queuedParts = [];
    notified.length = 0;
    mockOpenTurnStream.mockResolvedValue(new ReadableStream());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("retries openTurnStream on a pre-stream 404, then folds the stream (no Turn failed)", async () => {
    vi.useFakeTimers();
    const attachErr = new TurnStreamAttachError(404);
    mockOpenTurnStream
      .mockRejectedValueOnce(attachErr)
      .mockResolvedValueOnce(new ReadableStream());
    queuedParts = [{ type: "turn-completed" } as StreamPart];
    mockGetTurn.mockResolvedValue({ status: "running" });

    const done = attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    await vi.runAllTimersAsync();
    await done;

    expect(mockOpenTurnStream).toHaveBeenCalledTimes(2);
    expect(notified).toEqual([{ key: KEY, status: "completed" }]);
    expect(addMessage).not.toHaveBeenCalledWith(
      KEY,
      expect.objectContaining({ role: "error" }),
    );
  });

  it("falls through to getTurn when a pre-stream 404's turn is already completed", async () => {
    vi.useFakeTimers();
    const attachErr = new TurnStreamAttachError(404);
    mockOpenTurnStream.mockRejectedValue(attachErr);
    mockGetTurn.mockResolvedValue({ status: "completed" });

    const done = attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    await vi.runAllTimersAsync();
    await done;

    expect(notified).toEqual([{ key: KEY, status: "completed" }]);
  });

  // 409 replay_truncated: the running turn overflowed its replay buffer, so a
  // replay would have a gap. The pod says to attach again after the turn ends;
  // the status read is what settles it, without folding a gapped stream.
  describe("a truncated replay", () => {
    const status = (s: string) => ({ kind: "status", status: { status: s } });
    const truncated = () => mockOpenTurnStream.mockRejectedValue(new TurnStreamAttachError(409, "replay_truncated"));

    it("waits out the turn on its status, then settles from it", async () => {
      vi.useFakeTimers();
      truncated();
      mockReadTurnStatus
        .mockResolvedValueOnce(status("running"))
        .mockResolvedValueOnce(status("running"))
        .mockResolvedValueOnce(status("completed"));
      const onCompleted = vi.fn();

      const done = attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal, onCompleted);
      await vi.runAllTimersAsync();
      await done;

      expect(mockOpenTurnStream).toHaveBeenCalledTimes(1);
      expect(mockReadTurnStatus).toHaveBeenCalledTimes(3);
      expect(notified).toEqual([{ key: KEY, status: "completed" }]);
      expect(onCompleted).toHaveBeenCalledTimes(1);
    });

    // A 503 while the pod rolls, a network blip, AE Studio briefly not ready:
    // none of them says the turn ended, so none of them ends the wait.
    it("keeps polling through a failed read and settles once the turn ends", async () => {
      vi.useFakeTimers();
      truncated();
      mockReadTurnStatus
        .mockResolvedValueOnce({ kind: "unavailable" })
        .mockResolvedValueOnce({ kind: "unavailable" })
        .mockResolvedValueOnce(status("completed"));
      const onCompleted = vi.fn();

      const done = attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal, onCompleted);
      await vi.runAllTimersAsync();
      await done;

      expect(mockReadTurnStatus).toHaveBeenCalledTimes(3);
      expect(notified).toEqual([{ key: KEY, status: "completed" }]);
      expect(onCompleted).toHaveBeenCalledTimes(1);
    });

    // A 404: the pod no longer holds the turn, so no later read can answer.
    // The wait stops and the severed-stream handling takes over.
    it("stops on a turn the pod no longer holds and falls to the severed-stream handling", async () => {
      vi.useFakeTimers();
      truncated();
      mockReadTurnStatus.mockResolvedValue({ kind: "gone" });
      mockGetTurn.mockResolvedValue(null);

      const done = attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
      await vi.runAllTimersAsync();
      await done;

      expect(mockReadTurnStatus).toHaveBeenCalledTimes(1);
      expect(mockGetTurn).toHaveBeenCalledTimes(1); // the severed-stream poll
      expect(notified).toEqual([]);
    });

    it("stops polling when the view detaches", async () => {
      vi.useFakeTimers();
      truncated();
      mockReadTurnStatus.mockResolvedValue(status("running"));
      const ac = new AbortController();

      const done = attachAndFoldTurn(KEY, "proj1", "t1", ac.signal);
      await vi.advanceTimersByTimeAsync(5_000);
      const before = mockReadTurnStatus.mock.calls.length;
      ac.abort();
      await vi.runAllTimersAsync();
      await done;

      expect(before).toBeGreaterThanOrEqual(1);
      expect(mockReadTurnStatus.mock.calls.length).toBe(before);
      expect(notified).toEqual([]);
      expect(mockGetTurn).not.toHaveBeenCalled();
    });
  });

  it("re-throws non-404 attach failures (still surfaces Turn failed upstream)", async () => {
    mockOpenTurnStream.mockRejectedValue(new Error("Failed to attach to the turn stream")); // no status
    await expect(
      attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal),
    ).rejects.toThrow(/Failed to attach/);
  });
});

/**
 * The per-file spinner and tick. Two facts, two frames: `tool-input-end` says
 * the BODY is written (for a file tool the input IS the body), the verdict says
 * the bundle accepted it — and the card must never conflate them, or a rejected
 * write would show a success tick.
 *
 * Both wire orders are exercised here, because the console has to fold either:
 *  - verdict per call (what the agents service emits today — write-ledger.ts);
 *  - every verdict trailing the last call (the raw SDK order, which recorded
 *    streams and older producers still carry).
 */
describe("attachAndFoldTurn — a file card settles on its OWN input-end, not the step's results", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queuedParts = [];
    notified.length = 0;
    mockReadToolInputPath.mockReturnValue(null);
    mockOpenTurnStream.mockResolvedValue(new ReadableStream());
  });

  const callFrames = (id: string): StreamPart[] =>
    [
      { type: "tool-input-start", id, toolName: "addFile" },
      { type: "tool-input-delta", id, delta: `{"path":"specs/${id}.md","content":"x"}` },
      { type: "tool-input-end", id },
      { type: "tool-call", toolCallId: id, toolName: "addFile", input: {} },
    ] as StreamPart[];

  const resultFrame = (id: string, ok = true): StreamPart =>
    ({ type: "tool-result", toolCallId: id, toolName: "addFile", result: { ok } }) as StreamPart;

  /** The raw SDK order: every verdict trails the LAST call of the step. */
  const batch = (ids: string[]): StreamPart[] => [
    ...ids.flatMap(callFrames),
    ...ids.map((id) => resultFrame(id)),
  ];

  /** What the agents service emits: each verdict rides its own call. */
  const batchSettledPerCall = (ids: string[]): StreamPart[] =>
    ids.flatMap((id) => [...callFrames(id), resultFrame(id)]);

  const cardsFor = (id: string) =>
    vi.mocked(upsertToolMessage).mock.calls.map(([, m]) => m).filter((m) => m.toolCallId === id);

  it("stops the spinner at tool-input-end, with NO verdict yet", async () => {
    mockReadToolInputPath.mockReturnValue("specs/design/domain-model.md");
    queuedParts = batch(["c1"]);
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);

    const cards = cardsFor("c1");
    expect(cards.map((c) => c.status)).toEqual(["streaming", "done", "done"]);
    // The middle write is the new one: body complete, bundle not yet heard from.
    // `ok` must be absent — guessing `true` would paint a success tick on a
    // write the write-gates may still reject.
    expect(cards[1]!.ok).toBeUndefined();
    expect(cards[2]!.ok).toBe(true); // the result settles it
    // The store MERGES onto the existing card, so an `ok` written at ANY earlier
    // stage would survive into the settled one. Only the result may set it.
    expect(cards.slice(0, -1).every((c) => !("ok" in c) || c.ok === undefined)).toBe(true);
  });

  it("settles the FIRST file before the last file's call — the batch no longer blocks it", async () => {
    mockReadToolInputPath.mockReturnValue("specs/design/domain-model.md");
    queuedParts = batch(["c1", "c2", "c3"]);
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);

    const calls = vi.mocked(upsertToolMessage).mock.calls.map(([, m]) => m);
    const c1Done = calls.findIndex((m) => m.toolCallId === "c1" && m.status === "done");
    const c3Streaming = calls.findIndex((m) => m.toolCallId === "c3" && m.status === "streaming");
    expect(c1Done).toBeGreaterThanOrEqual(0);
    expect(c3Streaming).toBeGreaterThanOrEqual(0);
    expect(c1Done).toBeLessThan(c3Streaming);
  });

  it("ticks the FIRST file mid-batch when its verdict rides its own call", async () => {
    mockReadToolInputPath.mockReturnValue("specs/design/domain-model.md");
    queuedParts = batchSettledPerCall(["c1", "c2", "c3"]);
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);

    const calls = vi.mocked(upsertToolMessage).mock.calls.map(([, m]) => m);
    const c1Ticked = calls.findIndex((m) => m.toolCallId === "c1" && m.ok === true);
    const c3Streaming = calls.findIndex((m) => m.toolCallId === "c3" && m.status === "streaming");
    expect(c1Ticked).toBeGreaterThanOrEqual(0);
    // The whole point of the per-call verdict: file 1 carries a tick while file 3
    // is still being written, instead of a verdict-less ring for the whole batch.
    expect(c1Ticked).toBeLessThan(c3Streaming);
    expect(cardsFor("c1").map((c) => c.ok)).toEqual([undefined, undefined, true]);
  });

  it("writes no card when the path never resolved (nothing to settle)", async () => {
    mockReadToolInputPath.mockReturnValue(null); // path never parses out of the buffer
    queuedParts = [
      { type: "tool-input-start", id: "c1", toolName: "addFile" } as StreamPart,
      { type: "tool-input-delta", id: "c1", delta: "{" } as StreamPart,
      { type: "tool-input-end", id: "c1" } as StreamPart,
    ];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(upsertToolMessage).not.toHaveBeenCalled();
  });
});

describe("attachAndFoldTurn — draftExternalResource publishes a register draft", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queuedParts = [];
    notified.length = 0;
    mockOpenTurnStream.mockResolvedValue(new ReadableStream());
    clearRegisterDraft(KEY);
  });

  it("publishes a parsed draft from a complete draftExternalResource tool-call", async () => {
    const draft = {
      name: "stripe",
      description: "Payments API",
      consumptionInstructions: "Use the secret key as Bearer.",
      config: [{ key: "API_KEY", description: "Secret", secret: true }],
    };
    queuedParts = [
      {
        type: "tool-call",
        toolCallId: "d1",
        toolName: "draftExternalResource",
        input: draft,
      } as StreamPart,
    ];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(peekRegisterDraft(KEY)).toEqual(draft);
    expect(upsertToolMessage).not.toHaveBeenCalled();
  });
});

describe("attachAndFoldTurn — declare_plan folds into the plan store (#576)", () => {
  const CELL = "specs/design/design.cell";
  const OVERVIEW = "specs/design/domain-model.md";
  const PORTAL = "specs/design/components/portal/design.json";

  beforeEach(() => {
    vi.clearAllMocks();
    queuedParts = [];
    notified.length = 0;
    clearPlan(KEY);
    mockOpenTurnStream.mockResolvedValue(new ReadableStream());
  });

  afterEach(() => clearPlan(KEY));

  it("unions the waves, dedupes the double publish, and rows the chat once per call", async () => {
    const wave1 = { paths: [CELL, OVERVIEW] };
    const wave2 = { paths: [OVERVIEW, PORTAL] }; // OVERVIEW restated on purpose
    queuedParts = [
      // Wave one arrives twice — streamed input, then the complete call — the
      // belt-and-braces pair the union must collapse to one publication.
      { type: "tool-input-start", id: "p1", toolName: "declare_plan" },
      { type: "tool-input-delta", id: "p1", delta: JSON.stringify(wave1) },
      { type: "tool-input-end", id: "p1" },
      { type: "tool-call", toolCallId: "p1", toolName: "declare_plan", input: wave1 },
      { type: "tool-call", toolCallId: "p2", toolName: "declare_plan", input: wave2 },
      { type: "turn-failed", message: "died" },
    ] as StreamPart[];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(peekPlan(KEY)?.entries.map((e) => e.path)).toEqual([CELL, OVERVIEW, PORTAL]);
    expect(vi.mocked(upsertPlanMessage).mock.calls.map(([, m]) => [m.toolCallId, m.added, m.grew]))
      .toEqual([
        ["p1", 2, false],
        ["p2", 1, true],
      ]);
  });

  it("derives writing/done/error from the file frames and keeps the wreckage", async () => {
    mockReadToolInputPath.mockImplementation((buf: string) =>
      buf.includes("design.cell") ? CELL : buf.includes("domain-model.md") ? OVERVIEW : null,
    );
    queuedParts = [
      {
        type: "tool-call",
        toolCallId: "p1",
        toolName: "declare_plan",
        input: { paths: [CELL, OVERVIEW, PORTAL] },
      },
      { type: "tool-input-start", id: "f1", toolName: "addFile" },
      { type: "tool-input-delta", id: "f1", delta: '{"path":"specs/design/design.cell"' },
      { type: "tool-input-end", id: "f1" },
      // The VERDICT is what ticks it — the body being complete is not enough.
      {
        type: "tool-result",
        toolName: "addFile",
        toolCallId: "f1",
        path: "specs/design/design.cell",
        result: { ok: true },
      },
      { type: "tool-input-start", id: "f2", toolName: "addFile" },
      { type: "tool-input-delta", id: "f2", delta: '{"path":"specs/design/domain-model.md"' },
      { type: "turn-failed", message: "died mid-write" },
    ] as StreamPart[];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    const plan = peekPlan(KEY);
    expect(plan?.wreckage).toBe(true);
    expect(plan?.entries.map((e) => e.status)).toEqual(["done", "error", "planned"]);
  });

  // A restructure is removeFile + addFile. Following the removal would send the
  // editor to a document about to vanish, and settling it would tick a deleted
  // file green — so a removal moves nothing.
  it("a removeFile neither follows nor settles", async () => {
    mockReadToolInputPath.mockImplementation(() => CELL);
    queuedParts = [
      { type: "tool-call", toolCallId: "p1", toolName: "declare_plan", input: { paths: [CELL] } },
      { type: "tool-input-start", id: "r1", toolName: "removeFile" },
      { type: "tool-input-delta", id: "r1", delta: '{"path":"specs/design/design.cell"}' },
      { type: "tool-input-end", id: "r1" },
      { type: "turn-failed", message: "died" },
    ] as StreamPart[];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    const plan = peekPlan(KEY);
    expect(plan?.entries[0]?.status).toBe("planned");
    expect(plan?.writingPath).toBe(null);
  });

  it("a committed turn dissolves the plan entirely", async () => {
    queuedParts = [
      { type: "tool-call", toolCallId: "p1", toolName: "declare_plan", input: { paths: [CELL] } },
      { type: "turn-completed" },
    ] as StreamPart[];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(peekPlan(KEY)).toBe(null);
  });
});

describe("attachAndFoldTurn — a question call the schema rejected is not a card", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queuedParts = [];
    notified.length = 0;
    mockOpenTurnStream.mockResolvedValue(new ReadableStream());
  });

  const good = { question: "Which provider?", options: [{ label: "A" }] };

  it("skips the invalid call and withdraws any prefix that streamed onto a card", async () => {
    queuedParts = [
      { type: "tool-input-start", id: "q-bad", toolName: "ask_questions" },
      { type: "tool-input-delta", id: "q-bad", delta: JSON.stringify({ questions: [good] }).slice(0, -2) },
      { type: "tool-call", toolCallId: "q-bad", toolName: "ask_questions", input: { questions: [good] }, invalid: true },
      { type: "tool-error", toolCallId: "q-bad", toolName: "ask_questions", error: "invalid" },
      { type: "tool-call", toolCallId: "q-good", toolName: "ask_question", input: good },
      { type: "turn-completed" },
    ] as StreamPart[];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    // The prefix DID reach the log as a streaming card before the verdict…
    const streamed = vi.mocked(upsertQuestionMessage).mock.calls.map(([, m]) => m);
    expect(streamed.some((m) => m.toolCallId === "q-bad" && m.streaming)).toBe(true);
    // …and the rejection withdrew it.
    expect(vi.mocked(dropQuestionMessage)).toHaveBeenCalledWith(KEY, "q-bad");
    const finals = vi
      .mocked(upsertQuestionMessage)
      .mock.calls.map(([, m]) => m)
      .filter((m) => !m.streaming);
    expect(finals.map((m) => m.toolCallId)).toEqual(["q-good"]);
  });
});

describe("attachAndFoldTurn — a provider wait is status until the model answers", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queuedParts = [];
    waitOps.length = 0;
    mockOpenTurnStream.mockResolvedValue(new ReadableStream());
  });

  it("sets the wait on a provider-wait frame and clears it on the next frame", async () => {
    queuedParts = [
      { type: "start" },
      { type: "provider-wait", host: "ollama.com" } as StreamPart,
      { type: "start-step" },
      { type: "text-delta", delta: "hi" },
      { type: "turn-completed" } as StreamPart,
    ];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    const set = waitOps.indexOf("set:ollama.com");
    expect(set).toBeGreaterThan(-1);
    expect(waitOps.filter((op) => op.startsWith("set:"))).toHaveLength(1);
    expect(waitOps[set + 1]).toBe("clear"); // the start-step: the model answered
    expect(addMessage).not.toHaveBeenCalled(); // status, never a chat row
  });

  it("never sets a wait on a turn without one", async () => {
    queuedParts = [{ type: "start-step" }, { type: "text-delta", delta: "hi" }, { type: "turn-completed" } as StreamPart];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(waitOps.some((op) => op.startsWith("set:"))).toBe(false);
  });
});

describe("attachAndFoldTurn — a failure the agents service named", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queuedParts = [];
    mockOpenTurnStream.mockResolvedValue(new ReadableStream());
  });

  it("renders a provider limit once, from the terminal, naming the host and the reset", async () => {
    const resetAt = "2026-09-26T14:05:00.000Z";
    queuedParts = [
      { type: "error", code: "provider_limit", error: `ollama.com's usage limit is reached. Try again after ${resetAt}.`, host: "ollama.com", resetAt } as StreamPart,
      { type: "turn-failed", reason: "agent-error", code: "provider_limit", host: "ollama.com", resetAt, message: "raw" } as StreamPart,
    ];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(addMessage).toHaveBeenCalledTimes(1);
    const row = vi.mocked(addMessage).mock.calls[0]![1] as { role: string; content: string };
    expect(row.role).toBe("error");
    expect(row.content).toMatch(/^ollama\.com's usage limit is reached\. Try again after /);
    expect(row.content).not.toContain(resetAt); // the reader's local time, not the wire's ISO
  });

  it("still rows an uncoded error frame", async () => {
    queuedParts = [{ type: "error", error: "boom" }, { type: "turn-failed", message: "stream ended without a manifest" } as StreamPart];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(addMessage).toHaveBeenCalledTimes(2);
  });

  it("phrases the code off the status read when the stream severed before the terminal", async () => {
    const message = "The model's output limit (8192 tokens per step) cut off addFile before it finished, so nothing was written.";
    queuedParts = [];
    mockGetTurn.mockResolvedValue({ status: "failed", reason: "agent-error", code: "output_truncated", message });
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(addMessage).toHaveBeenCalledWith(KEY, { role: "error", content: message });
  });
});
