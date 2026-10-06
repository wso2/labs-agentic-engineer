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
 * The Issues agent files only on the user's own "File it" answer. The prompt
 * asks for it; this gate enforces it in code, because text the model reads
 * (an issue body from search_issues, something the user pasted) can try to talk
 * it into calling create_issue unconfirmed.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { tool, type ToolSet } from "ai";
import { z } from "zod";
import { buildAnswerInstruction, buildAnswersInstruction } from "@aep/agent-stream";
import { FILE_IT, FILE_QUESTION, filingConfirmed, gateCreateIssue } from "../src/agents/issues/filing-gate.js";

test("the question and option the gate waits for are the ones the prompt uses", () => {
  assert.equal(FILE_QUESTION, "File this issue?");
  assert.equal(FILE_IT, "File it");
});

test("filingConfirmed: the single and batch answer forms, with or without a note", () => {
  assert.equal(filingConfirmed(buildAnswerInstruction(FILE_QUESTION, [FILE_IT])), true);
  assert.equal(filingConfirmed(buildAnswerInstruction(FILE_QUESTION, [FILE_IT], "title: shorter")), true);
  assert.equal(filingConfirmed(buildAnswersInstruction([{ question: FILE_QUESTION, selected: [FILE_IT] }])), true);
  assert.equal(filingConfirmed(buildAnswersInstruction([{ question: FILE_QUESTION, selected: [FILE_IT], freeText: "thanks" }])), true);
  assert.equal(
    filingConfirmed(
      buildAnswersInstruction([
        { question: "Which kind?", selected: ["Bug"] },
        { question: FILE_QUESTION, selected: [FILE_IT] },
      ]),
    ),
    true,
  );
});

test("filingConfirmed: anything else is not a confirmation", () => {
  const no: Record<string, string> = {
    "change it": buildAnswerInstruction(FILE_QUESTION, ["Change it"]),
    "change it, batch": buildAnswersInstruction([{ question: FILE_QUESTION, selected: ["Change it"] }]),
    "plain chat that mentions it": "please File it now",
    "chat quoting the label": `The answer is: ${FILE_IT}`,
    "a different question": buildAnswerInstruction("Which kind?", [FILE_IT]),
    "a different question, batch": buildAnswersInstruction([{ question: "Which kind?", selected: [FILE_IT] }]),
    "a longer label": buildAnswerInstruction(FILE_QUESTION, ["File it later"]),
    "both labels": buildAnswerInstruction(FILE_QUESTION, [FILE_IT, "Change it"]),
    empty: "",
  };
  for (const [name, text] of Object.entries(no)) assert.equal(filingConfirmed(text), false, name);
});

const createIssue = tool({
  description: "file an issue",
  inputSchema: z.object({ title: z.string() }),
  execute: async () => "FILED",
});

test("gateCreateIssue: unconfirmed, create_issue refuses with a tool error that says what to do", async () => {
  const tools: ToolSet = { search_issues: tool({ inputSchema: z.object({}), execute: async () => "ok" }), create_issue: createIssue };
  const gated = gateCreateIssue(tools, false);
  assert.equal(gated.search_issues, tools.search_issues, "other tools are untouched");
  assert.equal(gated.create_issue!.description, "file an issue");
  assert.equal(gated.create_issue!.inputSchema, createIssue.inputSchema);
  await assert.rejects(
    () => gated.create_issue!.execute!({ title: "x" }, {} as never) as Promise<unknown>,
    /Not filed\. Ask the user "File this issue\?" with ask_question .*file only after they answer File it\./,
  );
});

test("gateCreateIssue: confirmed, or no create_issue, returns the tools unchanged", () => {
  const tools: ToolSet = { create_issue: createIssue };
  assert.equal(gateCreateIssue(tools, true), tools);
  const bare: ToolSet = {};
  assert.equal(gateCreateIssue(bare, false), bare);
});
