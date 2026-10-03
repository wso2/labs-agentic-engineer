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
 * The manifest rules, asserted against the table the Go save gate asserts too
 * (`services/aep-api/internal/platform/prototypespec`). Same documents, same
 * findings: a rule changed on one side only fails a row on the other.
 */

import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { parseManifest, parseManifestJson } from "../src/manifest/index.js";

type PatchOp = { op: "set"; path: (string | number)[]; value: unknown } | { op: "remove"; path: (string | number)[] };

/**
 * `set` writes the value at the path (an index one past an array's end
 * appends); `remove` deletes the key, or splices the array element. The Go
 * test applies the same operations.
 */
function applyPatch(base: unknown, patch: PatchOp[]): unknown {
  const doc = structuredClone(base) as Record<string | number, unknown>;
  for (const op of patch) {
    const parent = op.path.slice(0, -1).reduce<Record<string | number, unknown>>((node, key) => node[key] as Record<string | number, unknown>, doc);
    const last = op.path[op.path.length - 1]!;
    if (op.op === "set") parent[last] = op.value;
    else if (Array.isArray(parent)) parent.splice(last as number, 1);
    else delete parent[last];
  }
  return doc;
}

interface Row {
  name: string;
  /** Applied to a copy of `base`. */
  patch?: PatchOp[];
  /** Replaces the base outright. */
  document?: unknown;
  /** Raw file text, for what is not JSON. */
  text?: string;
  /** Only the code is shared: each side words and places its own schema violations. */
  schemaOnly?: true;
  findings: { code: string; location?: string; message?: string }[];
}

const table = JSON.parse(readFileSync(new URL("./fixtures/manifest-cases.json", import.meta.url), "utf8")) as {
  base: unknown;
  cases: Row[];
};

describe("manifest cases shared with the Go save gate", () => {
  it.each(table.cases.map((c) => [c.name, c] as const))("%s", (_name, row) => {
    const result =
      row.text !== undefined ? parseManifestJson(row.text) : parseManifest(row.document ?? applyPatch(table.base, row.patch ?? []));
    const got = result.ok ? [] : result.findings;
    if (row.schemaOnly) {
      expect(got.map((f) => f.code)).toEqual(row.findings.map((f) => f.code));
      return;
    }
    expect(got.map(({ code, location, message }) => ({ code, location, message }))).toEqual(
      row.findings.map(({ code, location, message }) => ({ code, location, message })),
    );
  });
});
