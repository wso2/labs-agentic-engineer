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
 * Reads `specs/acceptance/<slug>.feature` into the tree the view renders.
 *
 * A LINE SCANNER, not a Gherkin parser, and deliberately the same one the run
 * is checked with — `skills/acceptance-run/scripts/check-report.mjs`. That file
 * ships inside the skill and has no dependencies because it runs in a
 * validation pod; this one has none because `@cucumber/gherkin` is a parser
 * generator's worth of bytes to put in a console bundle for a grammar whose
 * whole surface here is `Feature` -> `Rule` -> `Scenario`.
 *
 * The two MUST agree on identity, because the report joins on it: a scenario
 * the checker counts and this file does not would render as though the run had
 * skipped it. `parseFeature.test.ts` pins that agreement against every feature
 * file in the repo.
 *
 * Three things the naive version gets wrong, all handled: a `#` comment, a
 * docstring whose body starts a line with `Scenario:`, and `Example:` — the
 * grammar's synonym for `Scenario:`, not a different construct. As in the
 * checker, `Scenario Outline:` counts as ONE scenario and its `Examples:` table
 * is not expanded: the report answers the outline once, so that is what the
 * file declares.
 */

/** One `Given` / `When` / `Then` / `And` / `But` line. */
export interface AcceptanceStep {
  readonly keyword: string;
  readonly text: string;
  /** 1-based, so a reader can be sent to the line. */
  readonly line: number;
}

export interface AcceptanceScenario {
  readonly name: string;
  /** `Scenario`, `Scenario Outline` or `Example` — the keyword as written. */
  readonly kind: string;
  readonly line: number;
  /** The scenario's OWN tags. Inherited ones are not copied down; see `negative`. */
  readonly tags: readonly string[];
  /** `@negative` anywhere up the chain — tags inherit Feature -> Rule -> Scenario. */
  readonly negative: boolean;
  readonly steps: readonly AcceptanceStep[];
}

export interface AcceptanceRule {
  /** Empty for scenarios written directly under the `Feature:`, which is legal. */
  readonly text: string;
  readonly tags: readonly string[];
  readonly scenarios: readonly AcceptanceScenario[];
}

export interface AcceptanceFeature {
  readonly name: string;
  /** The path it was read from, which is what the report's `featureFile` names. */
  readonly file: string;
  readonly tags: readonly string[];
  /**
   * `Background:` steps. The authoring skill discourages one and no generated
   * file has ever carried one — but they are parsed rather than ignored, so
   * that a file which does grow one cannot silently hang its steps off
   * whatever scenario happened to come before.
   */
  readonly background: readonly AcceptanceStep[];
  readonly rules: readonly AcceptanceRule[];
}

const NEGATIVE = "@negative";
const STEP = /^(Given|When|Then|And|But|\*)\s+(.*)$/;

/** `Scenario Outline:` must be tried before `Scenario:`, which is its prefix. */
const SCENARIO_KEYWORDS = ["Scenario Outline:", "Scenario:", "Example:"] as const;

interface MutableRule {
  text: string;
  tags: string[];
  scenarios: AcceptanceScenario[];
}

/**
 * Returns `null` for text with no `Feature:` line — the checker skips such a
 * file too, so a view that rendered it would show scenarios no run can answer.
 */
export function parseFeatureFile(file: string, text: string): AcceptanceFeature | null {
  let featureName = "";
  let featureTags: string[] = [];
  let pending: string[] = [];
  let fence = "";
  let inExamples = false;

  const background: AcceptanceStep[] = [];
  const rules: MutableRule[] = [];
  let rule: MutableRule | null = null;
  /** Where the next step line goes, or null when no block is open. */
  let steps: AcceptanceStep[] | null = null;

  const openRule = (): MutableRule => {
    if (rule === null) {
      rule = { text: "", tags: [], scenarios: [] };
      rules.push(rule);
    }
    return rule;
  };

  text.split("\n").forEach((raw, index) => {
    const line = raw.trim();

    // A docstring opens and closes on the same delimiter; everything between is
    // data the scenario quotes, not Gherkin.
    if (fence !== "") {
      if (line.startsWith(fence)) fence = "";
      return;
    }
    if (line.startsWith('"""') || line.startsWith("```")) {
      fence = line.startsWith('"""') ? '"""' : "```";
      return;
    }
    if (line === "" || line.startsWith("#")) return;

    // A tag line applies to whatever keyword comes next.
    if (line.startsWith("@")) {
      pending.push(...line.split(/\s+/).filter((t) => t.startsWith("@")));
      return;
    }

    // A table row — a step's data table, or an Examples body. Neither is a step.
    if (line.startsWith("|")) return;

    if (line.startsWith("Feature:")) {
      featureName = line.slice("Feature:".length).trim();
      featureTags = pending;
      pending = [];
      steps = null;
      inExamples = false;
      return;
    }

    if (line.startsWith("Background:")) {
      steps = background;
      pending = [];
      inExamples = false;
      return;
    }

    if (line.startsWith("Rule:")) {
      rule = { text: line.slice("Rule:".length).trim(), tags: pending, scenarios: [] };
      rules.push(rule);
      pending = [];
      steps = null;
      inExamples = false;
      return;
    }

    // Examples rows are consumed by the `|` branch above; this only has to stop
    // the heading itself being read as a step.
    if (line.startsWith("Examples:") || line.startsWith("Scenarios:")) {
      inExamples = true;
      steps = null;
      pending = [];
      return;
    }

    const keyword = SCENARIO_KEYWORDS.find((k) => line.startsWith(k));
    if (keyword !== undefined) {
      const own = pending;
      pending = [];
      inExamples = false;
      const owning = openRule();
      const inherited = [...featureTags, ...owning.tags, ...own];
      const scenarioSteps: AcceptanceStep[] = [];
      owning.scenarios.push({
        name: line.slice(keyword.length).trim(),
        kind: keyword.slice(0, -1),
        line: index + 1,
        tags: own,
        negative: inherited.includes(NEGATIVE),
        steps: scenarioSteps,
      });
      steps = scenarioSteps;
      return;
    }

    if (inExamples || steps === null) return;
    const step = STEP.exec(line);
    if (step !== null) {
      steps.push({ keyword: step[1] as string, text: (step[2] as string).trim(), line: index + 1 });
    }
  });

  if (featureName === "") return null;
  return { name: featureName, file, tags: featureTags, background, rules };
}

/** Every scenario in a feature, flattened — for counts and for the join. */
export function featureScenarios(
  feature: AcceptanceFeature,
): readonly { rule: AcceptanceRule; scenario: AcceptanceScenario }[] {
  return feature.rules.flatMap((rule) => rule.scenarios.map((scenario) => ({ rule, scenario })));
}
