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

import {
  decidingStep,
  featureScenarios,
  isReportParseError,
  parseAcceptanceReport,
  parseFeatureFile,
  type ReportScenario,
} from "@aep/ui-acceptance-view";
import type { components } from "../../../generated/aep-api";

// A version's validation, grouped by feature (B4). The report is per scenario
// (the runner's report.json), read with the acceptance files it was judged
// against at one commit (get-validation-report). The snapshot also says how
// the version read it: what it validates (its scope — features not built yet
// are not run, nor rules whose stories wait on one), and how each failure
// stood in the previous validated version (a regression, or still failing).
//
// A scenario belongs to the feature its file's `Feature: F2 Approvals` line
// names, by ID — so a renamed feature keeps its scenarios — and is matched to
// the report and to the server's standing by the same key the server uses:
// the feature's ID, the rule and the scenario, joined with " / ".

type ValidationSnapshot = components["schemas"]["ValidationSnapshot"];

/**
 * passed | failed | blocked | unjudgeable as the report says; `pending` before
 * the report exists; `not-run` for a scenario outside the version's scope.
 */
export type ScenarioOutcome = string;

/** How a failure stood in the previous validated version. */
export type Standing = "regression" | "still-failing" | null;

export interface ScenarioResult {
  name: string;
  /** The story it proves ("F2.4"); null when it carries no story tag. */
  story: string | null;
  outcome: ScenarioOutcome;
  /** A failed scenario's deciding step: what it expected, and what it got instead. */
  expected: string | null;
  got: string | null;
  /** The scenario's steps as the run recorded them, as log lines. */
  excerpt: string[];
  /** A failure's standing against the baseline; null for a plain failure or a pass. */
  standing: Standing;
}

export interface FeatureResults {
  /** "F2", or the feature's title when nothing names an ID. */
  id: string;
  name: string;
  scenarios: ScenarioResult[];
  passed: number;
  /** Scenarios judged so far (not pending, not left out). */
  judged: number;
  /** Scenarios the version runs: all but those left out of its scope. */
  runs: number;
  /** The feature is designed but not built in this version or before: none of it is run. */
  notBuilt: boolean;
}

const STORY_TAG = /^@story-(.+)$/;
const FEATURE_OF_STORY = /^(F\d+)\./;
const FEATURE_TITLE = /^(F\d+)\s+(.+)$/;

function storyOf(tags: readonly string[][]): string | null {
  for (const list of tags) {
    for (const tag of list) {
      const m = STORY_TAG.exec(tag);
      if (m) return m[1] ?? null;
    }
  }
  return null;
}

function excerptOf(scenario: ReportScenario): string[] {
  const decider = decidingStep(scenario);
  const shown = decider ? scenario.steps.slice(0, scenario.steps.indexOf(decider) + 1) : scenario.steps;
  return shown.flatMap((step) => [
    `${step.keyword} ${step.text}`,
    ...(step.command !== undefined ? [`  $ ${step.command}${step.exit !== undefined ? `  (exit ${step.exit})` : ""}`] : []),
    ...(step.observed !== undefined ? [`  observed: ${step.observed}`] : []),
  ]);
}

function resultOf(name: string, story: string | null, reported: ReportScenario | undefined, standing: Standing): ScenarioResult {
  if (!reported) return { name, story, outcome: "pending", expected: null, got: null, excerpt: [], standing: null };
  const failed = reported.outcome === "failed";
  const decider = failed ? decidingStep(reported) : undefined;
  return {
    name,
    story,
    outcome: reported.outcome,
    expected: decider ? decider.text : null,
    got: decider ? (decider.observed ?? (decider.exit !== undefined ? `exit ${decider.exit}` : null)) : null,
    excerpt: excerptOf(reported),
    standing: failed ? standing : null,
  };
}

/** The server's scenario key: the feature's ID, the rule and the scenario, the empty parts left out. */
export function scenarioKey(featureId: string, rule: string, scenario: string): string {
  return [featureId, rule, scenario].map((p) => p.trim()).filter(Boolean).join(" / ");
}

