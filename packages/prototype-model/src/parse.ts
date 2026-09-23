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
 * `parsePrototypeModel` is the one TypeScript entry point every layer uses to
 * accept a prototype: the agent's write gate, the console's reader, and the
 * serializer. It runs, in order and stopping at the first stage that fails:
 *
 *  1. the version check — a document that says it is another version is told
 *     so in one issue, instead of a schema dump about a document it is not;
 *  2. the structural pass (the Zod schema, also the published JSON Schema);
 *  3. the reference pass (`./references.ts`), including the component check
 *     when the caller knows the directory the file lives in.
 */

import { PROTOTYPE_SCHEMA_VERSION, type PrototypeModelV1 } from "./model.js";
import { formatJsonPath, type PrototypeValidationIssue } from "./issues.js";
import { prototypeModelSchema } from "./schema.js";
import { prototypeReferenceIssues } from "./references.js";

export type PrototypeParseResult =
  | { ok: true; model: PrototypeModelV1 }
  | { ok: false; issues: PrototypeValidationIssue[] };

export interface PrototypeParseOptions {
  /** The component directory the file is written to; `component` must equal it. */
  component?: string;
}

export function parsePrototypeModel(value: unknown, options: PrototypeParseOptions = {}): PrototypeParseResult {
  const version = unsupportedVersion(value);
  if (version) return { ok: false, issues: [version] };

  const shaped = prototypeModelSchema.safeParse(value);
  if (!shaped.success) {
    return {
      ok: false,
      issues: shaped.error.issues.map((i) => ({
        code: "SCHEMA_VIOLATION",
        path: formatJsonPath(i.path),
        message: i.message,
      })),
    };
  }

  const issues = prototypeReferenceIssues(shaped.data, options.component);
  return issues.length > 0 ? { ok: false, issues } : { ok: true, model: shaped.data };
}

function unsupportedVersion(value: unknown): PrototypeValidationIssue | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  if (!("schemaVersion" in value)) return undefined;
  const version = (value as { schemaVersion: unknown }).schemaVersion;
  if (version === PROTOTYPE_SCHEMA_VERSION) return undefined;
  return {
    code: "UNSUPPORTED_VERSION",
    path: "schemaVersion",
    message: `schemaVersion ${JSON.stringify(version)} is not supported; this platform reads version ${PROTOTYPE_SCHEMA_VERSION}`,
  };
}

/**
 * The one serialized form of a prototype: keys in schema order, a row's values
 * in its table's column order (then any other keys, sorted), arrays in
 * declared order, two-space indentation, one trailing newline. The same model
 * always yields the same bytes, whatever key order it was built in, so a
 * rewrite that changes nothing changes no bytes. Throws on an invalid model: a
 * file this function writes is one every gate accepts.
 */
export function stablePrototypeJson(model: PrototypeModelV1): string {
  const result = parsePrototypeModel(model);
  if (!result.ok) {
    const detail = result.issues.map((i) => `${i.code} at ${i.path || "(root)"}: ${i.message}`).join("; ");
    throw new Error(`stablePrototypeJson: the model is not a valid prototype — ${detail}`);
  }
  return `${JSON.stringify(canonicalRows(result.model), null, 2)}\n`;
}

/**
 * Row values are a record, so their key order is whatever the author wrote;
 * the schema cannot fix it. Put them in the order the table shows them. Tables
 * and task queues are the only nodes that carry both `columns` and `rows`.
 */
function canonicalRows(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalRows);
  if (typeof value !== "object" || value === null) return value;
  const out: Record<string, unknown> = {};
  for (const [key, child] of Object.entries(value)) out[key] = canonicalRows(child);
  const { columns, rows } = out;
  if (Array.isArray(columns) && Array.isArray(rows)) {
    out["rows"] = rows.map((row: { values: Record<string, string> }) => ({
      ...row,
      values: orderedValues(row.values, columns as string[]),
    }));
  }
  return out;
}

function orderedValues(values: Record<string, string>, columns: string[]): Record<string, string> {
  const keys = Object.keys(values);
  const ordered = [
    ...columns.filter((c) => Object.hasOwn(values, c)),
    ...keys.filter((k) => !columns.includes(k)).sort(),
  ];
  return Object.fromEntries(ordered.map((k) => [k, values[k]!]));
}
