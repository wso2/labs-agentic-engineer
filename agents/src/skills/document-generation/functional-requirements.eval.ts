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
 * functional-requirements eval (PAID). A PROSE skill — there is no `validate()`
 * for freeform Markdown, so scoring is structural: required headings present,
 * EARS FR-count in range, uses SHALL, no code fences. Report, not gate.
 */

import { test } from "node:test";
import { runDocGenSkill } from "./run.js";
import { functionalRequirements } from "./functional-requirements.js";
import {
  modelFromEnv,
  sampleCount,
  loadFixtures,
  aggregate,
  writeReport,
  formatSummary,
  allPass,
  type Check,
  type SampleResult,
} from "../../eval/harness.js";

interface FrFixture {
  name: string;
  difficulty?: string;
  input: { sources: Record<string, string>; prompt?: string };
  expect: { frMin?: number; frMax?: number };
}

/** Distinct `FR-<n>` labels — the skill numbers each requirement FR-1, FR-2… */
function countFRs(md: string): number {
  const m = md.match(/\bFR-\d+\b/g);
  return m ? new Set(m).size : 0;
}

function score(text: string, fx: FrFixture): Check[] {
  const checks: Check[] = [];

  const headings = [
    "# Overview",
    "# Actors",
    "# Functional Requirements",
    "# Out of Scope",
  ];
  const missing = headings.filter((h) => !text.includes(h));
  checks.push({
    name: "headings-present",
    pass: missing.length === 0,
    detail: missing.length ? `missing: ${missing.join(", ")}` : undefined,
  });

  const fr = countFRs(text);
  const frMin = fx.expect.frMin ?? 15;
  const frMax = fx.expect.frMax ?? 30;
  checks.push({
    name: "fr-count",
    pass: fr >= frMin && fr <= frMax,
    detail: `got ${fr}, expect ${frMin}..${frMax}`,
  });

  checks.push({ name: "uses-shall", pass: /\bSHALL\b/.test(text) });
  checks.push({ name: "no-code-fences", pass: !text.includes("```") });

  return checks;
}

test("functional-requirements eval", { timeout: 1_800_000 }, async (t) => {
  const model = modelFromEnv();
  if (!model) {
    t.skip("ANTHROPIC_API_KEY not set — skipping paid eval");
    return;
  }
  const modelName = process.env.AGENT_MODEL || "claude-sonnet-4-5";
  const fixtures = await loadFixtures<FrFixture>(
    import.meta.url,
    "__fixtures__/functional-requirements",
  );
  const k = sampleCount();

  const results: SampleResult[] = [];
  for (const fx of fixtures) {
    for (let s = 1; s <= k; s++) {
      const start = Date.now();
      try {
        const r = await runDocGenSkill(model, functionalRequirements, fx.input);
        const checks = score(r.text, fx);
        results.push({
          fixture: fx.name,
          difficulty: fx.difficulty,
          sample: s,
          pass: allPass(checks),
          checks,
          usage: r.usage,
          ms: Date.now() - start,
        });
      } catch (err) {
        results.push({
          fixture: fx.name,
          difficulty: fx.difficulty,
          sample: s,
          pass: false,
          checks: [],
          error: err instanceof Error ? err.message : String(err),
          ms: Date.now() - start,
        });
      }
      console.log(
        `[eval] ${fx.name} #${s}/${k} → ${results[results.length - 1].pass ? "PASS" : "FAIL"}`,
      );
    }
  }

  const agg = aggregate(results);
  const { mdPath } = await writeReport(
    "functional-requirements",
    modelName,
    results,
    agg,
  );
  console.log(formatSummary("functional-requirements", modelName, agg));
  console.log(`report: ${mdPath}`);
});
