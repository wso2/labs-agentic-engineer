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
 * What every prototype check reports: a stable code, the file the finding is
 * in, where in that file, and a message that says what to change. The codes
 * are a public contract — an agent branches on them — so a code is never
 * renamed or reused. Each code's one-line meaning below is published in the
 * generated reference.
 */

export const FINDING_CODES = {
  MISSING_FILE: "prototype.json or prototype.tsx is not in the folder.",
  SCHEMA_VIOLATION: "prototype.json is not JSON, or does not have the manifest's shape.",
  UNSUPPORTED_VERSION: "prototype.json's schemaVersion is not 3.",
  DUPLICATE_ID: "A role, state, screen or flow id repeats another; the four share one namespace.",
  UNKNOWN_REFERENCE: "A manifest reference names nothing, or a flow step its role cannot reach.",
  SYNTAX_ERROR: "prototype.tsx does not parse.",
  FORBIDDEN_IMPORT: "An import other than react and @wso2/prototype-kit, a dynamic import(), or require.",
  FORBIDDEN_API: "A browser, network, storage, code-generation or nondeterministic API.",
  FORBIDDEN_ELEMENT: "A raw HTML element; screens are drawn with kit components only.",
  SOURCE_TOO_LARGE: "prototype.tsx is over the size cap.",
  NO_APP: "prototype.tsx does not `export default defineApp({ screens, data })`.",
  SCREEN_MISMATCH: "defineApp's screens are not exactly the manifest's screens.",
  UNKNOWN_NAV_TARGET: "A `to` or `go()` names a screen that does not exist, or one the viewing role cannot reach.",
  RENDER_FAILED: "The module, or a screen for some role and display state, throws or runs too long.",
  DUPLICATE_ELEMENT_ID: "Two elements on one screen share an id.",
} as const;

export type FindingCode = keyof typeof FINDING_CODES;

/** The prototype file a finding is in. */
export type FindingFile = "prototype.json" | "prototype.tsx";

export const MANIFEST_FILE = "prototype.json" satisfies FindingFile;
export const SOURCE_FILE = "prototype.tsx" satisfies FindingFile;

export interface Finding {
  code: FindingCode;
  file: FindingFile;
  /**
   * Where in the file: a JSON path into the manifest (`flows[0].screenIds[1]`,
   * `(root)`), a source line (`line 12`), a render (`screen.home as admin in
   * state.empty`), `module` for the module as a whole, or `(file)`.
   */
  location: string;
  message: string;
}
