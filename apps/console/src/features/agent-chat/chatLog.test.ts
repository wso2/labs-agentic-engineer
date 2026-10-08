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

import { describe, expect, it } from "vitest";
import {
  answerableQuestionId,
  appendAgentText,
  askedScope,
  dropTurnOutput,
  historyItems,
  interviewWriteUp,
  openQuestionId,
  setAnswers,
  userLineText,
  type ChatItem,
} from "./chatLog";

const F4 = "specs/requirements/features/F4-spending-reports.md";
const Q = { question: "Who reads the reports?", options: [{ label: "Finance only" }] };

describe("historyItems", () => {
  it("keeps user and agent prose in order, naming the user when recorded", () => {
    expect(
      historyItems([
        { role: "user", content: "Staff submit expenses", author: { id: "u1", displayName: "Mark" } },
        { role: "assistant", content: [{ type: "text", text: "Who approves them?" }] },
      ]),
    ).toEqual([
      { kind: "user", id: "h0", text: "Staff submit expenses", state: "sent", author: "Mark", scope: { kind: "product" } },
      { kind: "agent", id: "h1", turnId: "history", text: "Who approves them?" },
    ]);
  });

  it("keeps the prototype review a message sent, so a reload reads it as its requests", () => {
    const prototypeFeedback = {
      prototypeHash: "a".repeat(64),
      component: "expense-web",
      requests: [{ screenId: "screen.pending", roleId: "manager", stateId: "state.default", elementIds: [], text: "Wider" }],
    };
    expect(historyItems([{ role: "user", content: "/prototype expense-web", prototypeFeedback }])).toEqual([
      { kind: "user", id: "h0", text: "/prototype expense-web", state: "sent", prototypeFeedback, scope: { kind: "product" } },
    ]);
  });

  it("drops tool results and messages with no prose, numbering only what is shown", () => {
    expect(
      historyItems([
        { role: "user", content: "   " },
        { role: "tool", content: [{ type: "tool-result", output: "ok" }] },
        { role: "user", content: "Go on" },
      ]),
    ).toEqual([{ kind: "user", id: "h0", text: "Go on", state: "sent", scope: { kind: "product" } }]);
  });

  it("shows the agent's prose, its file writes and its questions in the order it made them", () => {
    const items = historyItems([
      {
        role: "assistant",
        content: [
          { type: "text", text: "Writing it up." },
          { type: "tool-call", toolCallId: "w1", toolName: "editFile", input: { path: F4 } },
          { type: "text", text: "Done. One more thing:" },
          { type: "tool-call", toolCallId: "q1", toolName: "ask_question", input: Q },
        ],
      },
    ]);
    expect(items).toEqual([
      { kind: "agent", id: "h0", turnId: "history", text: "Writing it up." },
      { kind: "activity", id: "h1", turnId: "history", toolCallId: "w1", op: "edit", path: F4, state: "done" },
      { kind: "agent", id: "h2", turnId: "history", text: "Done. One more thing:" },
      { kind: "question", id: "h3", turnId: "history", toolCallId: "q1", questions: [Q], streaming: false },
    ]);
  });

  it("drops a question the SDK rejected and a write the bundle refused", () => {
    const items = historyItems([
      {
        role: "assistant",
        content: [
          { type: "tool-call", toolCallId: "q-bad", toolName: "ask_question", input: Q },
          { type: "tool-call", toolCallId: "w-bad", toolName: "addFile", input: { path: "specs/x.md" } },
        ],
      },
      {
        role: "tool",
        content: [
          { type: "tool-result", toolCallId: "q-bad", output: { type: "error-text", value: "invalid input" } },
          { type: "tool-result", toolCallId: "w-bad", output: { type: "json", value: { ok: false } } },
        ],
      },
    ]);
    expect(items).toEqual([]);
  });
});

