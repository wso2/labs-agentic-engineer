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
import { isAnswerable, parseAskQuestionInput, serializeAnswer } from "./questionCards";
import type { ChatMessage } from "./chatStore";

const INPUT = {
  question: "Which auth flow?",
  options: [
    { label: "OIDC", description: "Platform default", recommended: true },
    { label: "API keys" },
  ],
};

describe("parseAskQuestionInput", () => {
  it("accepts the wire object", () => {
    expect(parseAskQuestionInput(INPUT)).toEqual(INPUT);
  });

  it("accepts the provider's stringified JSON", () => {
    expect(parseAskQuestionInput(JSON.stringify(INPUT))).toEqual(INPUT);
  });

  it("keeps multiSelect only when explicitly true", () => {
    expect(parseAskQuestionInput({ ...INPUT, multiSelect: true })?.multiSelect).toBe(true);
    expect(parseAskQuestionInput({ ...INPUT, multiSelect: "yes" })?.multiSelect).toBeUndefined();
  });

  it.each([
    ["missing question", { options: INPUT.options }],
    ["empty options", { question: "q", options: [] }],
    ["option without label", { question: "q", options: [{ description: "x" }] }],
    ["malformed JSON string", "{nope"],
    ["null", null],
  ])("rejects %s", (_name, value) => {
    expect(parseAskQuestionInput(value)).toBeNull();
  });
});

describe("serializeAnswer", () => {
  it("serializes a single selection", () => {
    expect(serializeAnswer("Which auth flow?", ["OIDC"])).toBe('Answer to "Which auth flow?": OIDC');
  });

  it("combines multi-select labels and a free-text note", () => {
    expect(serializeAnswer("Which?", ["A", "B"], "prefer A if forced")).toBe(
      'Answer to "Which?": A, B — prefer A if forced',
    );
  });

  it("supports a free-text-only answer", () => {
    expect(serializeAnswer("Which?", [], "something else")).toBe('Answer to "Which?": something else');
  });
});

function question(id: string, answer?: { selected: string[] }): ChatMessage {
  return {
    id,
    role: "question",
    turnId: "t1",
    toolCallId: `tc-${id}`,
    question: "q?",
    options: [{ label: "a" }],
    ...(answer ? { answer } : {}),
  };
}

describe("isAnswerable", () => {
  const user: ChatMessage = { id: "u1", role: "user", content: "hi", status: "completed" };

  it("is answerable while unanswered and last", () => {
    const q = question("q1");
    expect(isAnswerable([user, q], q as Extract<ChatMessage, { role: "question" }>)).toBe(true);
  });

  it("is not answerable once the card recorded an answer", () => {
    const q = question("q1", { selected: ["a"] });
    expect(isAnswerable([q], q as Extract<ChatMessage, { role: "question" }>)).toBe(false);
  });

  it("is superseded by any later user message (typed replies count)", () => {
    const q = question("q1");
    const later: ChatMessage = { id: "u2", role: "user", content: "typed reply", status: "completed" };
    expect(isAnswerable([q, later], q as Extract<ChatMessage, { role: "question" }>)).toBe(false);
  });

  it("stays answerable across later assistant narration", () => {
    const q = question("q1");
    const narration: ChatMessage = { id: "a1", role: "assistant", turnId: "t1", content: "…" };
    expect(isAnswerable([q, narration], q as Extract<ChatMessage, { role: "question" }>)).toBe(true);
  });
});
