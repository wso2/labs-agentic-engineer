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
 * Does this payload actually describe the package and version we asked for?
 *
 * A cache keyed by coordinates is only as good as its answer to that question.
 * The key comes from our own argv, so a mismatch means the file on disk is not
 * what its path claims — a partially-written entry from an older layout, a
 * hand-copied file, a rename that went wrong. Any of those would otherwise serve
 * one package's signatures under another package's name, which is the single
 * worst thing this reader could do.
 *
 * It runs on the RAW JSON rather than the schema's output because zod strips
 * exactly the two fields it needs: `moduleSchema` has no `version` and
 * `centralDocsSchema` has no `apiDocsVersion`. Both are on the wire — verified
 * present in all nine fixtures. Adding them to the schema instead would make
 * them required reads and turn a cosmetic upstream change into a failed lookup,
 * which is the trade `schema.ts` deliberately does not make.
 *
 * Module matching uses the REQUESTED name, which is why `selectModule` had to
 * stop reading `modules[0]` first: a check that verifies one module while the
 * renderer reads another verifies nothing.
 */

import type { QualifiedName, Version } from "../qualified.js";

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

export function coordinatesMatch(raw: unknown, qualified: QualifiedName, version: Version): boolean {
  const root = asRecord(raw);
  if (!root) return false;
  if (typeof root["apiDocsVersion"] !== "string" || root["apiDocsVersion"] === "") return false;

  const docsData = asRecord(root["docsData"]);
  const modules = docsData?.["modules"];
  if (!Array.isArray(modules)) return false;

  return modules.some((entry) => {
    const module = asRecord(entry);
    if (!module) return false;
    const id = module["id"];
    const orgName = module["orgName"];
    if (typeof id !== "string" || orgName !== String(qualified.org)) return false;
    const named = id === String(qualified.name) || id.startsWith(`${String(qualified.name)}.`);
    return named && module["version"] === String(version);
  });
}
