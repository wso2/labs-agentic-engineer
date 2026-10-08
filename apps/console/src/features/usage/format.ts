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

export type Usage = components["schemas"]["Usage"];
export type PhaseUsage = components["schemas"]["PhaseUsage"];

/** A token count, abbreviated: "950", "12K", "3.1M". The exact split is the breakdown's. */
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

/**
 * A spend in USD, written to be read: "<$0.01" for dust, whole dollars when
 * the cents are zero ("$34", not "$34.00"), cents kept when they say
 * something ("$6.40").
 */
export function formatUsd(n: number): string {
  if (n > 0 && n < 0.01) return "<$0.01";
  return `$${n.toFixed(2).replace(/\.00$/, "")}`;
}

/** All four kinds of token together; cache traffic is why it dwarfs input plus output. */
export function totalTokens(u: Usage): number {
  return u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens;
}

/**
 * Why a figure has no dollars: who billed the work the platform could not
 * price. Null when it is priced, or when the host is unknown ("" on a total
 * that mixes hosts or predates the stamp).
 */
export function unpricedHostLine(u: Usage): string | null {
  if (u.costUsd !== null || !u.host) return null;
  return `not priced · billed by ${u.host}`;
}
