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
 * Regenerate the checked-in JSON Schema artifacts from the Zod schemas. Wired
 * into this package's `gen` script (turbo `build` runs `gen` first), so the
 * artifacts cannot silently drift from their schemas; `test/json-schema.test.ts`
 * fails the build if any does.
 *
 *   pnpm --filter @aep/agent-stream gen
 */

import { mkdirSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
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
} from "./artifact-path.js";

const artifacts: [string, Record<string, unknown>][] = [
  [COMPONENT_DESIGN_SCHEMA_ARTIFACT, componentDesignJsonSchema()],
  [ROLES_DESIGN_SCHEMA_ARTIFACT, rolesDesignJsonSchema()],
  [PLAN_TASK_SCHEMA_ARTIFACT, planTaskJsonSchema()],
  [UPDATE_TASK_SCHEMA_ARTIFACT, updateTaskJsonSchema()],
];

for (const [path, schema] of artifacts) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${JSON.stringify(schema, null, 2)}\n`, "utf8");
  process.stdout.write(`wrote ${path}\n`);
}
