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
 * Regenerate the kit's committed artifacts (see `artifacts.ts`):
 *
 *   pnpm --filter @wso2/prototype-kit gen
 */

import { mkdirSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { ARTIFACTS } from "./artifacts.js";

for (const artifact of ARTIFACTS) {
  mkdirSync(dirname(artifact.path), { recursive: true });
  writeFileSync(artifact.path, artifact.render(), "utf8");
  process.stdout.write(`wrote ${artifact.path}\n`);
}
