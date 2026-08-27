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
  toChange: (part: { toolCallId?: string; result?: unknown }) => ({
    op: "add",
    path: "specs/design/components/checkout-api/design.json",
    result: part.result,
  }),
  opForTool: () => "add",
  readToolInputPath: (buf: string) => mockReadToolInputPath(buf),
  // questionCards.ts reads these through this same module; a frame carrying a
  // tool name reaches isQuestionTool, so the mock has to carry them.
  ASK_QUESTION_TOOL: "ask_question",
  ASK_QUESTIONS_TOOL: "ask_questions",
  buildAnswerInstruction: () => "",
  buildAnswersInstruction: () => "",
}));

const notified: { key: string; status: string }[] = [];
vi.mock("./chatStore.js", () => ({
  appendAssistantText: vi.fn(),
  addMessage: vi.fn(),
  upsertToolMessage: vi.fn(),
  upsertQuestionMessage: vi.fn(),
  setTurnStatus: vi.fn(),
  notifyTurnEnd: (key: string, status: string) => notified.push({ key, status }),
}));

import { attachAndFoldTurn } from "./runTurn";
import { TurnStreamAttachError } from "./api/turns.js";
import { addMessage, upsertToolMessage } from "./chatStore.js";
import { clearRegisterDraft, peekRegisterDraft } from "./registerDraftStore.js";

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
 * The per-file spinner. One step can carry several file writes, and the SDK
 * flushes every `tool-result` for that step only after its LAST call — so
 * `tool-result` marks the end of the STEP, not of one file. Keying the spinner
 * off it made a batched design turn show five files "loading" for the whole
 * batch and settle them together, long after the first ones had landed.
 * `tool-input-end` is the per-file signal (for a file tool the input IS the body).
 */
describe("attachAndFoldTurn — a file card settles on its OWN input-end, not the step's results", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queuedParts = [];
    notified.length = 0;
    mockReadToolInputPath.mockReturnValue(null);
    mockOpenTurnStream.mockResolvedValue(new ReadableStream());
  });

  /** The frames one batched step produces for N files (see services/agents/test/frame-order.test.ts). */
  const batch = (ids: string[]): StreamPart[] => [
    ...ids.flatMap(
      (id) =>
        [
          { type: "tool-input-start", id, toolName: "addFile" },
          { type: "tool-input-delta", id, delta: `{"path":"specs/${id}.md","content":"x"}` },
          { type: "tool-input-end", id },
          { type: "tool-call", toolCallId: id, toolName: "addFile", input: {} },
        ] as StreamPart[],
    ),
    // Every result arrives only here, after the last call.
    ...ids.map((id) => ({ type: "tool-result", toolCallId: id, toolName: "addFile", result: { ok: true } }) as StreamPart),
  ];

  const cardsFor = (id: string) =>
    vi.mocked(upsertToolMessage).mock.calls.map(([, m]) => m).filter((m) => m.toolCallId === id);

  it("stops the spinner at tool-input-end, with NO verdict yet", async () => {
    mockReadToolInputPath.mockReturnValue("specs/design/design.md");
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
    mockReadToolInputPath.mockReturnValue("specs/design/design.md");
    queuedParts = batch(["c1", "c2", "c3"]);
    await attachAndFoldTurn(KEY, "proj1", "t1", new AbortController().signal);

    const calls = vi.mocked(upsertToolMessage).mock.calls.map(([, m]) => m);
    const c1Done = calls.findIndex((m) => m.toolCallId === "c1" && m.status === "done");
    const c3Streaming = calls.findIndex((m) => m.toolCallId === "c3" && m.status === "streaming");
    expect(c1Done).toBeGreaterThanOrEqual(0);
    expect(c3Streaming).toBeGreaterThanOrEqual(0);
    expect(c1Done).toBeLessThan(c3Streaming);
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
