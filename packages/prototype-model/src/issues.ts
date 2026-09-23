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
 * The validator's findings. The codes and the JSON-path spelling are a
 * CONTRACT shared with the Go save gate
 * (services/aep-api/internal/platform/prototypespec): a document either gate
 * refuses, the other refuses with the same code at the same path, and
 * `test/validation-cases.json` is the table both sides assert.
 */

export type PrototypeIssueCode =
  /** The document does not have the v1 shape (the published JSON Schema). */
  | "SCHEMA_VIOLATION"
  /** `schemaVersion` is present and is not 1. */
  | "UNSUPPORTED_VERSION"
  /** An `id` repeats one already used anywhere in the document. */
  | "DUPLICATE_ID"
  /** A reference names nothing, or names something of the wrong kind or on another screen. */
  | "UNKNOWN_REFERENCE"
  /** `component` disagrees with the directory the file is written to. */
  | "PROTOTYPE_COMPONENT_MISMATCH";

export interface PrototypeValidationIssue {
  code: PrototypeIssueCode;
  /** Where, as `screens[0].content[2].id`; empty for the document root. */
  path: string;
  message: string;
}

/** Spell a path the way both gates do: dotted keys, bracketed indices. */
export function formatJsonPath(segments: readonly PropertyKey[]): string {
  let out = "";
  for (const seg of segments) {
    if (typeof seg === "number") out += `[${seg}]`;
    else out += out === "" ? String(seg) : `.${String(seg)}`;
  }
  return out;
}
