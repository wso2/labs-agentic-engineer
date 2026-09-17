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
 * Parser for `tests/acceptance/report.json` — the deterministic run report the
 * e2e validation runner commits (generate-report.mjs, schemaVersion 1). Turns
 * the raw file text into a per-criterion status map the ValidationView overlays
 * onto the authored oracle (specs/validation/validation-criteria.json), joined
 * by criterion id.
 *
 * Intentionally permissive: a report is generated FROM the oracle, so every
 * criterion should appear, but a criterion absent from the report simply renders
 * with no state; a malformed file degrades to a ParseError the view surfaces as a
 * non-blocking warning (the oracle still renders).
 *
 * NOTE: nothing writes this shape on this branch — the acceptance run answers
 * per scenario, and `ValidationView` renders that report raw through `rawReport`
 * instead. This parser is kept because reports already merged into project repos
 * carry the criteria shape and are read long after they were written.
 */

import type { ParseError } from "./parse.js";

/** The closed set of per-criterion states generate-report.mjs emits. Kept open
 *  to a raw string so an unknown status still renders. */
export type CriterionRunState =
  | "pass" // e2e passed
  | "fail" // e2e failed
  | "not_run" // e2e mapped but not executed
  | "manual" // manual method — human checklist item
  | "not_validated"; // scenario method — out of scope for automation

/** One criterion's entry in the run report (the fields the view surfaces). */
export interface CriterionReport {
  /** Raw status string; one of CriterionRunState in practice. */
  status: CriterionRunState | string;
  /** Failure message for a failed e2e criterion. */
  failure?: string;
  /** `<spec file>:<line>` the failure was raised at, when the reporter had it. */
  failureLocation?: string;
  /** Spec file path backing an e2e criterion. */
  spec?: string;
  /** The spec was repaired by the healer this run. */
  healed?: boolean;
  /** The test was flaky (passed on retry). */
  flaky?: boolean;
  /** Wall-clock duration of the e2e test, ms. */
  durationMs?: number;
}

/** criterion id → its run report entry. */
export type ValidationReport = Map<string, CriterionReport>;

export type ReportParseResult = ValidationReport | ParseError;

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}
function optBool(v: unknown): boolean | undefined {
  return typeof v === "boolean" ? v : undefined;
}
function optNum(v: unknown): number | undefined {
  return typeof v === "number" && Number.isFinite(v) ? v : undefined;
}
function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/**
 * A criterion's failure, which generate-report.mjs writes as an OBJECT —
 * `{ message, location }` (specOutcome). Reading it with str() returned "" for
 * every real report, so the view's failure block was dead in production while the
 * tests passed on string-shaped fixtures.
 *
 * A plain string is still accepted: reports already merged into project repos
 * carry that shape, and a report is read long after it was written.
 */
function parseFailure(v: unknown): { message: string; location: string } {
  if (typeof v === "string") return { message: v, location: "" };
  if (isObject(v)) return { message: str(v.message), location: str(v.location) };
  return { message: "", location: "" };
}

export function parseValidationReport(raw: string): ReportParseResult {
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch (e) {
    return { kind: "parse-error", message: (e as Error).message };
  }
  if (!isObject(data)) {
    return { kind: "parse-error", message: "report.json is not a JSON object" };
  }
  if (!Array.isArray(data.criteria)) {
    return {
      kind: "parse-error",
      message: "report.json is missing a `criteria` array",
    };
  }
  const out: ValidationReport = new Map();
  for (const item of data.criteria) {
    if (!isObject(item)) continue;
    const id = str(item.id);
    const status = str(item.status);
    // A row is only meaningful with an id and a status to join/render.
    if (!id || !status) continue;
    const entry: CriterionReport = { status };
    const failure = parseFailure(item.failure);
    if (failure.message) entry.failure = failure.message;
    if (failure.location) entry.failureLocation = failure.location;
    const spec = str(item.spec);
    if (spec) entry.spec = spec;
    const healed = optBool(item.healed);
    if (healed !== undefined) entry.healed = healed;
    const flaky = optBool(item.flaky);
    if (flaky !== undefined) entry.flaky = flaky;
    const durationMs = optNum(item.durationMs);
    if (durationMs !== undefined) entry.durationMs = durationMs;
    out.set(id, entry);
  }
  return out;
}
