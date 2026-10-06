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
 * Toolset selection contract: the `files` tool set stays byte-identical to today
 * (same tool names, same prompt), and `task-plan` swaps the domain tools while
 * sharing the skill catalog — no file tools leak in.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { FileBundle } from "@aep/agent-stream";
import { buildFileToolSet, buildRegisterDraftTools } from "../src/agents/main/tools/files.js";
import { buildTaskPlanTools } from "../src/agents/main/tools/task-plan.js";
import { TaskPlan } from "../src/agents/main/task-plan-accumulator.js";
import { instructions, buildInstructions, taskPlanInstructions, buildTaskPlanInstructions } from "../src/agents/main/prompt.js";
import { buildIssuesTools } from "../src/agents/issues/tools.js";
import { FILE_IT, FILE_QUESTION } from "../src/agents/issues/filing-gate.js";
import { buildIssuesInstructions } from "../src/agents/issues/prompt.js";
import { testSkillSource } from "./skill-source.js";

const SKILLS = testSkillSource([{ name: "task-planning", description: "plan tasks", content: "one task per component" }]);
const bundle = () => new FileBundle({});
const plan = () => new TaskPlan({});

test("files tool set (no skills) is the file tools + the UI tools", () => {
  assert.deepEqual(Object.keys(buildFileToolSet(bundle()).tools), [
    "addFile",
    "editFile",
    "removeFile",
    "ask_question",
    "ask_questions",
    "declare_plan",
  ]);
});

test("draftExternalResource is not on the files set — only the register flow adds it", () => {
  assert.equal("draftExternalResource" in buildFileToolSet(bundle()).tools, false);
  assert.deepEqual(Object.keys(buildRegisterDraftTools()), ["draftExternalResource"]);
});

test("files tool set with skills adds only the skill loader", () => {
  assert.deepEqual(Object.keys(buildFileToolSet(bundle(), SKILLS).tools), [
    "addFile",
    "editFile",
    "removeFile",
    "ask_question",
    "ask_questions",
    "declare_plan",
    "loadSkill",
  ]);
});

test("task-plan tool set registers planTask+updateTask and NO file tools", () => {
  const keys = Object.keys(buildTaskPlanTools(plan()));
  assert.deepEqual(keys, ["planTask", "updateTask"]);
  for (const fileTool of ["addFile", "editFile", "removeFile"]) {
    assert.equal(keys.includes(fileTool), false, `${fileTool} must not be in the task-plan set`);
  }
});

test("task-plan tool set with skills shares the same skill loader", () => {
  assert.deepEqual(Object.keys(buildTaskPlanTools(plan(), SKILLS)), ["planTask", "updateTask", "loadSkill"]);
});

test("files instructions are unchanged; task-plan instructions are a distinct mission", () => {
  assert.equal(buildInstructions(), instructions); // today's prompt, byte-identical
  assert.ok(buildTaskPlanInstructions().startsWith(taskPlanInstructions));
  assert.match(taskPlanInstructions, /planTask/);
  // #373 layer charter: the skill POINTER rides the plan instruction (the BFF's
  // PlanInstruction), not this system prompt — the prompt fixes invariants only.
  assert.doesNotMatch(taskPlanInstructions, /task-planning skill/);
  assert.doesNotMatch(taskPlanInstructions, /editFile/); // the plan turn does not edit files
});

test("planTask carries the Task's feature through to the accumulator (B3)", async () => {
  const p = new TaskPlan({ "specs/design/components/api/design.json": '{"name":"api"}\n' });
  const planTask = buildTaskPlanTools(p).planTask!;
  const out = await planTask.execute!(
    { component: "api", title: "Approvals in the API", dependsOn: [], rationale: "r", feature: "F2" },
    {} as never,
  );
  assert.equal((out as { feature?: string }).feature, "F2");
  assert.equal(p.plannedTasks()[0]!.feature, "F2");
});

// --- issues tool set (the Issues chat) ---------------------------------------

test("issues tool set is the classifier + the question tools — no file tools, no loadSkill", () => {
  const tools = buildIssuesTools({ apiKey: undefined, url: "http://jev.invalid", fetch: globalThis.fetch });
  assert.deepEqual(Object.keys(tools).sort(), ["ask_question", "ask_questions", "classify_report"]);
});

test("issues instructions carry the procedure and keep the classifier unnamed to the user", () => {
  const out = buildIssuesInstructions(undefined, undefined);
  for (const needle of ["classify_report", "search_issues", "create_issue", "File it", "Change it"]) {
    assert.ok(out.includes(needle), `mentions ${needle}`);
  }
  assert.match(out, /never .*classifier/i);
  // The one allowed occurrence is the rule that forbids naming it.
  assert.equal(out.match(/Jev/g)?.length, 1);
  // Not the spec agent's prompt: no file-editing vocabulary.
  assert.equal(out.includes("addFile"), false);
});

test("issues instructions order the classifier outcomes: question, then ask the kind, and unknown means ask", () => {
  const out = buildIssuesInstructions(undefined, undefined);
  const question = out.indexOf('kind is "question"');
  const clarify = out.indexOf("needsClarification is true");
  assert.ok(question > 0 && clarify > question, "a question is handled before the clarification rule");
  assert.match(out, /"unknown"/);
  assert.match(out, /could not tell/);
});

test("issues instructions use the gate's question and option wording", () => {
  const out = buildIssuesInstructions(undefined, undefined);
  assert.ok(out.includes(`"${FILE_QUESTION}"`));
  assert.ok(out.includes(FILE_IT));
});

test("issues instructions append the surface's narration policy", () => {
  const skills = testSkillSource([{ name: "console", description: "how to speak", content: "Say issue, not ticket." }]);
  assert.match(buildIssuesInstructions(skills, "console"), /# Narration policy\n\nSay issue, not ticket\./);
  assert.equal(buildIssuesInstructions(skills, undefined), buildIssuesInstructions(undefined, undefined));
});
