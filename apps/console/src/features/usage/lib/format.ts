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

import type { components } from "../../../generated/aep-api";

export type Usage = components["schemas"]["Usage"];
export type ModelPricing = components["schemas"]["ModelPricing"];

// "12.3K" / "3.1M" — cost chips live in dense headers, so token counts are
// always abbreviated; the exact split lives in the breakdown tooltip.
export function formatTokens(n: number): string {
  if (n >= 1_000_000) {
    const m = n / 1_000_000;
    return `${m >= 10 ? m.toFixed(0) : m.toFixed(1)}M`;
  }
  if (n >= 1_000) {
    const k = n / 1_000;
    return `${k >= 10 ? k.toFixed(0) : k.toFixed(1)}K`;
  }
  return String(n);
}

export function formatUsd(n: number): string {
  if (n > 0 && n < 0.01) return "<$0.01";
  return `$${n.toFixed(2)}`;
}

// All four token kinds folded together — the number a chip shows; the
// breakdown tooltip explains why it can dwarf input+output (cache traffic).
export function totalTokens(u: Usage): number {
  return (
    u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
  );
}

// The comparator's what-if math: the SAME token mix priced at another model's
// rates. Deliberately naive (tokenizers differ; cheaper models may take more
// turns) — every surface showing it carries the approximation disclaimer.
export function repriceUsd(u: Usage, p: ModelPricing): number {
  return (
    (u.inputTokens * p.inputPerMTok +
      u.outputTokens * p.outputPerMTok +
      u.cacheReadTokens * p.cacheReadPerMTok +
      u.cacheCreationTokens * p.cacheWritePerMTok) /
    1_000_000
  );
}