describe("folding a turn's stream", () => {
  it("grows the turn's text row, and starts a new one after anything else", () => {
    let items: ChatItem[] = [];
    items = appendAgentText(items, "t1", "Two ");
    items = appendAgentText(items, "t1", "questions.");
    items = [...items, { kind: "error", id: "e1", text: "x" }];
    items = appendAgentText(items, "t1", "Next.");
    expect(items.filter((i) => i.kind === "agent").map((i) => (i.kind === "agent" ? i.text : ""))).toEqual([
      "Two questions.",
      "Next.",
    ]);
  });

  it("clears a turn's output before a replay, keeping the message that started it", () => {
    const items: ChatItem[] = [
      { kind: "user", id: "u1", text: "Go", state: "sent", turnId: "t1" },
      { kind: "agent", id: "t1:text:0", turnId: "t1", text: "Half" },
      { kind: "agent", id: "t0:text:0", turnId: "t0", text: "Earlier" },
    ];
    expect(dropTurnOutput(items, "t1").map((i) => i.id)).toEqual(["u1", "t0:text:0"]);
  });
});

describe("answerableQuestionId", () => {
  const card = (id: string, extra: Partial<Extract<ChatItem, { kind: "question" }>> = {}): ChatItem => ({
    kind: "question",
    id,
    turnId: "t1",
    toolCallId: id,
    questions: [Q],
    streaming: false,
    ...extra,
  });
  const user = (state: "sent" | "failed"): ChatItem => ({ kind: "user", id: `u-${state}`, text: "x", state });

  it("is the newest card, while nothing was said after it", () => {
    expect(answerableQuestionId([card("a"), card("b")])).toBe("b");
  });

  it("is none once the card is answered, or while it still streams", () => {
    expect(answerableQuestionId(setAnswers([card("a")], "a", [{ selected: ["Finance only"] }]))).toBeNull();
    expect(answerableQuestionId([card("a", { streaming: true })])).toBeNull();
  });

  it("is none once a later message reached the agent, but a failed send answers nothing", () => {
    expect(answerableQuestionId([card("a"), user("sent")])).toBeNull();
    expect(answerableQuestionId([card("a"), user("failed")])).toBe("a");
  });

  it("clears a card's answers again, so it can be answered", () => {
    const answered = setAnswers([card("a")], "a", [{ selected: ["Finance only"] }]);
    expect(answerableQuestionId(setAnswers(answered, "a", null))).toBe("a");
  });
});

describe("a question's place in the log (the Questions card)", () => {
  const card = (id: string, extra: Partial<Extract<ChatItem, { kind: "question" }>> = {}): ChatItem => ({
    kind: "question",
    id,
    turnId: "t1",
    toolCallId: id,
    questions: [Q],
    streaming: false,
    ...extra,
  });
  const said = (id: string, scope?: Extract<ChatItem, { kind: "user" }>["scope"], state: "sent" | "failed" = "sent"): ChatItem => ({
    kind: "user",
    id,
    text: "x",
    state,
    ...(scope ? { scope } : {}),
  });

  it("is open while it streams, so it can be answered as it lands, and until answered or superseded", () => {
    expect(openQuestionId([card("a", { streaming: true })])).toBe("a");
    expect(openQuestionId([card("a")])).toBe("a");
    expect(openQuestionId(setAnswers([card("a")], "a", [{ selected: ["Finance only"] }]))).toBeNull();
    expect(openQuestionId([card("a"), said("u")])).toBeNull();
    expect(openQuestionId([card("a"), said("u", undefined, "failed")])).toBe("a");
  });

  it("is answered in the scope of the message that started the turn that asked it", () => {
    const f4 = { kind: "feature" as const, featureId: "F4" };
    expect(askedScope([said("u1"), said("u2", f4), card("a")], "a")).toEqual(f4);
    expect(askedScope([said("u1", f4), said("u2", undefined, "failed"), card("a")], "a")).toEqual(f4);
  });

  it("is about the whole product when nothing sent from here started its turn (the kickoff)", () => {
    expect(askedScope([card("a")], "a")).toEqual({ kind: "product" });
    expect(askedScope([said("u1"), card("a")], "a")).toEqual({ kind: "product" });
  });

  it("reads a message's scope back from the history", () => {
    expect(historyItems([{ role: "user", content: "Interview F4", scope: { kind: "feature", feature: "F4" } }])).toEqual([
      { kind: "user", id: "h0", text: "Interview F4", state: "sent", scope: { kind: "feature", featureId: "F4" } },
    ]);
  });
});

