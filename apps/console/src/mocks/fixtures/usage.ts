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

import type { components } from "../../generated/aep-api";
import type { ProjectScenario } from "./project";

type Usage = components["schemas"]["Usage"];
type ProjectUsage = components["schemas"]["ProjectUsage"];
type ModelPricingList = components["schemas"]["ModelPricingList"];
type UsageEstimateOutputBody = components["schemas"]["UsageEstimateOutputBody"];

// Pricing catalog (#245): mirrors aep-api's checked-in defaults ($/MTok).
// Cache reads are ~0.1x input, cache writes 1.25x — the ratios the real
// catalog carries, so mock USD figures look like production ones.
export const modelPricing: ModelPricingList = {
  defaultModel: "claude-fable-5",
  models: [
    {
      model: "claude-fable-5",
      displayName: "Claude Fable 5",
      inputPerMTok: 10,
      outputPerMTok: 50,
      cacheReadPerMTok: 1,
      cacheWritePerMTok: 12.5,
    },
    {
      model: "claude-opus-4-8",
      displayName: "Claude Opus 4.8",
      inputPerMTok: 5,
      outputPerMTok: 25,
      cacheReadPerMTok: 0.5,
      cacheWritePerMTok: 6.25,
    },
    {
      model: "claude-sonnet-5",
      displayName: "Claude Sonnet 5",
      inputPerMTok: 3,
      outputPerMTok: 15,
      cacheReadPerMTok: 0.3,
      cacheWritePerMTok: 3.75,
    },
    {
      model: "claude-haiku-4-5",
      displayName: "Claude Haiku 4.5",
      inputPerMTok: 1,
      outputPerMTok: 5,
      cacheReadPerMTok: 0.1,
      cacheWritePerMTok: 1.25,
    },
  ],
};

const FABLE = "claude-fable-5";

// Catalog-derived USD, exactly the read-time math aep-api does (ADR-0011).
function priceUsd(u: Omit<Usage, "costUsd" | "model">, model = FABLE): number {
  const p = modelPricing.models.find((m) => m.model === model);
  if (!p) return 0;
  const usd =
    (u.inputTokens * p.inputPerMTok +
      u.outputTokens * p.outputPerMTok +
      u.cacheReadTokens * p.cacheReadPerMTok +
      u.cacheCreationTokens * p.cacheWritePerMTok) /
    1_000_000;
  return Math.round(usd * 100) / 100;
}

// Fixture builder: tokens in, priced Usage out.
export function usage(
  inputTokens: number,
  outputTokens: number,
  cacheReadTokens: number,
  cacheCreationTokens: number,
  model = FABLE,
): Usage {
  const tokens = { inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens };
  return { ...tokens, model, costUsd: priceUsd(tokens, model) };
}

export const zeroUsage: Usage = usage(0, 0, 0, 0);

function sum(items: Usage[], model = FABLE): Usage {
  const totals = items.reduce(
    (acc, u) => ({
      inputTokens: acc.inputTokens + u.inputTokens,
      outputTokens: acc.outputTokens + u.outputTokens,
      cacheReadTokens: acc.cacheReadTokens + u.cacheReadTokens,
      cacheCreationTokens: acc.cacheCreationTokens + u.cacheCreationTokens,
    }),
    { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 },
  );
  return { ...totals, model, costUsd: priceUsd(totals, model) };
}

// Per-task actuals for the building/deployed scenarios — one coding run each,
// cache-heavy the way real agentic runs are. Keyed by issue number.
export const taskUsage: Record<number, Usage> = {
  9: usage(140_000, 88_000, 1_900_000, 260_000), // storefront shell (merged)
  10: usage(95_000, 41_000, 1_150_000, 180_000), // catalog CRUD (in progress, accruing)
  11: usage(210_000, 120_000, 2_600_000, 340_000), // orders payment (failed — burnt before failing)
  // 12 (pending) has no execution yet — exercises the absent-usage cell.
};

const specTurnsAll = usage(180_000, 96_000, 1_400_000, 240_000);
const specDraftCycle = usage(52_000, 27_000, 410_000, 68_000);
const buildingBuildUsage = sum([taskUsage[9]!, taskUsage[10]!, taskUsage[11]!]);
const doneBuildUsage = sum(Object.values(taskUsage));
const validationUsage = usage(48_000, 22_000, 520_000, 70_000);

