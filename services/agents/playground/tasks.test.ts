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
import { buildPlanInput } from "./tasks.js";
import type { Skill } from "../src/contracts/sse-events.js";

const DESIGN_JSON = JSON.stringify({
  name: "webapp",
  type: "web-application",
  version: "1",
  language: "typescript",
  buildpack: "nodejs",
  appPath: ".",
  entrypoint: "src/main.ts",
  exposure: "internet",
  dependencies: [],
  description: "the web app",
});

const files = { "specs/design/components/webapp/design.json": DESIGN_JSON };

function taskBreakdownSkill(content: string): Skill {
  return { name: "task-breakdown", description: "plans tasks", content };
}

test("buildPlanInput pushes the raw task-breakdown body (frontmatter included) when supplied", () => {
  const raw = "---\nname: task-breakdown\ndescription: plans tasks\n---\n# Task breakdown\n\nbody\n";
  const { input } = buildPlanInput(
    "proj",
    files,
    [taskBreakdownSkill("# Task breakdown\n\nbody")], // the (stripped) catalog content — must NOT be what's pushed
    raw,
  );
  assert.equal(input.taskBreakdownSkill?.body, raw);
  assert.match(input.taskBreakdownSkill!.body, /^---\n/); // frontmatter survives, matching aep-api's planner_skill.go
});

test("buildPlanInput falls back to the skill's (stripped) catalog content when no raw body is supplied", () => {
  const { input } = buildPlanInput("proj", files, [taskBreakdownSkill("# Task breakdown\n\nbody")]);
  assert.equal(input.taskBreakdownSkill?.body, "# Task breakdown\n\nbody");
});

test("buildPlanInput omits taskBreakdownSkill when task-breakdown isn't in the resolved skill set", () => {
  const { input } = buildPlanInput("proj", files, [], "---\nname: task-breakdown\n---\nbody\n");
  assert.equal(input.taskBreakdownSkill, undefined);
});
