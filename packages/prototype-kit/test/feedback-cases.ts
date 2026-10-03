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
 * The shared prototype feedback table (`fixtures/feedback-cases.json`): every
 * host that judges a batch reads the same rows — this kit, `@aep/agent-stream`
 * (by relative import of this module) and the Go BFF — so a limit or rule
 * changed on one side only fails a row on another.
 */

import { readFileSync } from "node:fs";

export interface FeedbackCase {
  name: string;
  valid: boolean;
  /** The row judges the AEP batch's `component`, which the kit's submission has not. */
  component?: true;
  batch?: Record<string, unknown>;
  /** Overrides on the first request; null removes the field. */
  request?: Record<string, unknown>;
  fill?: { field: string; unit: string; count: number };
  requestCount?: number;
}

export interface FeedbackTable {
  limits: { requests: number; text: number; id: number };
  base: Record<string, unknown>;
  cases: FeedbackCase[];
}

export const feedbackTable = JSON.parse(readFileSync(new URL("./fixtures/feedback-cases.json", import.meta.url), "utf8")) as FeedbackTable;

/** The batch a row describes, built from the table's base. */
export function feedbackBatch(row: FeedbackCase): Record<string, unknown> {
  const batch = structuredClone(feedbackTable.base);
  const first = (batch["requests"] as Record<string, unknown>[])[0]!;
  for (const [key, value] of Object.entries(row.request ?? {})) {
    if (value === null) delete first[key];
    else first[key] = value;
  }
  if (row.fill) {
    const value = row.fill.unit.repeat(row.fill.count);
    if (row.fill.field === "component") batch["component"] = value;
    else first[row.fill.field] = row.fill.field === "elementIds" ? [value] : value;
  }
  if (row.requestCount !== undefined) batch["requests"] = Array.from({ length: row.requestCount }, () => structuredClone(first));
  return { ...batch, ...row.batch };
}
