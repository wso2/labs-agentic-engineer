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
 * The manifest's Zod schema as JSON Schema (draft 2020-12), so a non-
 * TypeScript reader validates the same shape. `scripts/generate.ts` writes
 * the committed `schema/prototype-manifest.schema.json`; the kit's test
 * fails when that file is stale.
 */

import { z } from "zod";
import { manifestSchema } from "./schema.js";

export function manifestJsonSchema(): Record<string, unknown> {
  return z.toJSONSchema(manifestSchema, { target: "draft-2020-12" }) as Record<string, unknown>;
}
