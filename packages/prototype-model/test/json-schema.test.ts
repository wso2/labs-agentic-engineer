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
 * Anti-drift gate: the checked-in JSON Schema must equal a fresh render of the
 * Zod schema. If this fails, run `pnpm --filter @aep/prototype-model gen`.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import { prototypeModelJsonSchema } from "../src/index.ts";
import { PROTOTYPE_MODEL_SCHEMA_ARTIFACT } from "../scripts/artifact-path.ts";

test("checked-in prototype-model.schema.json matches a fresh generation", () => {
  const onDisk = JSON.parse(readFileSync(PROTOTYPE_MODEL_SCHEMA_ARTIFACT, "utf8")) as unknown;
  assert.deepEqual(onDisk, prototypeModelJsonSchema(), "stale — run `pnpm --filter @aep/prototype-model gen`");
  assert.equal(
    readFileSync(PROTOTYPE_MODEL_SCHEMA_ARTIFACT, "utf8"),
    `${JSON.stringify(prototypeModelJsonSchema(), null, 2)}\n`,
    "the artifact is not in its generated form — run `pnpm --filter @aep/prototype-model gen`",
  );
});

test("the published schema is strict and pins the version", () => {
  const schema = prototypeModelJsonSchema() as {
    additionalProperties?: unknown;
    properties?: { schemaVersion?: { const?: unknown } };
  };
  assert.equal(schema.additionalProperties, false);
  assert.equal(schema.properties?.schemaVersion?.const, 1);
});
