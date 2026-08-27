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
 * Anti-drift gate: the checked-in JSON Schema artifact must equal a fresh render
 * of `componentDesignSchema`. If this fails, the Zod schema changed without
 * regenerating — run `pnpm --filter @aep/agent-stream gen`.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  componentDesignJsonSchema,
  rolesDesignJsonSchema,
  planTaskJsonSchema,
  updateTaskJsonSchema,
} from "../src/json-schema.js";
import {
  COMPONENT_DESIGN_SCHEMA_ARTIFACT,
  ROLES_DESIGN_SCHEMA_ARTIFACT,
  PLAN_TASK_SCHEMA_ARTIFACT,
  UPDATE_TASK_SCHEMA_ARTIFACT,
} from "../scripts/artifact-path.js";

// Every published artifact must equal a fresh render of its Zod schema; a stale
// one means the schema changed without `pnpm --filter @aep/agent-stream gen`.
const artifacts: [string, string, () => Record<string, unknown>][] = [
  ["component-design.schema.json", COMPONENT_DESIGN_SCHEMA_ARTIFACT, componentDesignJsonSchema],
  ["roles-design.schema.json", ROLES_DESIGN_SCHEMA_ARTIFACT, rolesDesignJsonSchema],
  ["plan-task.schema.json", PLAN_TASK_SCHEMA_ARTIFACT, planTaskJsonSchema],
  ["update-task.schema.json", UPDATE_TASK_SCHEMA_ARTIFACT, updateTaskJsonSchema],
];

for (const [name, path, render] of artifacts) {
  test(`checked-in ${name} matches a fresh generation`, () => {
    const onDisk = readFileSync(path, "utf8");
    const fresh = `${JSON.stringify(render(), null, 2)}\n`;
    assert.equal(onDisk, fresh, `${name} is stale — run \`pnpm --filter @aep/agent-stream gen\``);
  });
}

test("plan-task/update-task schemas expose their routable fields first (streaming order)", () => {
  const plan = planTaskJsonSchema() as { properties?: Record<string, unknown> };
  assert.deepEqual(Object.keys(plan.properties ?? {}), ["component", "title", "dependsOn", "origin", "rationale"]);
  const update = updateTaskJsonSchema() as { properties?: { set?: { properties?: Record<string, unknown> } } };
  // `body` (the long free-text field) streams last within `set`.
  assert.deepEqual(Object.keys(update.properties?.set?.properties ?? {}), ["title", "dependsOn", "rationale", "body"]);
});

test("the schema is a strict object exposing the ComponentDesign fields", () => {
  const schema = componentDesignJsonSchema() as {
    type?: string;
    additionalProperties?: boolean;
    required?: string[];
    properties?: Record<string, unknown>;
  };
  assert.equal(schema.type, "object");
  assert.equal(schema.additionalProperties, false, "strictObject → additionalProperties:false for the BFF gate");
  for (const key of ["name", "type", "version", "language", "buildpack", "appPath", "entrypoint", "exposure", "dependencies", "description"]) {
    assert.ok(schema.properties && key in schema.properties, `missing property ${key}`);
    assert.ok(schema.required?.includes(key), `expected ${key} required`);
  }
});
