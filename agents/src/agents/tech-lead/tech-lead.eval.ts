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
 * Tech-lead PLAN eval (PAID). Runs `runTechLeadPlan` K times per fixture and
 * scores each plan with the planner's OWN `validatePlan` (run inside the
 * runner) + non-emptiness + full-component coverage. Report, not gate.
 */

import { test } from "node:test";
import { runTechLeadPlan } from "./run.js";
import { TechLeadPlanInput } from "./schema.js";
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

interface TechLeadFixture {
  name: string;
  difficulty?: string;
  input: unknown; // validated to TechLeadPlanInput at load
  expect: { minItems?: number; coverAllComponents?: boolean };
}

function score(
  result: Awaited<ReturnType<typeof runTechLeadPlan>>,
  fx: TechLeadFixture,
  input: TechLeadPlanInput,
): Check[] {
  const checks: Check[] = [];

  const min = fx.expect.minItems ?? 1;
  checks.push({
    name: "min-items",
    pass: result.items.length >= min,
    detail: `got ${result.items.length}, min ${min}`,
  });

  checks.push({
    name: "validator-clean",
    pass: result.issues.length === 0,
    detail: result.issues.length
      ? result.issues.map((i) => i.code).join(",")
      : undefined,
  });

  if (fx.expect.coverAllComponents) {
    const targeted = new Set(result.items.map((i) => i.componentName));
    const missing = input.slimDesign
      .map((c) => c.name)
      .filter((n) => !targeted.has(n));
    checks.push({
      name: "covers-all-components",
      pass: missing.length === 0,
      detail: missing.length ? `missing: ${missing.join(",")}` : undefined,
    });
  }

  return checks;
}

test("tech-lead plan eval", { timeout: 1_800_000 }, async (t) => {
  const model = modelFromEnv();
  if (!model) {
    t.skip("ANTHROPIC_API_KEY not set — skipping paid eval");
    return;
  }
  const modelName = process.env.AGENT_MODEL || "claude-sonnet-4-5";
  const fixtures = await loadFixtures<TechLeadFixture>(import.meta.url);
  const k = sampleCount();

  const results: SampleResult[] = [];
  for (const fx of fixtures) {
    const input = TechLeadPlanInput.parse(fx.input);
    for (let s = 1; s <= k; s++) {
      const start = Date.now();
      try {
        const r = await runTechLeadPlan({ model, input });
        const checks = score(r, fx, input);
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
  const { mdPath } = await writeReport("tech-lead", modelName, results, agg);
  console.log(formatSummary("tech-lead", modelName, agg));
  console.log(`report: ${mdPath}`);
});
