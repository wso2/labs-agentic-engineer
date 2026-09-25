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

import { THRESHOLD } from "./config.js";
import type { ScenarioFile } from "./scenario.js";

export interface ScenarioVerdict {
  id: string;
  score: number;
  passed: boolean;
  failed: { id: string; reason: string }[];
  // Rubric lines the grader never returned a verdict on, kept out of
  // `failed` on purpose: `failed` feeds the revision prompt verbatim, and a
  // line that was never graded is not something a prompt fix can address.
  ungraded: { id: string; reason: string }[];
  // Operations the agent called that its allow-list withholds, as
  // `ENV_VAR: operationId`. Kept apart from BOTH `failed` and `ungraded`: an
  // over-reach is neither a rubric miss nor a grading gap, it is a security
  // finding, and folding it into either bucket would ask a reader (or the
  // fix loop) to treat it as the wrong kind of problem.
  toolOverReach: string[];
}
export interface Verdict { passed: boolean; overall: number; scenarios: ScenarioVerdict[] }

interface ComponentResult {
  pass?: boolean;
  assertion?: { metric?: string };
  reason?: string;
  error?: string;
}
interface Row {
  error?: string;
  metadata?: RowMetadata;
  // promptfoo only attaches OUR metadata when the provider returns
  // normally — a row whose provider call threw (the common shape for an
  // errored scenario) carries none. `vars.scenario.id` is promptfoo's OWN
  // record of the test's input and survives that case, so it is the
  // fallback rather than a second, weaker guess.
  vars?: { scenario?: { id?: string } };
  gradingResult?: { componentResults?: ComponentResult[] };
}

interface RowMetadata {
  scenarioId?: string;
  toolOverReach?: string[];
}

function findComponent(parts: ComponentResult[], metric: string): ComponentResult | undefined {
  return parts.find((p) => p.assertion?.metric === metric);
}

// A component is gradeable only if promptfoo actually rendered a verdict for
// it — an errored or absent component is not a soft zero, it is missing data,
// and "achievable weight" must exclude it rather than count it as a miss.
function isGradeable(comp: ComponentResult | undefined): comp is ComponentResult & { pass: boolean } {
  return comp !== undefined && comp.error === undefined && typeof comp.pass === "boolean";
}

function scoreScenario(row: Row, file: ScenarioFile): ScenarioVerdict {
  const id = row.metadata?.scenarioId ?? row.vars?.scenario?.id ?? "?";
  // Present regardless of how this scenario scored — an over-reach is a
  // security signal independent of whether the rubric was otherwise met.
  const toolOverReach = row.metadata?.toolOverReach ?? [];

  // A row-level error means promptfoo never produced real grading for this
  // scenario at all (provider timeout, crash, ...). That is never a pass,
  // and the error text is the only thing worth citing back to a fix.
  if (row.error !== undefined) {
    return {
      id,
      score: 0,
      passed: false,
      failed: [{ id, reason: row.error }],
      ungraded: [],
      toolOverReach,
    };
  }

  const scenario = file.scenarios.find((s) => s.id === id);
  const parts = row.gradingResult?.componentResults ?? [];
  const failed: { id: string; reason: string }[] = [];
  const ungraded: { id: string; reason: string }[] = [];

  if (scenario === undefined) {
    return {
      id,
      score: 0,
      passed: false,
      failed: [{ id, reason: `no matching scenario in file for id "${id}"` }],
      ungraded: [],
      toolOverReach,
    };
  }

  // Score is OUR arithmetic over the declared mustCover weights, not
  // promptfoo's row-level mean — that mean also folds in mustNot lines and
  // cannot tell a missing component from a failed one. "Achievable" weight
  // is the weight of items that were actually gradeable; anything ungraded
  // is excluded from the denominator AND marks the scenario incomplete.
  let gradeableWeight = 0;
  let passedWeight = 0;
  let allGradeable = true;
  for (const item of scenario.rubric.mustCover) {
    const comp = findComponent(parts, item.id);
    if (!isGradeable(comp)) {
      allGradeable = false;
      ungraded.push({ id: item.id, reason: comp?.error ?? comp?.reason ?? "not graded" });
      continue;
    }
    gradeableWeight += item.weight;
    if (comp.pass) {
      passedWeight += item.weight;
    } else {
      failed.push({ id: item.id, reason: comp.reason ?? "" });
    }
  }
  const score = gradeableWeight > 0 ? passedWeight / gradeableWeight : 0;

  // mustNot keeps zero tolerance for a genuine violation. An UNGRADED
  // mustNot is not a satisfied veto either — it gets the identical
  // gradeability treatment as mustCover, so a grader crash on the one
  // dimension with zero tolerance can never silently wave a harm through.
  let mustNotViolated = false;
  for (const item of scenario.rubric.mustNot) {
    const comp = findComponent(parts, item.id);
    if (!isGradeable(comp)) {
      allGradeable = false;
      ungraded.push({ id: item.id, reason: comp?.error ?? comp?.reason ?? "not graded" });
      continue;
    }
    if (!comp.pass) {
      mustNotViolated = true;
      failed.push({ id: item.id, reason: comp.reason ?? "" });
    }
  }

  // An incomplete evaluation is not a pass: zero gradeable weight, or any
  // ungraded mustCover or mustNot item, blocks passing even at a perfect
  // partial score.
  const passed = !mustNotViolated && allGradeable && gradeableWeight > 0 && score >= THRESHOLD;
  return { id, score, passed, failed, ungraded, toolOverReach };
}

/**
 * promptfoo's JSON, read as the one thing the fix loop needs: which rubric
 * lines failed and why. The reasons are quoted verbatim into the revision
 * prompt, so a fix cites the line that drove it rather than guessing.
 *
 * `file` is required (not just the JSON) because the "0.8 of achievable
 * mustCover weight" threshold is OUR arithmetic, computed from the rubric's
 * declared weights against promptfoo's per-assertion componentResults — it
 * is not promptfoo's own row-level score, which mixes mustCover and mustNot
 * into one undifferentiated mean.
 */
export function readVerdict(json: unknown, file: ScenarioFile): Verdict {
  const rows = (json as { results?: { results?: unknown[] } })?.results?.results ?? [];
  const scenarios: ScenarioVerdict[] = rows.map((rowRaw) => scoreScenario(rowRaw as Row, file));
  const overall = scenarios.length
    ? scenarios.reduce((a, s) => a + s.score, 0) / scenarios.length
    : 0;
  // A missing, empty, or malformed result is not a clean pass — it is an
  // un-evaluated run, and reporting `passed: true` for it would let the fix
  // loop ship a prompt that was never actually graded.
  const passed = scenarios.length > 0 && scenarios.every((s) => s.passed);
  return { passed, overall, scenarios };
}
