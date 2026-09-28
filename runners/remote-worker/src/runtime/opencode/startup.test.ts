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

import { test } from "node:test";
import assert from "node:assert/strict";
import { hiddenTools, startupProblems, visibleTools, type PermissionRuleRecord, type StartupFacts } from "./startup.js";

// What 1.18.32's `GET /experimental/tool/ids` and `/experimental/tool` report
// for anthropic/claude-haiku-4-5 (read from a live server, no model call):
// websearch is withheld on this provider and apply_patch is GPT-only.
const TOOL_IDS = ["invalid", "question", "bash", "read", "glob", "grep", "edit", "write", "task", "webfetch", "todowrite", "skill"];
const TASK_PARAMS = ["description", "prompt", "subagent_type", "task_id", "command"];

const r = (permission: string, pattern: string, action: string): PermissionRuleRecord => ({ permission, pattern, action });

// The head of the `aep` agent's merged rule set as `GET /agent` returned it for
// the config builder's output: OpenCode's defaults first, the config after.
const DEFAULTS = [r("*", "*", "allow"), r("doom_loop", "*", "ask"), r("question", "*", "deny"), r("read", "*.env", "ask")];
const CORRECT = [
  ...DEFAULTS,
  r("edit", "*", "allow"),
  r("task", "*", "deny"),
  r("task", "general", "allow"),
  r("skill", "*", "deny"),
  r("skill", "aep", "allow"),
  r("question", "*", "deny"),
  r("external_directory", "*", "allow"),
];

function facts(over: Partial<StartupFacts> = {}): StartupFacts {
  return {
    guardReady: true,
    systemTransformLive: true,
    toolIds: TOOL_IDS,
    agentRules: CORRECT,
    taskParameters: TASK_PARAMS,
    ...over,
  };
}

test("startupProblems: the config builder's rule set passes all four assertions", () => {
  assert.deepEqual(startupProblems(facts()), []);
  const visible = visibleTools(facts());
  assert.ok(visible.includes("task") && visible.includes("skill"));
  assert.ok(!visible.includes("question"));
});

// Spike S1, reproduced: the allowlist written allows-first hides the tool.
test("startupProblems: an allowlist whose `*: deny` is last hides the tool, and the run is refused", () => {
  const wrongOrder = [...DEFAULTS, r("task", "general", "allow"), r("task", "*", "deny"), r("skill", "aep", "allow"), r("skill", "*", "deny")];
  assert.deepEqual([...hiddenTools(TOOL_IDS, wrongOrder)].sort(), ["question", "skill", "task"]);
  const [problem] = startupProblems(facts({ agentRules: wrongOrder }));
  assert.match(problem, /not offered task, skill/);
});

test("startupProblems: a silent plugin, a visible question tool, a background schema — each refuses", () => {
  assert.match(startupProblems(facts({ guardReady: false }))[0], /aep-guard plugin did not announce itself/);
  const askable = CORRECT.filter((x) => x.permission !== "question");
  assert.match(startupProblems(facts({ agentRules: askable })).join(" "), /offered `question`/);
  assert.match(
    startupProblems(facts({ taskParameters: [...TASK_PARAMS, "background"] }))[0],
    /OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS is set/,
  );
  assert.match(startupProblems(facts({ agentRules: undefined }))[0], /no `aep` agent/);
});

test("startupProblems: a system transform that never fired refuses the run, unless the plugin itself is missing", () => {
  const [problem, ...rest] = startupProblems(facts({ systemTransformLive: false }));
  assert.match(problem, /experimental\.chat\.system\.transform hook did not fire/);
  assert.deepEqual(rest, []);
  // A plugin that never loaded cannot run the probe; that is one problem, said once.
  const silent = startupProblems(facts({ guardReady: false, systemTransformLive: false }));
  assert.equal(silent.length, 1);
  assert.match(silent[0], /did not announce itself/);
});

test("hiddenTools: the authoring tools share the edit permission", () => {
  assert.deepEqual([...hiddenTools(["edit", "write", "bash"], [r("*", "*", "allow"), r("edit", "*", "deny")])].sort(), ["edit", "write"]);
  // A pattern-scoped deny restricts calls; it does not hide the tool.
  assert.deepEqual([...hiddenTools(["bash"], [r("*", "*", "allow"), r("bash", "rm *", "deny")])], []);
});
