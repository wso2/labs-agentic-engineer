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

/**
 * A confirmation binds to the card the user is answering: the one question
 * card the agent asked since the user's last message in the conversation's
 * stored history, as the console shows exactly one card as answerable (the
 * last one, and none once the user said anything after it). Where that is not
 * one readable single question, there is no card to bind to.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { ModelMessage } from "ai";
import type { AskQuestionInput } from "@aep/agent-stream";
import { answerableQuestion } from "../src/agents/confirmation.js";

const q = (question: string, description?: string): AskQuestionInput => ({
  question,
  options: [{ label: "Yes", ...(description === undefined ? {} : { description }) }, { label: "No" }],
});

type Call = { id: string; input: unknown; toolName?: string; error?: boolean };

/** One assistant step making `calls` in parallel, and their results. */
function step(...calls: Call[]): ModelMessage[] {
  return [
    {
      role: "assistant",
      content: calls.map(({ id, input, toolName = "ask_question" }) => ({ type: "tool-call", toolCallId: id, toolName, input })),
    },
    {
      role: "tool",
      content: calls.map(({ id, toolName = "ask_question", error = false }) => ({
        type: "tool-result",
        toolCallId: id,
        toolName,
        output: error ? { type: "error-text", value: "invalid input" } : { type: "json", value: { status: "awaiting_user_response" } },
      })),
    },
  ];
}

const asked = (id: string, input: unknown, toolName = "ask_question", error = false): ModelMessage[] =>
  step({ id, input, toolName, error });

const user = (text: string): ModelMessage => ({ role: "user", content: text });
const said = (text: string): ModelMessage => ({ role: "assistant", content: [{ type: "text", text }] });

test("answerableQuestion: the card the last turn asked", () => {
  const history = [user("hi"), ...asked("a", q("First?", "one")), user("Yes"), ...asked("b", q("Second?", "two"))];
  assert.deepEqual(answerableQuestion(history), q("Second?", "two"));
});

test("answerableQuestion: none asked, or only other tools → undefined", () => {
  assert.equal(answerableQuestion([]), undefined);
  assert.equal(answerableQuestion([user("Answer to \"Post this comment?\": Post it")]), undefined);
  assert.equal(answerableQuestion([user("hi"), ...asked("b", { body: "x" }, "comment_issue")]), undefined);
});

test("answerableQuestion: a card from before the user's last message is answered or superseded → undefined", () => {
  const history = [user("hi"), ...asked("a", q("Shown?", "shown")), user("wait, not yet"), said("Okay.")];
  assert.equal(answerableQuestion(history), undefined);
});

test("answerableQuestion: a call the SDK rejected showed no card; the accepted retry is the card", () => {
  const history = [user("hi"), ...asked("a", q("Rejected?", "never shown"), "ask_question", true), ...asked("b", q("Shown?", "shown"))];
  assert.deepEqual(answerableQuestion(history), q("Shown?", "shown"));
});

test("answerableQuestion: more than one card in the last turn is ambiguous → undefined", () => {
  const parallel = [user("hi"), ...step({ id: "a", input: q("Post this comment?", "one") }, { id: "b", input: q("Post this comment?", "two") })];
  assert.equal(answerableQuestion(parallel), undefined);
  const thenBatch = [user("hi"), ...asked("a", q("Post this comment?", "one")), ...asked("b", { questions: [q("Post this comment?", "two")] }, "ask_questions")];
  assert.equal(answerableQuestion(thenBatch), undefined);
});

test("answerableQuestion: a batch of one question is that card (the console answers it in the single form)", () => {
  assert.deepEqual(answerableQuestion([user("hi"), ...asked("a", { questions: [q("Post this comment?", "one")] }, "ask_questions")]), q("Post this comment?", "one"));
  assert.equal(
    answerableQuestion([user("hi"), ...asked("a", { questions: [q("Post this comment?", "one"), q("Other?")] }, "ask_questions")]),
    undefined,
  );
});

test("answerableQuestion: a last card not readable as a question is no card, never an earlier one", () => {
  for (const input of [{ question: "Bad?", options: [{ label: "Yes", description: 7 }] }, { question: 3, options: [] }, "{}", null]) {
    const history = [user("hi"), ...asked("a", q("Shown?", "shown")), ...asked("b", input)];
    assert.equal(answerableQuestion(history), undefined, JSON.stringify(input));
  }
});