/** A report entry's feature ID, as the server reads it: the `Feature:` line's, else its file's. */
function reportedFeatureId(r: ReportScenario): string {
  const file = (r as { featureFile?: string }).featureFile ?? "";
  return FEATURE_TITLE.exec(r.feature)?.[1] ?? /(?:^|\/)(F\d+)\b/.exec(file)?.[1] ?? r.feature;
}

/**
 * Every scenario of the criteria, grouped by the feature it proves, in the
 * order the files and scenarios come; each with its outcome from the report
 * (`pending` while there is none), its standing, or `not-run` when the
 * version's scope leaves it out.
 */
export function groupByFeature(
  snapshot: Pick<ValidationSnapshot, "criteria" | "report" | "scope" | "regressions" | "stillFailing">,
): FeatureResults[] {
  const parsed = snapshot.report ? parseAcceptanceReport(snapshot.report) : null;
  const reported = parsed && !isReportParseError(parsed) ? parsed.scenarios : [];
  const regressions = new Set(snapshot.regressions ?? []);
  const stillFailing = new Set(snapshot.stillFailing ?? []);
  const scope = snapshot.scope;
  const groups = new Map<string, FeatureResults>();

  for (const file of [...snapshot.criteria].sort((a, b) => a.path.localeCompare(b.path))) {
    const feature = parseFeatureFile(file.path, file.content);
    if (!feature) continue;
    const titled = FEATURE_TITLE.exec(feature.name);
    for (const { rule, scenario } of featureScenarios(feature)) {
      const story = storyOf([[...scenario.tags], [...rule.tags], [...feature.tags]]);
      const id = titled?.[1] ?? (story && FEATURE_OF_STORY.exec(story)?.[1]) ?? feature.name;
      const name = titled?.[2] ?? feature.name;
      const notBuilt = scope ? !scope.features.includes(id) : false;
      const group = groups.get(id) ?? { id, name, scenarios: [], passed: 0, judged: 0, runs: 0, notBuilt };
      const heldBack = scope !== undefined && scenario.stories.length > 0 && scenario.stories.every((st) => scope.heldBack.includes(st));
      const key = scenarioKey(id, rule.text, scenario.name);
      let result: ScenarioResult;
      if (notBuilt || heldBack) {
        result = { name: scenario.name, story, outcome: "not-run", expected: null, got: null, excerpt: [], standing: null };
      } else {
        const outcome = reported.find((r) => scenarioKey(reportedFeatureId(r), r.rule ?? "", r.scenario) === key);
        const standing: Standing = regressions.has(key) ? "regression" : stillFailing.has(key) ? "still-failing" : null;
        result = resultOf(scenario.name, story, outcome, standing);
        group.runs += 1;
        if (result.outcome !== "pending") group.judged += 1;
        if (result.outcome === "passed") group.passed += 1;
      }
      group.scenarios.push(result);
      groups.set(id, group);
    }
  }
  return [...groups.values()];
}

/** A version's validation at a glance: how many passed, of how many, and which failed. */
export interface ValidationOutcome {
  passed: number;
  /** Scenarios the version ran: those outside its scope are not counted. */
  total: number;
  failing: { featureId: string; featureName: string; story: string | null; name: string; standing: Standing }[];
  /** How many of the failing passed in the previous validated version. */
  regressions: number;
}

export function validationOutcome(groups: FeatureResults[]): ValidationOutcome {
  const failing = groups.flatMap((g) =>
    g.scenarios
      .filter((s) => s.outcome !== "passed" && s.outcome !== "pending" && s.outcome !== "not-run")
      .map((s) => ({ featureId: g.id, featureName: g.name, story: s.story, name: s.name, standing: s.standing })),
  );
  return {
    passed: groups.reduce((n, g) => n + g.passed, 0),
    total: groups.reduce((n, g) => n + g.runs, 0),
    failing,
    regressions: failing.filter((f) => f.standing === "regression").length,
  };
}
