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
 * Architect eval (PAID — real model calls). Run with `npm run eval:architect`.
 *
 * For each fixture we run the headless `runArchitect` K times at production
 * settings and score each output programmatically: the design's own
 * `validate()` (the very rules the route trusts) + schema validity + fixture
 * expectations. Output is a pass-rate report under `eval-results/` — a measured
 * signal, NOT a CI gate (see the harness header).
 */

import { test } from "node:test";
import { runArchitect } from "./run.js";
import type { FinalizeResolver } from "./tools.js";
import { ArchitectInput, ArchitectOutput } from "./schema.js";
import {
  modelFromEnv,
  makeCollectingSink,
  sampleCount,
  loadFixtures,
  aggregate,
  writeReport,
  formatSummary,
  allPass,
  type Check,
  type SampleResult,
} from "../../eval/harness.js";

interface ArchitectFixture {
  name: string;
  difficulty?: string;
  input: unknown; // validated to ArchitectInput at load time
  expect: {
    componentsMin?: number;
    componentsMax?: number;
    expectAuthService?: boolean;
    expectDependentApi?: boolean;
  };
}

async function runOnce(
  model: NonNullable<ReturnType<typeof modelFromEnv>>,
  input: ArchitectInput,
) {
  const { sink } = makeCollectingSink();
  const controller = new AbortController();
  const finalizer: FinalizeResolver = {
    finalized: false,
    resolve: () => controller.abort(),
  };
  return runArchitect({
    model,
    input,
    sink,
    finalizer,
    abortSignal: controller.signal,
  });
}

function score(
  result: Awaited<ReturnType<typeof runOnce>>,
  fx: ArchitectFixture,
): Check[] {
  const checks: Check[] = [];

  const parsed = ArchitectOutput.safeParse(result.design);
  checks.push({
    name: "schema-valid",
    pass: parsed.success,
    detail: parsed.success ? undefined : parsed.error.issues[0]?.message,
  });

  // finalize() only succeeds when validate() is clean, so these track together;
  // when NOT finalized, `validator-clean` shows which rules blocked it.
  checks.push({ name: "finalized", pass: result.finalized });
  checks.push({
    name: "validator-clean",
    pass: result.issues.length === 0,
    detail: result.issues.length
      ? result.issues.map((i) => i.code).join(",")
      : undefined,
  });

  const n = result.design.components.length;
  const min = fx.expect.componentsMin ?? 0;
  const max = fx.expect.componentsMax ?? Number.POSITIVE_INFINITY;
  checks.push({
    name: "component-count",
    pass: n >= min && n <= max,
    detail: `got ${n}, expect ${min}..${max === Number.POSITIVE_INFINITY ? "∞" : max}`,
  });

  if (fx.expect.expectAuthService !== undefined) {
    const hasAuth = result.design.components.some(
      (c) =>
        c.componentType === "service" &&
        c.exposesAPI?.auth === "end-user-required",
    );
    checks.push({
      name: "auth-service",
      pass: hasAuth === fx.expect.expectAuthService,
      detail: `hasAuthService=${hasAuth}, expected=${fx.expect.expectAuthService}`,
    });
  }

  if (fx.expect.expectDependentApi !== undefined) {
    const hasDep = result.design.components.some(
      (c) => (c.dependentApis?.length ?? 0) > 0,
    );
    checks.push({
      name: "dependent-api",
      pass: hasDep === fx.expect.expectDependentApi,
      detail: `hasDependentApi=${hasDep}, expected=${fx.expect.expectDependentApi}`,
    });
  }

  return checks;
}

test("architect eval", { timeout: 1_800_000 }, async (t) => {
  const model = modelFromEnv();
  if (!model) {
    t.skip("ANTHROPIC_API_KEY not set — skipping paid eval");
    return;
  }
  const modelName = process.env.AGENT_MODEL || "claude-sonnet-4-5";
  const fixtures = await loadFixtures<ArchitectFixture>(import.meta.url);
  const k = sampleCount();

  const results: SampleResult[] = [];
  for (const fx of fixtures) {
    const input = ArchitectInput.parse(fx.input);
    for (let s = 1; s <= k; s++) {
      const start = Date.now();
      try {
        const r = await runOnce(model, input);
        const checks = score(r, fx);
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
  const { mdPath } = await writeReport("architect", modelName, results, agg);
  console.log(formatSummary("architect", modelName, agg));
  console.log(`report: ${mdPath}`);
  // Report, not gate: intentionally no assertion on pass-rate.
});
