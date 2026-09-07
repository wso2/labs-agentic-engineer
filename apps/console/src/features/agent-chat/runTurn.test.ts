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
vi.mock("./api/turns.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./api/turns.js")>();
  return {
    ...actual,
    openTurnStream: (...args: unknown[]) => mockOpenTurnStream(...args),
    getTurn: (...args: unknown[]) => mockGetTurn(...args),
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
  upsertPlanMessage: vi.fn(),
  setTurnStatus: vi.fn(),
  notifyTurnEnd: (key: string, status: string) => notified.push({ key, status }),
}));

import { attachAndFoldTurn } from "./runTurn";
import { TurnStreamAttachError } from "./api/turns.js";
import { addMessage, upsertToolMessage } from "./chatStore.js";
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

  it("notifies turn-end with 'completed' on a turn-committed terminal frame", async () => {
    queuedParts = [{ type: "turn-committed" } as StreamPart];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(notified).toEqual([{ key: KEY, status: "completed" }]);
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
    queuedParts = [{ type: "turn-committed" } as StreamPart];
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
      { type: "turn-committed" },
    ] as StreamPart[];
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);
    expect(peekPlan(KEY)).toBe(null);
  });
});
