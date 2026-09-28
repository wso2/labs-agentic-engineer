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

// PROVIDER LIMITS: when a model provider's 429 is a wait, and when it is the
// end of the run.
//
// A 429 means one of two things — wait a little (a rate or concurrency limit),
// or the plan is spent until it resets — and the status alone cannot tell them
// apart (ADR-0038 §8). ONE pure rule decides (`providerLimit`), and a per-run
// tracker feeds it from the retries the runtime reports.
//
// The runner sees no HTTP response: the runtime makes the calls. What reaches
// it is the runtime's retry report (`ApiRetryInfo`): the delay it derived from
// `retry-after` / `retry-after-ms` (OpenCode's `next`, Claude Code's
// `retry_delay_ms`), the status when it knows one, and the provider's text. An
// `x-ratelimit-reset*` header never gets this far, so a reset time is only ever
// the runtime's own delay.
//
// The five-minute total is per STREAK: a streak starts at the first 429 and
// ends when the model answers again (`progress`). A long run that meets a short
// rate limit twice an hour apart has not waited on its provider for the sum of
// both.

import type { ApiRetryInfo } from "../runtime/port.js";

/** A retry delay, or a streak of 429 retries, this long or longer is a provider limit. */
export const PROVIDER_LIMIT_MS = 5 * 60_000;

/** How much of the provider's own text rides a `run_settled` (the contract's cap). */
export const PROVIDER_DETAIL_MAX = 300;

export type ProviderLimitVerdict = "wait" | "provider_limit";

/**
 * The rule, and all of it.
 *
 * `retryAfterMs` is the delay the provider asked for — null when it stated
 * none. `waitedMs` is how long this streak of 429 retries has run.
 */
export function providerLimit(retryAfterMs: number | null, waitedMs: number): ProviderLimitVerdict {
  if (retryAfterMs !== null && retryAfterMs >= PROVIDER_LIMIT_MS) return "provider_limit";
  if (waitedMs >= PROVIDER_LIMIT_MS) return "provider_limit";
  return "wait";
}

/** One 429 the runtime reported, with the rule's verdict on it. */
export interface ProviderLimitHit {
  verdict: ProviderLimitVerdict;
  /** How long this streak of 429 retries has run, this one included. */
  waitedMs: number;
  /** The HTTP status when the runtime knew it; a rate-limit class without one is still a 429. */
  status: number | null;
  /** The runtime's retry delay, which is what it read off the provider's headers. */
  retryDelayMs: number;
  /**
   * When the provider said the limit resets (ISO 8601): only for a delay long
   * enough to BE a provider limit. A 20-second backoff is not a reset time, and
   * printing one would promise the reader something nobody said.
   */
  resetAt?: string;
  /** The provider's words on this 429, capped. Not scrubbed here: every sink scrubs on the way out. */
  detail: string;
}

/** One run's view of its provider's 429s. Stateful, per run, like the classifier. */
export interface ProviderLimits {
  /** The host the run's connection names — what the reader's sentence says ran out. */
  readonly host: string;
  /**
   * One retry the runtime reported. A 429 comes back with its verdict; any
   * other retry (an overload, a 5xx, a refused connection) is undefined and
   * leaves the streak as it was — it is neither a limit nor progress.
   */
  observe(info: ApiRetryInfo): ProviderLimitHit | undefined;
  /** The model answered: whatever streak of 429s there was is over. */
  progress(): void;
  /**
   * The last 429 of a streak still open, or undefined. What a run that ENDS
   * mid-streak settles with: a runtime that gives up on a rate limit before
   * the rule trips (Claude Code's retry ceiling is about a minute) has still
   * been stopped by its provider.
   */
  open(): ProviderLimitHit | undefined;
}

export interface ProviderLimitsOptions {
  host: string;
  /** The clock a streak is measured on; tests pin it. */
  now?: () => number;
}

export function createProviderLimits(opts: ProviderLimitsOptions): ProviderLimits {
  const now = opts.now ?? Date.now;
  let streakStart: number | undefined;
  let last: ProviderLimitHit | undefined;

  return {
    host: opts.host,
    observe(info) {
      if (!isRateLimit(info)) return undefined;
      const at = now();
      streakStart ??= at;
      // Zero is what a runtime reports when it stated no delay (OpenCode's
      // status with no `next`); it is not a provider asking for none.
      const retryAfterMs = info.retryDelayMs > 0 ? info.retryDelayMs : null;
      const waitedMs = at - streakStart;
      const verdict = providerLimit(retryAfterMs, waitedMs);
      last = {
        verdict,
        waitedMs,
        status: info.errorStatus,
        retryDelayMs: info.retryDelayMs,
        ...(retryAfterMs !== null && retryAfterMs >= PROVIDER_LIMIT_MS
          ? { resetAt: new Date(at + retryAfterMs).toISOString() }
          : {}),
        detail: cap(info.providerText ?? ""),
      };
      return last;
    },
    progress() {
      streakStart = undefined;
      last = undefined;
    },
    open() {
      return last;
    },
  };
}

/**
 * A 429 by status, or by class when the runtime did not know the status —
 * OpenCode reads its status out of the provider's message, which often reads
 * "Too Many Requests" with no number in it.
 */
function isRateLimit(info: ApiRetryInfo): boolean {
  return info.errorStatus === 429 || info.error === "rate_limit";
}

function cap(text: string): string {
  const collapsed = text.replace(/\s+/g, " ").trim();
  return collapsed.length <= PROVIDER_DETAIL_MAX ? collapsed : collapsed.slice(0, PROVIDER_DETAIL_MAX - 1) + "…";
}
