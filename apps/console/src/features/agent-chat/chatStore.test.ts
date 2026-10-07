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
import type { ConversationMessage } from "./api/conversation";
import { ConversationRotatedError, TurnInProgressError, type TurnStatus } from "./api/turns";
import { createChatStore, type ChatApi } from "./chatStore";
import type { TurnBody, TurnScope } from "./turnScope";

// The store's rules, on a fake server: the turn lifecycle, one turn at a
// time, the scope on every turn, reattaching after a reload, and a card's
// answer going out as the next turn. Streams are real SSE bytes, read by the
// real parser.

const PROJECT = "acme";
const F4: TurnScope = { kind: "feature", featureId: "F4" };
const PRODUCT: TurnScope = { kind: "product" };

/** A stream the test feeds frame by frame, and ends when it chooses. */
function controlledStream() {
  const encoder = new TextEncoder();
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  const body = new ReadableStream<Uint8Array>({ start: (c) => void (controller = c) });
  return {
    body,
    send: (part: StreamPart) => controller.enqueue(encoder.encode(`data: ${JSON.stringify(part)}\n\n`)),
    end: () => {
      controller.enqueue(encoder.encode("data: [DONE]\n\n"));
      controller.close();
    },
  };
}

/** A stream that plays these frames and ends. */
function sse(parts: StreamPart[]): ReadableStream<Uint8Array> {
  const s = controlledStream();
  for (const p of parts) s.send(p);
  s.end();
  return s.body;
}

function running(turnId: string, instruction?: string): TurnStatus {
  return {
    turnId,
    conversationId: "conv-1",
    useCase: "general",
    status: "running",
    createdAt: "2026-09-30T09:00:00Z",
    updatedAt: "2026-09-30T09:00:00Z",
    ...(instruction ? { instruction } : {}),
  };
}

function setup(
  options: {
    history?: ConversationMessage[];
    active?: TurnStatus | null;
    api?: Partial<ChatApi>;
    beforeTurn?: (projectName: string) => Promise<void>;
  } = {},
) {
  const started: TurnBody[] = [];
  let next = 0;
  const streams = new Map<string, ReadableStream<Uint8Array>>();
  const api: ChatApi = {
    conversationId: vi.fn(async () => "conv-1"),
    history: vi.fn(async () => options.history ?? []),
    activeTurn: vi.fn(async () => options.active ?? null),
    startTurn: vi.fn(async (_p: string, _c: string, body: TurnBody) => {
      started.push(body);
      return `t${++next}`;
    }),
    turn: vi.fn(async () => null),
    openStream: vi.fn(async (_p: string, turnId: string) => streams.get(turnId) ?? sse([{ type: "turn-committed" }])),
    ...options.api,
  };
  const onAgentWrite = vi.fn();
  const store = createChatStore({
    api,
    onAgentWrite,
    pollDelay: () => 60_000,
    ...(options.beforeTurn ? { beforeTurn: options.beforeTurn } : {}),
  });
  const ended = vi.fn();
  store.onTurnEnd(ended);
  return { store, api, started, streams, onAgentWrite, ended, chat: () => store.get(PROJECT) };
}

describe("loading a project's chat", () => {
  it("reads the history into the chat and is ready, with no turn running", async () => {
    const { store, chat } = setup({
      history: [
        { role: "user", content: "Staff submit expenses" },
        { role: "assistant", content: [{ type: "text", text: "Five features." }] },
      ],
    });
    await store.open(PROJECT);
    expect(chat().status).toBe("ready");
    expect(chat().turn).toEqual({ phase: "idle" });
    expect(chat().items.map((i) => i.kind)).toEqual(["user", "agent"]);
  });

  it("says why when the conversation cannot be read", async () => {
    const { store, chat } = setup({ api: { history: async () => Promise.reject(new Error("Couldn't load the conversation")) } });
    await store.open(PROJECT);
    expect(chat()).toMatchObject({ status: "error", error: "Couldn't load the conversation" });
  });
});

