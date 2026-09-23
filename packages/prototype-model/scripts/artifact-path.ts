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
 * The published schema's single location, shared by the generator and the
 * freshness test. It lives in `packages/contracts/schemas/` beside the other
 * gated artifacts' schemas; the Go save gate vendors a copy
 * (services/aep-api/internal/platform/prototypespec).
 */

import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// scripts/ → packages/prototype-model → packages → contracts/schemas/…
const here = dirname(fileURLToPath(import.meta.url));

export const PROTOTYPE_MODEL_SCHEMA_ARTIFACT = join(here, "..", "..", "contracts", "schemas", "prototype-model.schema.json");
