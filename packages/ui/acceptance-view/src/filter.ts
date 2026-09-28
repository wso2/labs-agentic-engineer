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

import { featureScenarios, type AcceptanceFeature } from "./parseFeature.js";
import { scenarioKey, type AcceptanceReport } from "./report.js";

/**
 * Narrowing a set of scenarios: free text, tags, and outcome.
 *
 * Pure and rendering-free so the predicate can be tested on its own — the
 * combination of three filters is where this sort of thing goes wrong, and a
 * component test is a poor place to find out.
 */

/** The segmented control's leftmost option: no outcome filter at all. */
export const ANY_STATUS = "all";

/**
 * A scenario the report does not cover. Its own value, not a missing one, so
 * it can be filtered for like any other — `outcomeLabel("")` already renders it
 * as `No result`.
 */
export const NO_RESULT = "";

export interface ScenarioFilter {
  readonly query: string;
  readonly tags: readonly string[];
  readonly status: string;
}

export const NO_FILTER: ScenarioFilter = { query: "", tags: [], status: ANY_STATUS };

export function isFiltering(filter: ScenarioFilter): boolean {
  return filter.query !== "" || filter.tags.length > 0 || filter.status !== ANY_STATUS;
}

/** What a query is matched against: everything a reader can see on the row. */
export function haystack(parts: {
  readonly feature: string;
  readonly rule: string;
  readonly scenario: string;
  readonly tags: readonly string[];
  readonly steps: readonly { readonly text: string }[];
}): string {
  return [
    parts.feature,
    parts.rule,
    parts.scenario,
    ...parts.tags,
    ...parts.steps.map((s) => s.text),
  ]
    .join(" ")
    .toLowerCase();
}

export interface FilterableScenario {
  readonly hay: string;
  readonly tags: readonly string[];
  /**
   * The outcome AS RENDERED, which for an uncovered scenario is `NO_RESULT`
   * rather than nothing. Filtering on `report.outcome` instead would drop every
   * uncovered row out of every segment, `All` included — the same trap
   * BuildsLedger documents for its own status filter.
   */
  readonly outcome: string;
}

export function matchesFilter(s: FilterableScenario, filter: ScenarioFilter): boolean {
  if (filter.status !== ANY_STATUS && s.outcome !== filter.status) return false;
  // EVERY selected tag, not any: two tags narrow, they do not widen.
  if (!filter.tags.every((t) => s.tags.includes(t))) return false;
  if (filter.query !== "" && !s.hay.includes(filter.query.toLowerCase())) return false;
  return true;
}

/** `@negative` first, then the story tags in order; anything else after. */
export function deriveTags(features: readonly AcceptanceFeature[]): readonly string[] {
  const seen = new Set<string>();
  for (const f of features) {
    for (const t of f.tags) seen.add(t);
    for (const r of f.rules) {
      for (const t of r.tags) seen.add(t);
      for (const s of r.scenarios) for (const t of s.tags) seen.add(t);
    }
  }
  return [...seen].sort(compareTags);
}

const STORY = /^@story-(\d+)$/;

function compareTags(a: string, b: string): number {
  if (a === "@negative") return -1;
  if (b === "@negative") return 1;
  const [na, nb] = [STORY.exec(a)?.[1], STORY.exec(b)?.[1]];
  // Numeric, so @story-10 sorts after @story-9 rather than between 1 and 2.
  if (na !== undefined && nb !== undefined) return Number(na) - Number(nb);
  if (na !== undefined) return -1;
  if (nb !== undefined) return 1;
  return a.localeCompare(b);
}

/**
 * The outcome options worth offering, which is only the ones present.
 *
 * A run with nothing blocked should not carry a `Blocked` segment that can only
 * ever empty the list, and the control stays narrow enough to read.
 */
export function deriveStatuses(
  features: readonly AcceptanceFeature[],
  report: AcceptanceReport | undefined,
  awaiting = false,
): readonly string[] {
  if (report === undefined) return [];
  const seen = new Set<string>();
  for (const f of features) {
    for (const { rule, scenario } of featureScenarios(f)) {
      const entry = report.byKey.get(scenarioKey(f.name, rule.text, scenario.name));
      seen.add(entry?.outcome ?? NO_RESULT);
    }
  }
  // While an attempt is in flight an uncovered scenario carries no pill, so a
  // `No result` segment would select rows showing no state at all.
  if (awaiting) seen.delete(NO_RESULT);
  if (seen.size < 2) return [];
  const order = ["passed", "failed", "blocked", "unjudgeable", NO_RESULT];
  const known = order.filter((o) => seen.has(o));
  const rest = [...seen].filter((o) => !order.includes(o)).sort();
  return [ANY_STATUS, ...known, ...rest];
}