describe("a turn's lifecycle", () => {
  it("shows the message, runs the turn, folds its stream and ends idle", async () => {
    const { store, streams, chat, ended } = setup();
    await store.open(PROJECT);
    const stream = controlledStream();
    streams.set("t1", stream.body);

    const sent = store.send(PROJECT, "  What is left to do?  ", PRODUCT);
    expect(chat().turn).toEqual({ phase: "starting", instruction: "What is left to do?" });
    expect(chat().items[0]).toMatchObject({ kind: "user", text: "What is left to do?", state: "sending" });
    expect(await sent).toBe(true);
    expect(chat().items[0]).toMatchObject({ state: "sent", turnId: "t1" });
    await vi.waitFor(() => expect(chat().turn).toEqual({ phase: "running", turnId: "t1", instruction: "What is left to do?" }));

    stream.send({ type: "text-delta", delta: "Two features " });
    stream.send({ type: "text-delta", delta: "are left." });
    await vi.waitFor(() => expect(chat().items[1]).toMatchObject({ kind: "agent", text: "Two features are left." }));
    stream.send({ type: "turn-committed" });
    stream.end();

    await vi.waitFor(() => expect(chat().turn).toEqual({ phase: "idle" }));
    expect(ended).toHaveBeenCalledWith(PROJECT, "completed");
  });

  it("keeps a prototype review's batch on its row, and sends it typed", async () => {
    const { store, streams, chat, started } = setup();
    await store.open(PROJECT);
    streams.set("t1", controlledStream().body);
    const feedback = { prototypeHash: "a".repeat(64), component: "expense-web", requests: [{ screenId: "s", roleId: "r", stateId: "d", elementIds: [], text: "Wider" }] };
    await store.send(PROJECT, "/prototype expense-web", { kind: "prototype", feedback });
    expect(chat().items[0]).toMatchObject({ kind: "user", text: "/prototype expense-web", prototypeFeedback: feedback });
    expect(started[0]).toMatchObject({ instruction: "/prototype expense-web", collab: true, prototypeFeedback: feedback });
  });

  it("says why a turn failed, and ends it failed", async () => {
    const { store, streams, chat, ended } = setup();
    await store.open(PROJECT);
    streams.set("t1", sse([{ type: "turn-failed", message: "The model refused." } as StreamPart]));
    await store.send(PROJECT, "Go", PRODUCT);
    await vi.waitFor(() => expect(ended).toHaveBeenCalledWith(PROJECT, "failed"));
    expect(chat().items.at(-1)).toMatchObject({ kind: "error", text: "The model refused." });
  });
});

describe("a line posted from outside the chat", () => {
  it("reads as the agent's, and survives the next turn's replay", async () => {
    const { store, chat } = setup();
    await store.open(PROJECT);
    store.post(PROJECT, "v1 is building: Submit expenses, Approvals. Watch it here.");
    expect(chat().items).toEqual([
      { kind: "note", id: expect.any(String), text: "v1 is building: Submit expenses, Approvals. Watch it here." },
    ]);
    expect(await store.send(PROJECT, "What is left?", PRODUCT)).toBe(true);
    await vi.waitFor(() => expect(chat().turn).toEqual({ phase: "idle" }));
    expect(chat().items.map((i) => i.kind)).toEqual(["note", "user"]);
  });

  it("carries the next steps it offers", async () => {
    const { store, chat } = setup();
    await store.open(PROJECT);
    store.post(PROJECT, "v1 is building. Watch it here.", [{ kind: "open-build", label: "Open v1", version: "v1" }]);
    expect(chat().items.at(-1)).toMatchObject({ actions: [{ kind: "open-build", version: "v1" }] });
  });
});

