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
 * Parser for `specs/validation/validation-criteria.json` — the acceptance
 * oracle authored by the validation-criteria skill. Turns the raw file text
 * into a tolerant, UI-friendly model the React view can render without knowing
 * the schema. Intentionally permissive — generated criteria may be partial
 * drafts; requirements/criteria missing their required identity (`id`, and
 * `statement`/`must`) are skipped, and a malformed file degrades to a
 * ParseError the view shows as an alert instead of throwing.
 *
 * The oracle is READ-ONLY: per-criterion run outcomes are NOT written back into
 * it. Coverage/pass/fail lives in the runner's separate report
 * (tests/acceptance/report.json — see report.ts), which the view joins onto
 * this structure by criterion id. The authored shape is defined server-side by
 * `criteriaDoc` in services/aep-api/internal/delivery/validation/criteria.go.
 */

/** One of e2e | scenario | manual; kept as a raw string so an unknown method
 *  still renders. */
export type CriterionMethod = "e2e" | "scenario" | "manual";

export interface Criterion {
  id: string;
  must: string;
  /** Kept as a raw string so an unknown method still renders. */
  method: CriterionMethod | string;
}

export interface Requirement {
  id: string;
  statement: string;
  criteria: Criterion[];
}

export interface ValidationCriteria {
  requirements: Requirement[];
}

export interface ParseError {
  kind: "parse-error";
  message: string;
}

export type ParseResult = ValidationCriteria | ParseError;

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}
function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function parseCriteria(v: unknown): Criterion[] {
  if (!Array.isArray(v)) return [];
  const out: Criterion[] = [];
  for (const item of v) {
    if (!isObject(item)) continue;
    const id = str(item.id);
    const must = str(item.must);
    // A criterion is only meaningful with an id and its assertion.
    if (!id || !must) continue;
    out.push({
      id,
      must,
      method: str(item.method) || "unknown",
    });
  }
  return out;
}

function parseRequirements(v: unknown): Requirement[] {
  if (!Array.isArray(v)) return [];
  const out: Requirement[] = [];
  for (const item of v) {
    if (!isObject(item)) continue;
    const id = str(item.id);
    const statement = str(item.statement);
    // A requirement needs at least an id and a statement to be shown.
    if (!id || !statement) continue;
    out.push({ id, statement, criteria: parseCriteria(item.criteria) });
  }
  return out;
}

export function parseValidationCriteria(raw: string): ParseResult {
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch (e) {
    return { kind: "parse-error", message: (e as Error).message };
  }
  if (!isObject(data)) {
    return {
      kind: "parse-error",
      message: "validation-criteria.json is not a JSON object",
    };
  }
  if (!Array.isArray(data.requirements)) {
    return {
      kind: "parse-error",
      message: "validation-criteria.json is missing a `requirements` array",
    };
  }
  return { requirements: parseRequirements(data.requirements) };
}
