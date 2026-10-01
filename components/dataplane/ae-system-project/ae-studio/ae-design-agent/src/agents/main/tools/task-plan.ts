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
 * The task-plan tools (`planTask`/`updateTask`) the generic agent calls on a
 * `toolset: "task-plan"` turn, built over a `TaskPlan` accumulator. `execute()`
 * validates + accumulates ONLY — the service never touches GitHub; the BFF plan
 * tap performs the issue writes off the stream.
 *
 * The Zod `inputSchema`s are imported from `@aep/agent-stream` rather than
 * redeclared here (the file tools keep theirs local): they are the SAME
 * definition the published JSON Schema is rendered from — the agent-stream
 * package must own them because it, not this service, generates the artifacts the
 * Go BFF vendors. They are drift-guarded against the wire `*Input` types there,
 * and their property order (short routable fields first, long strings last) is
 * fixed there for the streaming UX.
 */

import { tool } from "ai";
import type { Tool } from "ai";
import {
  PLAN_TASK,
  UPDATE_TASK,
  planTaskInputSchema,
  updateTaskInputSchema,
  type PlanTaskResult,
  type UpdateTaskResult,
} from "@aep/agent-stream";
import { buildSkillTools } from "./skill-tools.js";
import type { SkillSource } from "../skill-source.js";
import type { TaskPlan } from "../task-plan-accumulator.js";

/**
 * Build the task-plan tool set bound to one accumulator for the turn. Registers
 * `planTask` + `updateTask` and (when skills are supplied) the shared skill
 * loaders — NO file tools. The catalog behaves exactly as on a files turn.
 */
export function buildTaskPlanTools(plan: TaskPlan, skills?: SkillSource): Record<string, Tool> {
  const tools: Record<string, Tool> = {
    [PLAN_TASK]: tool({
      description:
        "Plan a new Task for ONE design component: what should be built/changed and its dependencies. component must " +
        "be a known component; dependsOn lists component names (never issue numbers); title must be unique. Returns " +
        "the normalized operation, or a self-correctable error (UNKNOWN_COMPONENT / DUPLICATE_TITLE / DEPENDENCY_CYCLE).",
      inputSchema: planTaskInputSchema,
      // Drop an undefined optional (the AI SDK widens optionals to `| undefined`;
      // the wire type keeps them exact) so the accumulator's contract stays clean.
      execute: async ({ component, title, dependsOn, origin, rationale }): Promise<PlanTaskResult> =>
        plan.planTask({ component, title, dependsOn, rationale, ...(origin ? { origin } : {}) }),
    }),

    [UPDATE_TASK]: tool({
      description:
        "Patch a Task's fields and/or write its body. Reference a Task planned earlier this turn by { title }, or an " +
        "existing Task from context by { issueNumber }. Use it after planning to write each Task's full body (scope, " +
        "acceptance criteria, references into the spec/design). Returns the resolved ref, or a self-correctable error " +
        "(UNKNOWN_REF / DUPLICATE_TITLE / UNKNOWN_COMPONENT / DEPENDENCY_CYCLE). There is no close operation.",
      inputSchema: updateTaskInputSchema,
      execute: async ({ ref, set }): Promise<UpdateTaskResult> =>
        plan.updateTask({
          ref,
          set: {
            ...(set.title !== undefined ? { title: set.title } : {}),
            ...(set.dependsOn !== undefined ? { dependsOn: set.dependsOn } : {}),
            ...(set.rationale !== undefined ? { rationale: set.rationale } : {}),
            ...(set.body !== undefined ? { body: set.body } : {}),
          },
        }),
    }),
  };

  return { ...tools, ...buildSkillTools(skills) };
}