describe("one turn at a time", () => {
  it("refuses a second message while a turn runs, and takes one once it ends", async () => {
    const { store, streams, api, chat } = setup();
    await store.open(PROJECT);
    const stream = controlledStream();
    streams.set("t1", stream.body);
    await store.send(PROJECT, "First", PRODUCT);

    expect(await store.send(PROJECT, "Second", PRODUCT)).toBe(false);
    expect(api.startTurn).toHaveBeenCalledTimes(1);
    expect(chat().items.filter((i) => i.kind === "user")).toHaveLength(1);

    stream.send({ type: "turn-committed" });
    stream.end();
    await vi.waitFor(() => expect(chat().turn).toEqual({ phase: "idle" }));
    expect(await store.send(PROJECT, "Second", PRODUCT)).toBe(true);
  });

  it("when the server says a turn is already running, marks the message not sent and shows that turn", async () => {
    const { store, api, chat } = setup({
      api: { startTurn: async () => Promise.reject(new TurnInProgressError("t9")) },
    });
    await store.open(PROJECT);
    expect(await store.send(PROJECT, "Hello?", PRODUCT)).toBe(false);
    expect(chat().items[0]).toMatchObject({ kind: "user", state: "failed" });
    expect(chat().items[1]).toMatchObject({ kind: "error" });
    await vi.waitFor(() => expect(api.openStream).toHaveBeenCalledWith(PROJECT, "t9", 0, expect.anything()));
  });
});

