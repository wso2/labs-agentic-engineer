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
 * The issue agent's instructions: one issue, read first, every write drafted
 * and confirmed with the gate's own question and option.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { CONFIRMATIONS, NOT_NOW } from "../src/agents/issue/confirm-gate.js";
import { buildIssueInstructions } from "../src/agents/issue/prompt.js";
import { testSkillSource } from "./skill-source.js";

const flat = (s: string): string => s.replace(/\s+/g, " ");

test("issue instructions name the issue and keep the agent on it", () => {
  const out = flat(buildIssueInstructions(42));
  assert.match(out, /issue #42/);
  assert.equal(out.includes("#7"), false);
  assert.match(out, /only/);
  // Reads the issue before anything else.
  assert.match(out, /call get_issue/);
  // Not the spec agent's prompt, nor the Issues chat's.
  for (const absent of ["addFile", "create_issue", "search_issues", "classify_report"]) {
    assert.equal(out.includes(absent), false, absent);
  }
});

test("issue instructions carry the gate's confirmation table, one ask_question each, recommended as a flag", () => {
  const out = flat(buildIssueInstructions(42));
  for (const [tool, { question, option }] of Object.entries(CONFIRMATIONS)) {
    assert.ok(out.includes(tool), `names ${tool}`);
    assert.ok(out.includes(`"${question}"`), `asks "${question}"`);
    assert.ok(out.includes(`"${option}"`), `offers "${option}"`);
  }
  assert.ok(out.includes(`"${NOT_NOW}"`));
  assert.match(out, /recommended: true/);
  assert.equal(out.includes("(recommended)"), false, "never a label that carries the word");
  assert.match(out, /never ask_questions/);
  assert.match(out, /draft/i);
});

test("issue instructions pick the component before the hand-off and relay the deploy-first answer", () => {
  const out = flat(buildIssueInstructions(42));
  const list = out.indexOf("Call list_components");
  const confirm = out.indexOf(`"${CONFIRMATIONS.hand_to_coding_agent.question}"`, list);
  assert.ok(list > 0 && confirm > list, "the component question comes before the hand-off confirmation");
  assert.match(out, /deploy a version first/i);
});

test("issue instructions keep the classifier unnamed and speak plainly", () => {
  const out = buildIssueInstructions(42);
  // The one allowed occurrence is the rule that forbids naming it.
  assert.equal(out.match(/Jev/g)?.length, 1);
  assert.match(out, /never .*classifier/i);
  assert.match(out, /plain words/);
});

test("issue instructions append the surface's narration policy", () => {
  const skills = testSkillSource([{ name: "console", description: "how to speak", content: "Say issue, not ticket." }]);
  assert.match(buildIssueInstructions(42, skills, "console"), /# Narration policy\n\nSay issue, not ticket\./);
  assert.equal(buildIssueInstructions(42, skills, undefined), buildIssueInstructions(42));
});
