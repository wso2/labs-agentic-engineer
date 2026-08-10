/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * `@aep/ballerina-central` — read a Ballerina package's public API off Central
 * and render it as Ballerina.
 *
 * The `bal-library` command is one consumer of this module and the tests are
 * another: the corpus runs fixtures through the real pipeline rather than
 * shelling out, which is what makes a rendering regression a readable diff
 * instead of an exit code.
 */

export { CENTRAL_BASE_URL, fetchDocs, fetchJson, parseCentralDocs, parseDependenciesToml } from "./central/client.js";
export type { FetchLike, HttpOptions } from "./central/client.js";
export { centralDocsSchema } from "./central/schema.js";
export type { CentralDocs, CentralType } from "./central/schema.js";
export { fromCentral } from "./from-central.js";
export { loadLibrary, loadReadmes, renderLibrary, renderReadme, resolveVersion } from "./library.js";
export type { ResolveOptions } from "./library.js";
export { applyPatches } from "./patches.js";
export { collectReadmes, toReadmeDocument } from "./readme.js";
export type { ModuleReadme } from "./readme.js";
export { toSyntaxString } from "./render.js";
export { formatQualifiedName, parseQualifiedName, parseVersion } from "./qualified.js";
export type { Org, PkgName, QualifiedName, Version } from "./qualified.js";
export { describeFailure, err, exitCodeFor, ok } from "./result.js";
export type { Failure, Result, SchemaIssue } from "./result.js";
export type * from "./model.js";
