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
import { buildAnswerInstruction } from "@aep/agent-stream";
import { answerableQuestionIds, parseAskQuestionInput } from "./questionCards";
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
    [
      "duplicate labels (ambiguous selection identity)",
      { question: "q", options: [{ label: "a" }, { label: "a", description: "other" }] },
    ],
    ["malformed JSON string", "{nope"],
    ["null", null],
  ])("rejects %s", (_name, value) => {
    expect(parseAskQuestionInput(value)).toBeNull();
  });
});

describe("buildAnswerInstruction (wire contract)", () => {
  it("serializes a single selection", () => {
    expect(buildAnswerInstruction("Which auth flow?", ["OIDC"])).toBe(
      'Answer to "Which auth flow?": OIDC',
    );
  });

  it("combines multi-select labels and a free-text note", () => {
    expect(buildAnswerInstruction("Which?", ["A", "B"], "prefer A if forced")).toBe(
      'Answer to "Which?": A, B — prefer A if forced',
    );
  });

  it("supports a free-text-only answer", () => {
    expect(buildAnswerInstruction("Which?", [], "something else")).toBe(
      'Answer to "Which?": something else',
    );
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

function user(id: string, status: "completed" | "failed" | "in_flight" = "completed"): ChatMessage {
  return { id, role: "user", content: "text", status };
}

describe("answerableQuestionIds", () => {
  it("keeps an unanswered trailing question answerable", () => {
    expect(answerableQuestionIds([user("u1"), question("q1")])).toEqual(new Set(["q1"]));
  });

  it("excludes a question the card already answered", () => {
    expect(answerableQuestionIds([question("q1", { selected: ["a"] })])).toEqual(new Set());
  });

  it("is superseded by any later delivered user message (typed replies count)", () => {
    expect(answerableQuestionIds([question("q1"), user("u2")])).toEqual(new Set());
  });

  it("is NOT superseded by a failed send — the agent never saw it", () => {
    expect(answerableQuestionIds([question("q1"), user("u2", "failed")])).toEqual(
      new Set(["q1"]),
    );
  });

  it("stays answerable across later assistant narration", () => {
    const narration: ChatMessage = { id: "a1", role: "assistant", turnId: "t1", content: "…" };
    expect(answerableQuestionIds([question("q1"), narration])).toEqual(new Set(["q1"]));
  });

  it("supersedes earlier questions but not later ones, in one pass", () => {
    const log = [question("q1"), user("u1"), question("q2")];
    expect(answerableQuestionIds(log)).toEqual(new Set(["q2"]));
  });
});
