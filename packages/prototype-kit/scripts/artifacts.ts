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
 * The kit's generated, committed files: what renders each, and where it
 * goes. `generate.ts` writes them; `test/generated.test.ts` fails when one is
 * stale.
 */

import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { manifestJsonSchema } from "../src/manifest/json-schema.js";
import { KIT_REFERENCE_PATH, kitReference } from "./kit-reference.js";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

export interface GeneratedArtifact {
  path: string;
  render: () => string;
}

export const ARTIFACTS: GeneratedArtifact[] = [
  { path: join(root, "schema", "prototype-manifest.schema.json"), render: () => `${JSON.stringify(manifestJsonSchema(), null, 2)}\n` },
  { path: KIT_REFERENCE_PATH, render: kitReference },
];
