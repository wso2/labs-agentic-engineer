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
 * Loader for `validation-cases.json`, the parity table the Go validator reads
 * too. Each case is the shared base document plus a tiny patch, so a case says
 * exactly what it breaks.
 */

import { readFileSync } from "node:fs";

type Segment = string | number;

interface PatchOp {
  op: "set" | "remove";
  path: Segment[];
  value?: unknown;
}

export interface ExpectedIssue {
  code: string;
  path?: string;
}

export interface ValidationCase {
  name: string;
  component?: string;
  patch: PatchOp[];
  issues: ExpectedIssue[];
}

interface CaseFile {
  base: unknown;
  cases: ValidationCase[];
}

export const caseFile = JSON.parse(
  readFileSync(new URL("./validation-cases.json", import.meta.url), "utf8"),
) as CaseFile;

/** A fresh copy of the base document with `patch` applied. */
export function patched(patch: PatchOp[]): unknown {
  const doc = structuredClone(caseFile.base);
  for (const op of patch) {
    const parent = walk(doc, op.path.slice(0, -1));
    const key = op.path[op.path.length - 1]!;
    if (op.op === "set") {
      (parent as Record<Segment, unknown>)[key] = structuredClone(op.value);
    } else if (Array.isArray(parent) && typeof key === "number") {
      parent.splice(key, 1);
    } else {
      delete (parent as Record<Segment, unknown>)[key];
    }
  }
  return doc;
}

function walk(doc: unknown, path: Segment[]): unknown {
  let at = doc;
  for (const seg of path) at = (at as Record<Segment, unknown>)[seg];
  return at;
}
