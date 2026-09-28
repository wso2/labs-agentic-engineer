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

type Usage = components["schemas"]["Usage"];
type PhaseUsage = components["schemas"]["PhaseUsage"];
type ProjectUsageCard = components["schemas"]["ProjectUsageCard"];
type ProjectUsageList = components["schemas"]["ProjectUsageList"];

const FABLE = "claude-fable-5";

// The active model's rates ($/MTok), mirroring the model_rates seed the #299
// backend ships. Cache reads are ~0.1x input, cache writes 1.25x, so mock USD
// figures look like production ones.
const RATES = {
  inputPerMTok: 10,
  outputPerMTok: 50,
  cacheReadPerMTok: 1,
  cacheWritePerMTok: 12.5,
};

// The stamp math the backend runs at capture time (amended ADR-0011): rates
// in force when the work ran; fixtures stamp once, never reprice.
function priceUsd(u: Omit<Usage, "costUsd" | "model">): number {
  const usd =
    (u.inputTokens * RATES.inputPerMTok +
      u.outputTokens * RATES.outputPerMTok +
      u.cacheReadTokens * RATES.cacheReadPerMTok +
      u.cacheCreationTokens * RATES.cacheWritePerMTok) /
    1_000_000;
  return Math.round(usd * 100) / 100;
}

// Fixture builder: tokens in, stamped Usage out.
export function usage(
  inputTokens: number,
  outputTokens: number,
  cacheReadTokens: number,
  cacheCreationTokens: number,
  model = FABLE,
): Usage {
  const tokens = { inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens };
  return { ...tokens, model, costUsd: priceUsd(tokens) };
}

// Pre-stamping rows (#291: no backfill): tokens exist, USD never will.
function unstamped(
  inputTokens: number,
  outputTokens: number,
  cacheReadTokens: number,
  cacheCreationTokens: number,
): Usage {
  return {
    inputTokens,
    outputTokens,
    cacheReadTokens,
    cacheCreationTokens,
    model: FABLE,
    costUsd: null,
  };
}

// Work billed by a host the platform holds no rate for (an open model on
// Ollama Cloud): tokens and the host, never a guessed dollar figure.
function unpriced(
  inputTokens: number,
  outputTokens: number,
  cacheReadTokens: number,
  cacheCreationTokens: number,
  model: string,
  host: string,
): Usage {
  return { inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, model, host, costUsd: null };
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
  return { ...totals, model, costUsd: priceUsd(totals) };
}

// Per-task actuals for the building/deployed scenarios — one coding run each,
// cache-heavy the way real agentic runs are. Keyed by issue number.
export const taskUsage: Record<number, Usage> = {
  9: usage(140_000, 88_000, 1_900_000, 260_000), // storefront shell (merged)
  10: usage(95_000, 41_000, 1_150_000, 180_000), // catalog CRUD (in progress, accruing)
  11: usage(210_000, 120_000, 2_600_000, 340_000), // orders payment (failed — burnt before failing)
  // 12 (pending) has no execution yet — exercises the absent-usage cell.
};

const doneBuildUsage = sum(Object.values(taskUsage));

// ---- Settings → Usage page (#291) -----------------------------------------

export type UsageScenario = "default" | "empty" | "error";

// A plausible mock split of a total across the three SDLC phases (#291):
// spec/design ~20%, build ~70%, validation ~10%. Tokens and cost scale
// together; a null (unpriced) total keeps null costs per phase.
function scale(u: Usage, f: number): Usage {
  return {
    inputTokens: Math.round(u.inputTokens * f),
    outputTokens: Math.round(u.outputTokens * f),
    cacheReadTokens: Math.round(u.cacheReadTokens * f),
    cacheCreationTokens: Math.round(u.cacheCreationTokens * f),
    model: u.model,
    ...(u.host !== undefined ? { host: u.host } : {}),
    costUsd: u.costUsd === null ? null : Math.round(u.costUsd * f * 100) / 100,
  };
}

function phasesOf(u: Usage): PhaseUsage {
  return { spec: scale(u, 0.2), build: scale(u, 0.7), validation: scale(u, 0.1) };
}

const card = (
  projectName: string,
  displayName: string,
  deleted: boolean,
  u: Usage,
): ProjectUsageCard => ({
  projectName,
  displayName,
  deleted,
  usage: u,
  phases: phasesOf(u),
});

// An idle live project: no agent has run yet, so a real $0 (not a null unpriced
// cost). The Usage page lists every live project, not only ones with spend.
function idle(): Usage {
  return {
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    model: "",
    costUsd: 0,
  };
}

// The org roll-up in the tiered order the backend emits: stamped-cost desc,
// then unpriced-but-active (tokens only), then idle $0 projects last. Covers:
// live projects with spend, a deleted project that kept its spend, a
// pre-stamping project (tokens, null cost), and a brand-new idle project.
export const orgUsage: Record<Exclude<UsageScenario, "error">, ProjectUsageList> = {
  default: {
    projects: [
      card(
        "storefront-webapp",
        "Storefront Webapp",
        false,
        sum([doneBuildUsage, usage(180_000, 96_000, 1_400_000, 240_000)]),
      ),
      card(
        "notification-hub",
        "Notification Hub",
        false,
        usage(160_000, 74_000, 1_100_000, 210_000),
      ),
      card(
        "legacy-crm-poc",
        "legacy-crm-poc",
        true, // project deleted; its spend remains — greyed card
        usage(120_000, 52_000, 700_000, 90_000),
      ),
      card(
        "basic-calculator-webapp",
        "Basic Calculator",
        false,
        usage(12_000, 4_500, 60_000, 9_000),
      ),
      card(
        "order-events",
        "Order Events",
        false,
        unpriced(410_000, 160_000, 760_000, 0, "kimi-k3", "ollama.com"),
      ),
      card(
        "spike-notifications",
        "Spike Notifications",
        false,
        unstamped(52_000, 27_000, 410_000, 68_000), // pre-v2 rows: tokens only
      ),
      card("fresh-idea", "Fresh Idea", false, idle()), // new project, $0
    ],
  },
  empty: { projects: [] },
};

export const usageLoadError = {
  code: "internal",
  message: "usage roll-up unavailable",
} satisfies components["schemas"]["Error"];