const noUsageRollup: ProjectUsage = {
  spec: zeroUsage,
  build: zeroUsage,
  validation: zeroUsage,
  draftCycle: zeroUsage,
  total: zeroUsage,
  versions: null,
};

// Per-scenario rollups for get-project-usage, consistent with the tallies in
// fixtures/project.ts (building = v1 mid-build; deployed = v1 done + drifted
// spec so the draft cycle has fresh spend).
export const projectUsage: Record<
  Exclude<ProjectScenario, "error">,
  ProjectUsage
> = {
  fresh: noUsageRollup,
  spec: {
    spec: specTurnsAll,
    build: zeroUsage,
    validation: zeroUsage,
    draftCycle: specTurnsAll, // nothing published yet — the whole spend is the cycle
    total: specTurnsAll,
    versions: null,
  },
  "spec-failed": {
    spec: specDraftCycle,
    build: zeroUsage,
    validation: zeroUsage,
    draftCycle: specDraftCycle,
    total: specDraftCycle,
    versions: null,
  },
  building: {
    spec: specTurnsAll,
    build: buildingBuildUsage,
    validation: zeroUsage,
    draftCycle: zeroUsage, // v1 just published; no new spec turns since
    total: sum([specTurnsAll, buildingBuildUsage]),
    versions: [{ tag: "v1", usage: buildingBuildUsage }],
  },
  deploying: {
    spec: specTurnsAll,
    build: doneBuildUsage,
    validation: validationUsage,
    draftCycle: zeroUsage,
    total: sum([specTurnsAll, doneBuildUsage, validationUsage]),
    versions: [{ tag: "v1", usage: sum([doneBuildUsage, validationUsage]) }],
  },
  deployed: {
    spec: sum([specTurnsAll, specDraftCycle]),
    build: doneBuildUsage,
    validation: validationUsage,
    draftCycle: specDraftCycle, // specs/ drifted past v1 — the v1+ cycle
    total: sum([specTurnsAll, specDraftCycle, doneBuildUsage, validationUsage]),
    versions: [{ tag: "v1", usage: sum([doneBuildUsage, validationUsage]) }],
  },
  "deploy-failed": {
    spec: specTurnsAll,
    build: doneBuildUsage,
    validation: validationUsage,
    draftCycle: zeroUsage,
    total: sum([specTurnsAll, doneBuildUsage, validationUsage]),
    versions: [{ tag: "v1", usage: sum([doneBuildUsage, validationUsage]) }],
  },
  "repo-error": noUsageRollup,
};

// Build totals shared with fixtures/project.ts so the builds list agrees with
// the rollup above.
export const buildUsageByScenario = {
  running: buildingBuildUsage,
  completed: doneBuildUsage,
} as const;

const designEstimate: UsageEstimateOutputBody = {
  action: "design",
  estimate: {
    tokensMin: 420_000,
    tokensMax: 780_000,
    costUsdMin: 4.2,
    costUsdMax: 8.1,
    model: FABLE,
    sampleSize: 12,
    average: usage(60_000, 34_000, 430_000, 75_000),
  },
};

const buildEstimate: UsageEstimateOutputBody = {
  action: "build",
  estimate: {
    tokensMin: 3_600_000,
    tokensMax: 6_800_000,
    costUsdMin: 34,
    costUsdMax: 66,
    model: FABLE,
    sampleSize: 7,
    average: usage(480_000, 260_000, 3_900_000, 640_000),
  },
};

// Cold start (fresh/repo-error scenarios): no historical data → estimate is
// null and the console shows "no estimate yet", never a fabricated number.
export function estimateFor(
  s: Exclude<ProjectScenario, "error">,
  action: string | null,
): UsageEstimateOutputBody {
  const cold = s === "fresh" || s === "repo-error";
  if (action === "build") {
    return cold ? { action: "build", estimate: null } : buildEstimate;
  }
  return cold ? { action: "design", estimate: null } : designEstimate;
}