describe("a thread that was replaced", () => {
  it("when the server says the thread was replaced, follows the new one and sends the message there", async () => {
    let current = "conv-1";
    const { store, api, chat } = setup({
      api: {
        conversationId: vi.fn(async () => current),
        startTurn: vi.fn(async (_p: string, conversationId: string) => {
          if (conversationId !== current) throw new ConversationRotatedError();
          return "t1";
        }),
      },
    });
    await store.open(PROJECT);
    current = "conv-2";

    expect(await store.send(PROJECT, "/prototype expense-web", PRODUCT)).toBe(true);
    expect(api.startTurn).toHaveBeenLastCalledWith(PROJECT, "conv-2", expect.anything());
    expect(api.history).toHaveBeenLastCalledWith(PROJECT, "conv-2");
    expect(chat().items[0]).toMatchObject({ kind: "user", text: "/prototype expense-web", state: "sent", turnId: "t1" });
  });

  it("follows a thread a teammate started, and a send meanwhile goes to it instead of being dropped", async () => {
    vi.useFakeTimers();
    try {
      // A teammate started a new thread, and a turn is running in it.
      let active: TurnStatus | null = null;
      let current = "conv-1";
      const { store, api, chat } = setup({
        api: { activeTurn: vi.fn(async () => active), conversationId: vi.fn(async () => current) },
      });
      const stop = store.watch(PROJECT);
      await vi.waitFor(() => expect(chat().status).toBe("ready"));
      current = "conv-2";
      active = { ...running("t5"), conversationId: "conv-2" };
      await vi.advanceTimersByTimeAsync(60_000);
      active = null;
      await vi.advanceTimersByTimeAsync(60_000);
      expect(api.history).toHaveBeenLastCalledWith(PROJECT, "conv-2");

      expect(await store.send(PROJECT, "Hello", PRODUCT)).toBe(true);
      expect(api.startTurn).toHaveBeenCalledWith(PROJECT, "conv-2", expect.anything());
      stop();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("the scope on every turn", () => {
  it("scopes a feature's turn to its ID, and the whole product to no scope", async () => {
    const { store, started, chat } = setup();
    await store.open(PROJECT);
    await store.send(PROJECT, "Add a yearly view", F4);
    await vi.waitFor(() => expect(chat().turn).toEqual({ phase: "idle" }));
    await store.send(PROJECT, "Where are we?", PRODUCT);
    await vi.waitFor(() => expect(chat().turn).toEqual({ phase: "idle" }));
    await store.send(PROJECT, "Tighten the layout", { kind: "design" });

    expect(started[0]).toEqual({
      instruction: "Add a yearly view",
      collab: true,
      scope: { kind: "feature", feature: "F4" },
    });
    expect(started[1]).toEqual({ instruction: "Where are we?", collab: true });
    expect(started[2]).toEqual({ instruction: "Tighten the layout", collab: true, scope: { kind: "design-review" } });
  });
});

describe("reattaching after a reload", () => {
  it("finds the running turn, shows the message that started it, and folds it from the start", async () => {
    const { store, streams, chat, ended } = setup({
      history: [{ role: "user", content: "Earlier" }],
      active: running("t7", "Interview Spending reports."),
    });
    streams.set("t7", sse([{ type: "text-delta", delta: "Two questions." }, { type: "turn-committed" }]));
    await store.open(PROJECT);

    await vi.waitFor(() => expect(ended).toHaveBeenCalledWith(PROJECT, "completed"));
    expect(chat().items).toMatchObject([
      { kind: "user", text: "Earlier" },
      { kind: "user", text: "Interview Spending reports.", turnId: "t7" },
      { kind: "agent", text: "Two questions." },
    ]);
  });

  it("applies each of the agent's file writes once, though a reattach replays them", async () => {
    const write: StreamPart = {
      type: "tool-result",
      toolName: "editFile",
      toolCallId: "w1",
      input: { path: "specs/requirements/features/F4-spending-reports.md", oldString: "a", newString: "b" },
      output: { ok: true, op: "edit", path: "specs/requirements/features/F4-spending-reports.md" },
    };
    // The first attach's stream is cut before the turn's end, which is still running.
    const { store, api, onAgentWrite, chat } = setup({
      active: running("t7"),
      api: { startTurn: async () => Promise.reject(new TurnInProgressError("t7")) },
    });
    vi.mocked(api.openStream).mockImplementation(async () => sse([write]));
    await store.open(PROJECT);
    await vi.waitFor(() => expect(chat().turn).toEqual({ phase: "idle" }));

    // A send meets the same turn, which is attached again and replayed from its start.
    vi.mocked(api.openStream).mockImplementation(async () => sse([write, { type: "turn-committed" }]));
    await store.send(PROJECT, "Hello?", F4);
    await vi.waitFor(() => expect(api.openStream).toHaveBeenCalledTimes(2));
    await vi.waitFor(() => expect(chat().turn).toEqual({ phase: "idle" }));

    expect(onAgentWrite).toHaveBeenCalledTimes(1);
    expect(chat().items.filter((i) => i.kind === "activity")).toHaveLength(1);
  });
});

describe("answering a question card", () => {
  const ask: StreamPart = {
    type: "tool-call",
    toolCallId: "q1",
    toolName: "ask_question",
    input: { question: "Who reads the reports?", options: [{ label: "Finance only" }, { label: "Everyone" }] },
  };

  async function asked(scope: TurnScope = F4) {
    const t = setup();
    await t.store.open(PROJECT);
    t.streams.set("t1", sse([ask, { type: "turn-committed" }]));
    await t.store.send(PROJECT, "Interview Spending reports.", scope);
    await vi.waitFor(() => expect(t.chat().turn).toEqual({ phase: "idle" }));
    const card = t.chat().items.find((i) => i.kind === "question")!;
    return { ...t, card };
  }

  it("sends the answer as the next turn, in the asking turn's scope, and keeps it on the item", async () => {
    const { store, started, chat, card } = await asked();
    expect(await store.answer(PROJECT, card.id, [{ selected: ["Finance only"] }])).toBe(true);
    expect(started[1]).toMatchObject({
      instruction: 'Answer to "Who reads the reports?": Finance only',
      scope: { kind: "feature", feature: "F4" },
    });
    expect(chat().items.find((i) => i.id === card.id)).toMatchObject({ answers: [{ selected: ["Finance only"] }] });
  });

  it("answers a question the whole product's turn asked with no scope", async () => {
    const { store, started, card } = await asked(PRODUCT);
    await store.answer(PROJECT, card.id, [{ selected: ["Everyone"] }]);
    expect(started[1]).not.toHaveProperty("scope");
  });

  it("carries a free answer typed into the card", async () => {
    const { store, started, card } = await asked();
    await store.answer(PROJECT, card.id, [{ selected: [], freeText: "Finance and the board" }]);
    expect(started[1]?.instruction).toBe('Answer to "Who reads the reports?": Finance and the board');
  });

  it("leaves the card answerable when the answer could not be sent", async () => {
    const { store, api, chat, card } = await asked();
    vi.mocked(api.startTurn).mockRejectedValueOnce(new Error("Network down"));
    expect(await store.answer(PROJECT, card.id, [{ selected: ["Everyone"] }])).toBe(false);
    expect(chat().items.find((i) => i.id === card.id)).not.toHaveProperty("answers");
  });

  it("takes no answer for a card a later message already answered", async () => {
    const { store, api, chat, card } = await asked();
    await store.send(PROJECT, "Finance only, please", F4);
    await vi.waitFor(() => expect(chat().turn).toEqual({ phase: "idle" }));
    expect(await store.answer(PROJECT, card.id, [{ selected: ["Everyone"] }])).toBe(false);
    expect(api.startTurn).toHaveBeenCalledTimes(2);
  });
});

describe("announcing the questions a turn asks (the Questions card opens on them)", () => {
  const ask: StreamPart = {
    type: "tool-call",
    toolCallId: "q1",
    toolName: "ask_question",
    input: { question: "Who reads the reports?", options: [{ label: "Finance only" }, { label: "Everyone" }] },
  };

  it("announces a question a turn sent from here asked, once", async () => {
    const t = setup();
    const asked = vi.fn();
    t.store.onQuestionsAsked(asked);
    await t.store.open(PROJECT);
    t.streams.set("t1", sse([ask, ask, { type: "turn-committed" }]));
    await t.store.send(PROJECT, "Interview Spending reports.", F4);
    await vi.waitFor(() => expect(t.chat().turn).toEqual({ phase: "idle" }));
    expect(asked).toHaveBeenCalledTimes(1);
    expect(asked).toHaveBeenCalledWith(PROJECT, "t1:q:q1");
  });

  it("announces nothing for a turn found running that this browser did not start", async () => {
    const t = setup({ active: running("t7", "Interview Spending reports.") });
    const asked = vi.fn();
    t.store.onQuestionsAsked(asked);
    t.streams.set("t7", sse([ask, { type: "turn-committed" }]));
    await t.store.open(PROJECT);
    await vi.waitFor(() => expect(t.ended).toHaveBeenCalled());
    expect(t.chat().items.some((i) => i.kind === "question")).toBe(true);
    expect(asked).not.toHaveBeenCalled();
  });

  it("announces the kickoff's questions once this browser claimed it (it created the project)", async () => {
    const t = setup({ active: running("t7", "/start") });
    const asked = vi.fn();
    t.store.onQuestionsAsked(asked);
    t.store.claimKickoff(PROJECT);
    t.streams.set("t7", sse([ask, { type: "turn-committed" }]));
    await t.store.open(PROJECT);
    await vi.waitFor(() => expect(t.ended).toHaveBeenCalled());
    expect(asked).toHaveBeenCalledWith(PROJECT, "t7:q:q1");
  });
});

describe("the held kickoff", () => {
  it("is sent once the conversation turns out empty", async () => {
    const { store, started } = setup();
    store.seed(PROJECT, "/start");
    await vi.waitFor(() => expect(started).toEqual([{ instruction: "/start", collab: true }]));
  });

  it("is dropped when the conversation already started", async () => {
    const { store, api } = setup({ history: [{ role: "user", content: "/start An idea" }] });
    store.seed(PROJECT, "/start");
    await store.open(PROJECT);
    await Promise.resolve();
    expect(api.startTurn).not.toHaveBeenCalled();
  });

  it("waits for a kickoff that is running, then is dropped", async () => {
    const { store, api, ended } = setup({ active: running("t5", "/start An idea") });
    store.seed(PROJECT, "/start");
    await vi.waitFor(() => expect(ended).toHaveBeenCalled());
    expect(api.startTurn).not.toHaveBeenCalled();
  });
});

// The room is committed before a turn starts, so the commit the turn records
// as its base holds what its agent reads (seen on the live walk: a design
// recorded the stubs it never read, and every feature read out of date).
describe("before a turn", () => {
  it("commits the room first, and a failure to commit does not stop the turn", async () => {
    const order: string[] = [];
    const { store, api } = setup({
      beforeTurn: async () => {
        order.push("flush");
        throw new Error("room offline");
      },
      api: {
        startTurn: vi.fn(async () => {
          order.push("start");
          return "t1";
        }),
      },
    });
    await store.open(PROJECT);
    expect(await store.send(PROJECT, "Design F1.", PRODUCT)).toBe(true);
    expect(order).toEqual(["flush", "start"]);
    expect(api.startTurn).toHaveBeenCalledOnce();
  });
});

