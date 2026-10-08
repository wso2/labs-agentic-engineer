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
 * A confirmation binds to the card the user last saw: the last accepted
 * ask_question call in the conversation's stored history.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { ModelMessage } from "ai";
import type { AskQuestionInput } from "@aep/agent-stream";
import { lastAskedQuestion } from "../src/agents/confirmation.js";

const q = (question: string, description?: string): AskQuestionInput => ({
  question,
  options: [{ label: "Yes", ...(description === undefined ? {} : { description }) }, { label: "No" }],
});

/** One assistant step calling `toolName` with `input`, and its result. */
function asked(id: string, input: unknown, toolName = "ask_question", error = false): ModelMessage[] {
  return [
    { role: "assistant", content: [{ type: "tool-call", toolCallId: id, toolName, input }] },
    {
      role: "tool",
      content: [
        {
          type: "tool-result",
          toolCallId: id,
          toolName,
          output: error ? { type: "error-text", value: "invalid input" } : { type: "json", value: { status: "awaiting_user_response" } },
        },
      ],
    },
  ];
}

const user = (text: string): ModelMessage => ({ role: "user", content: text });

test("lastAskedQuestion: the last ask_question card in the history", () => {
  const history = [user("hi"), ...asked("a", q("First?", "one")), user("Yes"), ...asked("b", q("Second?", "two")), user("x")];
  assert.deepEqual(lastAskedQuestion(history), q("Second?", "two"));
});

test("lastAskedQuestion: none asked, or only batches and other tools → undefined", () => {
  assert.equal(lastAskedQuestion([]), undefined);
  assert.equal(lastAskedQuestion([user("Answer to \"Post this comment?\": Post it")]), undefined);
  const others = [...asked("a", { questions: [q("Batch?")] }, "ask_questions"), ...asked("b", { body: "x" }, "comment_issue")];
  assert.equal(lastAskedQuestion(others), undefined);
});

test("lastAskedQuestion: a call the SDK rejected showed no card; the one before it stands", () => {
  const history = [...asked("a", q("Shown?", "shown")), ...asked("b", q("Rejected?", "never shown"), "ask_question", true)];
  assert.deepEqual(lastAskedQuestion(history), q("Shown?", "shown"));
});

test("lastAskedQuestion: an input not shaped like a question is no card", () => {
  const history = [
    ...asked("a", q("Shown?", "shown")),
    ...asked("b", { question: "Bad?", options: [{ label: "Yes", description: 7 }] }),
    ...asked("c", { question: 3, options: [] }),
  ];
  assert.deepEqual(lastAskedQuestion(history), q("Shown?", "shown"));
});
