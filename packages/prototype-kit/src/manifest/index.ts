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

/** `@wso2/prototype-kit/manifest`: the manifest's types, Zod schema, parser and JSON Schema. */

export type { PrototypeDisplayState, PrototypeFlow, PrototypeManifest, PrototypeRole, PrototypeScreen } from "./types.js";
export { PROTOTYPE_SCHEMA_VERSION, manifestSchema } from "./schema.js";
export { parseManifest, parseManifestJson, type ManifestResult } from "./parse.js";
export { manifestJsonSchema } from "./json-schema.js";
export { screensForRole } from "./screens.js";
