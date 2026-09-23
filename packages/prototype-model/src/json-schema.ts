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
 * The prototype model's Zod schema published as JSON Schema (draft 2020-12),
 * so the Go save gate validates the SAME shape the agent's write gate does.
 * `scripts/generate-json-schema.ts` writes the checked-in artifact;
 * `test/json-schema.test.ts` fails when it drifts from a fresh render.
 */

import { z } from "zod";
import { prototypeModelSchema } from "./schema.js";

export function prototypeModelJsonSchema(): Record<string, unknown> {
  return z.toJSONSchema(prototypeModelSchema, { target: "draft-2020-12" }) as Record<string, unknown>;
}
