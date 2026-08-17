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

// What the agent is doing, off the chat log (#485). The count is of QUESTIONS,
// not of cards: one form routinely carries several, and "1 question" over a
// form of four is a lie the form itself immediately contradicts.

import { describe, expect, it } from "vitest";
import type { AskQuestionInput } from "@aep/agent-stream";
import type { ChatMessage } from "./chatStore";
import { interviewState } from "./interviewState";

const QUESTIONS: AskQuestionInput[] = [
  { question: "Who signs in?", options: [{ label: "Anyone" }] },
  { question: "How do they pay?", options: [{ label: "Card" }] },
  { question: "Who can refund?", options: [{ label: "Staff" }] },
];

function question(id: string, questions: AskQuestionInput[], answers?: unknown): ChatMessage {
  return {
    id,
    role: "question",
    turnId: "t1",
    toolCallId: id,
    questions,
    ...(answers ? { answers } : {}),
  } as ChatMessage;
}

function user(id: string, status: "in_flight" | "completed" | "failed"): ChatMessage {
  return { id, role: "user", content: "hi", turnId: "t1", status } as ChatMessage;
}

describe("interviewState", () => {
  it("is quiet on an empty log", () => {
    expect(interviewState([])).toEqual({ turnRunning: false, pendingQuestions: 0 });
  });

  it("counts every question on a pending form, not the form", () => {
    expect(interviewState([question("q1", QUESTIONS)]).pendingQuestions).toBe(3);
  });

  it("sums across several pending forms", () => {
    const log = [question("q1", QUESTIONS.slice(0, 2)), question("q2", QUESTIONS.slice(2))];
    expect(interviewState(log).pendingQuestions).toBe(3);
  });

  // A later delivered user message is the answer (or the finish valve); the
  // agent is no longer waiting on the form it superseded.
  it("stops counting a form a later message superseded", () => {
    const log = [question("q1", QUESTIONS), user("u1", "completed")];
    expect(interviewState(log).pendingQuestions).toBe(0);
  });

  it("keeps counting when the message that would supersede it failed to send", () => {
    const log = [question("q1", QUESTIONS), user("u1", "failed")];
    expect(interviewState(log).pendingQuestions).toBe(3);
  });

  it("reports a turn in flight", () => {
    expect(interviewState([user("u1", "in_flight")]).turnRunning).toBe(true);
    expect(interviewState([user("u1", "completed")]).turnRunning).toBe(false);
  });
});
