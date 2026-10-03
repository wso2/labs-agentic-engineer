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
 * Rewrite the kit reference in `skills/prototype/SKILL.md` from the kit's
 * `reference.md` (`@wso2/prototype-kit`). Wired into this package's `gen`
 * script (turbo `build`/`test`/`typecheck` run `gen` first);
 * `test/prototype-skill.test.ts` fails when the block is stale.
 *
 *   pnpm --filter @aep/agents gen
 */

import { readFileSync, writeFileSync } from "node:fs";
import { KIT_REFERENCE, PROTOTYPE_SKILL_PATH, withKitReference } from "./prototype-skill-reference.js";

const before = readFileSync(PROTOTYPE_SKILL_PATH, "utf8");
const after = withKitReference(before, readFileSync(KIT_REFERENCE, "utf8"));
if (after !== before) writeFileSync(PROTOTYPE_SKILL_PATH, after, "utf8");
process.stdout.write(`${after === before ? "unchanged" : "wrote"} ${PROTOTYPE_SKILL_PATH}\n`);
