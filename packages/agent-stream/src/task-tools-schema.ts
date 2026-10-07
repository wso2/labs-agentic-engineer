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
 * The Zod `inputSchema`s for the plan-turn tools. They live HERE (not in the
 * service's tool defs, unlike the file tools) because they are the single
 * definition the agents service uses as tool `inputSchema` AND the source the
 * published JSON Schema is rendered from (`json-schema.ts`) for the Go BFF to
 * vendor — one definition, no hand-kept copies (mirrors `componentDesignSchema`).
 *
 * PROPERTY ORDER IS LOAD-BEARING: the provider streams properties in declaration
 * order, so the short routable fields come first (a consumer can render a task
 * card the instant they resolve) and the long free-text fields (`rationale`,
 * `set.body`) come last, streaming delta-by-delta.
 *
 * The `.describe()` text is the model-facing tool doc; detailed planning behavior
 * stays in the `task-planning` skill, not here.
 */

import { z } from "zod";
import type { PlanTaskInput, UpdateTaskInput } from "./contracts/task-tools.js";
import type { Equal } from "./type-equal.js";

export const planTaskInputSchema = z.object({
  component: z
    .string()
    .describe("The design component this Task implements. Must be one of the known components (a directory under specs/design/components/)."),
  feature: z
    .string()
    .regex(/^(F[0-9]+|foundation)$/)
    .optional()
    .describe(
      "The feature this Task builds in its component (F2), or 'foundation' for the component's shared setup and the product-wide requirements the version carries. Omit only for a design with no features.",
    ),
  title: z
    .string()
    .describe("Short human-facing title; must be unique across planned and existing Tasks."),
  dependsOn: z
    .array(z.string())
    .describe("Component names this Task depends on (never issue numbers). Empty when it has no dependencies."),
  origin: z
    .enum(["spec-plan", "incident", "manual"])
    .optional()
    .describe("Where the Task came from; omit for the default 'spec-plan'."),
  rationale: z
    .string()
    .describe("One sentence: why this Task exists."),
});

const taskRefSchema = z
  .union([z.object({ issueNumber: z.number().int() }), z.object({ title: z.string() })])
  .describe("Target: { issueNumber } for an existing Task from context, or { title } for a Task planned earlier this turn.");

const updateTaskSetSchema = z
  .object({
    title: z.string().optional().describe("New title (must stay unique)."),
    dependsOn: z.array(z.string()).optional().describe("Replacement dependency component names."),
    rationale: z.string().optional().describe("New one-sentence rationale."),
    body: z.string().optional().describe("The full Task body markdown: scope, acceptance criteria, references into the spec/design."),
  })
  .describe("The fields to patch; omit a field to leave it unchanged.");

export const updateTaskInputSchema = z.object({
  ref: taskRefSchema,
  set: updateTaskSetSchema,
});

// --- Drift guard: Zod schema ⇄ task-tools wire type (cf. component-design-schema.ts) ---
const _drift: [
  Equal<z.infer<typeof planTaskInputSchema>, PlanTaskInput>,
  Equal<z.infer<typeof updateTaskInputSchema>, UpdateTaskInput>,
] = [true, true];
void _drift;
