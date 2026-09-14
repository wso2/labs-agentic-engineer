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
import { projectableHistory } from "./history";
import type { ConversationMessage } from "./api/turns";

describe("projectableHistory", () => {
  it("carries author onto rehydrated user messages when present", () => {
    const history: ConversationMessage[] = [
      { role: "user", content: "hi", author: { id: "u-me", displayName: "Developer" } },
      { role: "assistant", content: "hello" },
    ];
    expect(projectableHistory(history)).toEqual([
      {
        id: "h0",
        role: "user",
        content: "hi",
        status: "completed",
        author: { id: "u-me", displayName: "Developer" },
      },
      { id: "h1", role: "assistant", turnId: "history", content: "hello" },
    ]);
  });

  it("omits author when the message has none (backward compatible)", () => {
    const [msg] = projectableHistory([{ role: "user", content: "hi" }]);
    expect(msg).toEqual({ id: "h0", role: "user", content: "hi", status: "completed" });
    expect((msg as { author?: unknown } | undefined)?.author).toBeUndefined();
  });

  it("puts a teammate-initiated turn's triggering message before its reply, attributed correctly", () => {
    const history: ConversationMessage[] = [
      { role: "user", content: "scope checkout", author: { id: "u-me", displayName: "Developer" } },
      { role: "assistant", content: "on it" },
      { role: "user", content: "add returns", author: { id: "u-sarah", displayName: "Sarah Perera" } },
    ];
    const out = projectableHistory(history);
    expect(out.map((m) => m.role)).toEqual(["user", "assistant", "user"]);
    expect(out[2]).toMatchObject({ author: { id: "u-sarah", displayName: "Sarah Perera" } });
  });

  it("drops parts with no extractable text and ignores other roles", () => {
    const history: ConversationMessage[] = [
      { role: "system", content: "you are an agent" },
      { role: "user", content: [{ type: "tool-result" }] },
      { role: "assistant", content: [{ type: "text", text: "hi " }, { type: "text", text: "there" }] },
    ];
    expect(projectableHistory(history)).toEqual([
      { id: "h0", role: "assistant", turnId: "history", content: "hi there" },
    ]);
  });

  it("reconstructs a question card from an ask_question tool-call (ADR-0012)", () => {
    const q = { question: "Who is the user?", options: [{ label: "A" }, { label: "B" }] };
    const history: ConversationMessage[] = [
      { role: "user", content: "grill me" },
      {
        role: "assistant",
        content: [
          { type: "text", text: "one question:" },
          { type: "tool-call", toolName: "ask_question", toolCallId: "tc-9", input: q },
        ],
      },
    ];
    const out = projectableHistory(history);
    expect(out.map((m) => m.role)).toEqual(["user", "assistant", "question"]);
    expect(out[2]).toMatchObject({ role: "question", toolCallId: "tc-9", questions: [q] });
  });

  it("skips a question call the schema rejected, keeping the retry that resolved", () => {
    const bad = { question: "Which provider?", options: [{ label: "A" }, { label: "", freeText: true }] };
    const good = { question: "Which provider?", options: [{ label: "A" }] };
    const history: ConversationMessage[] = [
      { role: "user", content: "resolve it" },
      { role: "assistant", content: [{ type: "tool-call", toolName: "ask_question", toolCallId: "tc-bad", input: bad }] },
      {
        role: "tool",
        content: [
          { type: "tool-result", toolCallId: "tc-bad", toolName: "ask_question", output: { type: "error-text", value: "invalid" } },
        ],
      },
      { role: "assistant", content: [{ type: "tool-call", toolName: "ask_question", toolCallId: "tc-good", input: good }] },
      {
        role: "tool",
        content: [
          { type: "tool-result", toolCallId: "tc-good", toolName: "ask_question", output: { type: "json", value: {} } },
        ],
      },
    ];
    const out = projectableHistory(history);
    expect(out.map((m) => m.role)).toEqual(["user", "question"]);
    expect(out[1]).toMatchObject({ role: "question", toolCallId: "tc-good", questions: [good] });
  });

  it("carries attachment names onto a rehydrated user row (#428)", () => {
    // The point of putting names on the journal: without this, a reload shows
    // the agent discussing a document that appears nowhere in the thread.
    const history: ConversationMessage[] = [
      { role: "user", content: "what is wrong here?", attachments: ["error.png"] },
    ];
    expect(projectableHistory(history)).toEqual([
      {
        id: "h0",
        role: "user",
        content: "what is wrong here?",
        status: "completed",
        attachments: ["error.png"],
      },
    ]);
  });

  it("leaves a message without attachments in its pre-feature shape", () => {
    const history: ConversationMessage[] = [{ role: "user", content: "hi" }];
    expect(projectableHistory(history)).toEqual([
      { id: "h0", role: "user", content: "hi", status: "completed" },
    ]);
  });

  it("reconstructs a batch form from an ask_questions tool-call, even with no narration text", () => {
    const input = {
      questions: [
        { question: "Q1", options: [{ label: "A" }] },
        { question: "Q2", options: [{ label: "X" }] },
      ],
    };
    const history: ConversationMessage[] = [
      { role: "assistant", content: [{ type: "tool-call", toolName: "ask_questions", toolCallId: "tc-b", input }] },
    ];
    const out = projectableHistory(history);
    expect(out).toEqual([
      { id: "h0:q-tc-b", role: "question", turnId: "history", toolCallId: "tc-b", questions: input.questions },
    ]);
  });
});
