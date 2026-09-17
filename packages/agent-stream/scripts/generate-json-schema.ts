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
 * Regenerate the checked-in contract artifacts: the JSON Schemas rendered from
 * the Zod schemas, and the security-gate message catalog the BFF vendors. Wired
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
  dependencyDesignJsonSchema,
  securityDesignJsonSchema,
  planTaskJsonSchema,
  updateTaskJsonSchema,
} from "../src/json-schema.js";
import {
  COMPONENT_DESIGN_SCHEMA_ARTIFACT,
  DEPENDENCY_DESIGN_SCHEMA_ARTIFACT,
  SECURITY_DESIGN_SCHEMA_ARTIFACT,
  PLAN_TASK_SCHEMA_ARTIFACT,
  UPDATE_TASK_SCHEMA_ARTIFACT,
  SECURITY_DESIGN_MESSAGES_ARTIFACT,
  OPENAPI_SECURITY_MESSAGES_ARTIFACT,
} from "./artifact-path.js";
import { SECURITY_DESIGN_MESSAGES } from "../src/security-design-messages.js";
import { OPENAPI_SECURITY_MESSAGES } from "../src/openapi-security-messages.js";

const artifacts: [string, Record<string, unknown>][] = [
  [COMPONENT_DESIGN_SCHEMA_ARTIFACT, componentDesignJsonSchema()],
  [DEPENDENCY_DESIGN_SCHEMA_ARTIFACT, dependencyDesignJsonSchema()],
  [SECURITY_DESIGN_SCHEMA_ARTIFACT, securityDesignJsonSchema()],
  [PLAN_TASK_SCHEMA_ARTIFACT, planTaskJsonSchema()],
  [UPDATE_TASK_SCHEMA_ARTIFACT, updateTaskJsonSchema()],
  // Not schemas, but the same anti-drift deal: one source module, one
  // committed artifact, one freshness test. Both catalogs are vendored by the
  // BFF, so a reword that lands in one language and not the other would give
  // the two gates two wordings of one rule.
  [SECURITY_DESIGN_MESSAGES_ARTIFACT, SECURITY_DESIGN_MESSAGES],
  [OPENAPI_SECURITY_MESSAGES_ARTIFACT, OPENAPI_SECURITY_MESSAGES],
];

for (const [path, schema] of artifacts) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${JSON.stringify(schema, null, 2)}\n`, "utf8");
  process.stdout.write(`wrote ${path}\n`);
}
