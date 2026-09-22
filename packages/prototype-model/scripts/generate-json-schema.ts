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
 * Regenerate `packages/contracts/schemas/prototype-model.schema.json` from the
 * Zod schema. Wired into this package's `gen` script (turbo `build` runs `gen`
 * first); `test/json-schema.test.ts` fails the build when the artifact is
 * stale.
 *
 *   pnpm --filter @aep/prototype-model gen
 */

import { mkdirSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { prototypeModelJsonSchema } from "../src/json-schema.js";
import { PROTOTYPE_MODEL_SCHEMA_ARTIFACT } from "./artifact-path.js";

mkdirSync(dirname(PROTOTYPE_MODEL_SCHEMA_ARTIFACT), { recursive: true });
writeFileSync(PROTOTYPE_MODEL_SCHEMA_ARTIFACT, `${JSON.stringify(prototypeModelJsonSchema(), null, 2)}\n`, "utf8");
process.stdout.write(`wrote ${PROTOTYPE_MODEL_SCHEMA_ARTIFACT}\n`);
