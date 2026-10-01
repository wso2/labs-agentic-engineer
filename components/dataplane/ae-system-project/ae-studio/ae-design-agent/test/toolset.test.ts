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
