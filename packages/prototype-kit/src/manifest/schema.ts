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
 * The manifest's Zod schema: the structural half of the validator, every
 * object strict (an unknown key is a finding). Published as JSON Schema by
 * `json-schema.ts`; the rules it cannot express are `references.ts`.
 */

import { z } from "zod";
import type { PrototypeManifest } from "./types.js";

export const PROTOTYPE_SCHEMA_VERSION = 3;

const id = z.string().min(1);
const label = z.string().min(1);
const named = z.strictObject({ id, name: label });

export const manifestSchema = z.strictObject({
  schemaVersion: z.literal(PROTOTYPE_SCHEMA_VERSION),
  name: label,
  entryScreen: id,
  roles: z.array(named).min(1),
  states: z.array(named).min(1),
  screens: z.array(z.strictObject({ id, name: label, roleIds: z.array(id).min(1) })).min(1),
  flows: z.array(z.strictObject({ id, name: label, roleId: id, screenIds: z.array(id).min(1) })),
});

// The schema and the hand-written types must describe the same object.
type Same<A, B> = [A] extends [B] ? ([B] extends [A] ? true : false) : false;
const sameShape: Same<z.infer<typeof manifestSchema>, PrototypeManifest> = true;
void sameShape;
