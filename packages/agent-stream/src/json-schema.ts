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
 * Publish the shared Zod schemas as JSON Schema so the Go BFF validates against
 * the SAME definitions the agents service uses — one schema, not two hand-kept
 * copies (§8 of the migration decision record; §10.3 of tasks-github-native).
 *
 *  - `componentDesignJsonSchema` — the design.json write/save gate. The one
 *    contextual rule the agent adds (`name` must equal the component directory)
 *    is NOT expressible in a standalone JSON Schema (it needs the path), so both
 *    sides apply it separately (`checkComponentDesign` / the BFF save-gate).
 *  - `rolesDesignJsonSchema` — the roles.json write/save gate. Its referential
 *    rules (every `testUsers[].role` names a declared role, `coldStartRole`
 *    likewise) are equally inexpressible standalone, and are likewise applied
 *    separately on both sides.
 *  - `planTaskJsonSchema` / `updateTaskJsonSchema` — the plan-turn tool inputs
 *    the BFF plan tap acts on; vendored by phase 2.
 *
 * `scripts/generate-json-schema.ts` writes the checked-in artifacts from these;
 * `test/json-schema.test.ts` fails if any drifts from a fresh render.
 */

import { z } from "zod";
import { componentDesignSchema } from "./component-design-schema.js";
import { rolesDesignSchema } from "./roles-design-schema.js";
import { planTaskInputSchema, updateTaskInputSchema } from "./task-tools-schema.js";

/** The `ComponentDesign` structural schema as JSON Schema (draft 2020-12). */
export function componentDesignJsonSchema(): Record<string, unknown> {
  return z.toJSONSchema(componentDesignSchema, { target: "draft-2020-12" }) as Record<string, unknown>;
}

/** The `RolesDesign` structural schema as JSON Schema (draft 2020-12). */
export function rolesDesignJsonSchema(): Record<string, unknown> {
  return z.toJSONSchema(rolesDesignSchema, { target: "draft-2020-12" }) as Record<string, unknown>;
}

/** The `planTask` tool input as JSON Schema (draft 2020-12). */
export function planTaskJsonSchema(): Record<string, unknown> {
  return z.toJSONSchema(planTaskInputSchema, { target: "draft-2020-12" }) as Record<string, unknown>;
}

/** The `updateTask` tool input as JSON Schema (draft 2020-12). */
export function updateTaskJsonSchema(): Record<string, unknown> {
  return z.toJSONSchema(updateTaskInputSchema, { target: "draft-2020-12" }) as Record<string, unknown>;
}
