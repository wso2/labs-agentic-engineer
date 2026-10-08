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

import { http, HttpResponse } from "msw";
import type { components } from "../../generated/aep-api";

type ProjectUsageCard = components["schemas"]["ProjectUsageCard"];
type Usage = components["schemas"]["Usage"];

// Settings, Usage in mock mode: a fixed roll-up with one of each kind of row
// (priced, unpriced with its host, idle, deleted), in the server's order.

const zero: Usage = { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0, model: "", costUsd: 0 };

function spend(costUsd: number | null, tokens: number, over: Partial<Usage> = {}): Usage {
  return {
    inputTokens: Math.round(tokens * 0.08),
    outputTokens: Math.round(tokens * 0.04),
    cacheReadTokens: Math.round(tokens * 0.76),
    cacheCreationTokens: Math.round(tokens * 0.12),
    model: "claude-sonnet-4-5",
    costUsd,
    ...over,
  };
}

function row(projectName: string, displayName: string, usage: Usage, deleted = false): ProjectUsageCard {
  const part = (share: number): Usage => ({
    ...usage,
    inputTokens: Math.round(usage.inputTokens * share),
    outputTokens: Math.round(usage.outputTokens * share),
    cacheReadTokens: Math.round(usage.cacheReadTokens * share),
    cacheCreationTokens: Math.round(usage.cacheCreationTokens * share),
    costUsd: usage.costUsd === null ? null : Math.round(usage.costUsd * share * 100) / 100,
  });
  return { projectName, displayName, deleted, usage, phases: { spec: part(0.2), build: part(0.7), validation: part(0.1) } };
}

const projects: ProjectUsageCard[] = [
  row("acme-expenses", "Acme Expenses", spend(18.42, 4_100_000)),
  row("equipment-loans", "Equipment loans", spend(6.07, 1_300_000)),
  row("legacy-crm-poc", "legacy-crm-poc", spend(2.9, 600_000), true),
  row("order-events", "Order events", spend(null, 900_000, { model: "kimi-k3", host: "ollama.com" })),
  row("track-each-hire", "Track each hire", zero),
];

export const usageHandlers = [http.get("*/api/v1/usage/projects", () => HttpResponse.json({ projects }))];