describe("interviewWriteUp", () => {
  const paths = new Set([F4]);
  const wrote = (path: string, state: "writing" | "done" = "done", turnId = "t2"): ChatItem => ({
    kind: "activity",
    id: `w-${turnId}-${path}-${state}`,
    turnId,
    toolCallId: "w1",
    op: "edit",
    path,
    state,
  });
  const asked: ChatItem = { kind: "question", id: "q1", turnId: "t1", toolCallId: "q1", questions: [Q], streaming: false };
  const answer: ChatItem = { kind: "user", id: "u2", text: 'Answer to "Who reads the reports?": Finance only', state: "sent" };
  const text: ChatItem = { kind: "agent", id: "a2", turnId: "t2", text: "Written." };
  const interview = (...after: ChatItem[]): ChatItem[] => [
    { kind: "user", id: "u1", text: "Interview Spending reports.", state: "sent" },
    asked,
    answer,
    ...after,
  ];

  it("is the feature an interview's answer turn wrote, placed after its last row", () => {
    expect(interviewWriteUp(interview(wrote(F4), text), paths)).toEqual({ path: F4, afterId: "a2" });
  });

  it("counts a typed reply to the question as its answer", () => {
    const typed: ChatItem = { kind: "user", id: "u2", text: "Finance, monthly", state: "sent" };
    const items = interview(wrote(F4), text).map((i) => (i.id === "u2" ? typed : i));
    expect(interviewWriteUp(items, paths)).toEqual({ path: F4, afterId: "a2" });
  });

  it("is none once the user has said anything since", () => {
    expect(interviewWriteUp(interview(wrote(F4), text, { kind: "user", id: "u3", text: "Next", state: "sent" }), paths)).toBeNull();
  });

  it("is none for a turn that wrote a feature without answering a question", () => {
    // Address comments edits F2 after a design turn: the feature was written,
    // but no interview wrote it.
    const addressed: ChatItem[] = [
      ...interview(wrote(F4), text),
      { kind: "user", id: "u3", text: "Address comments · 1", state: "sent" },
      { kind: "agent", id: "a3", turnId: "t3", text: "Working through 1 comment." },
      wrote(F4, "done", "t3"),
      { kind: "agent", id: "a4", turnId: "t3", text: "Done." },
    ];
    expect(interviewWriteUp(addressed, paths)).toBeNull();
    expect(interviewWriteUp([{ kind: "user", id: "u", text: "Tighten F4", state: "sent" }, wrote(F4), text], paths)).toBeNull();
  });

  it("is none for a write still streaming, or a file that is not a feature", () => {
    expect(interviewWriteUp(interview(wrote(F4, "writing")), paths)).toBeNull();
    expect(interviewWriteUp(interview(wrote("specs/requirements/prd.md"), text), paths)).toBeNull();
  });
});

describe("userLineText", () => {
  it("reads a feature's interview command as asking for it", () => {
    expect(userLineText("/interview F4")).toBe("Interview F4.");
  });

  it("reads an answer as the answer", () => {
    expect(userLineText('Answer to "Who reads the reports?": Finance only')).toBe("Finance only");
  });

  it("reads the kickoff as the user's idea, or says what it does when bare", () => {
    expect(userLineText("/start A room booking app")).toBe("A room booking app");
    expect(userLineText("/start")).toBe("Start the project from the brief.");
  });

  it("reads a prototype command as asking for the prototype", () => {
    expect(userLineText("/prototype expense-web")).toBe("Prototype expense-web.");
    expect(userLineText("/prototype")).toBe("Make the prototypes.");
  });

  it("leaves anything else as typed", () => {
    expect(userLineText("/started a draft")).toBe("/started a draft");
    expect(userLineText("Where are we?")).toBe("Where are we?");
  });
});
