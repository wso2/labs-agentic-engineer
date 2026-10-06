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

import { apiErrorCode } from "./errors";

/** aep-api's answer while the org's AE Studio comes up or rolls. */
const AE_STUDIO_UNAVAILABLE = "ae_studio_unavailable";

/** The wait when a 503 ae_studio_unavailable carries no Retry-After. */
const AE_STUDIO_UNAVAILABLE_RETRY_MS = 5_000;

function carriedRetryAfterMs(error: unknown): number | undefined {
  if (error && typeof error === "object") {
    const v = (error as Record<string, unknown>).retryAfterMs;
    if (typeof v === "number" && v > 0) return v;
  }
  return undefined;
}

/**
 * Query `retry`: an AE Studio that is restarting gets more patience (six
 * tries, ~30 s at its pace) than an ordinary failure (TanStack's three).
 */
export function queryRetry(failureCount: number, error: unknown): boolean {
  return apiErrorCode(error) === AE_STUDIO_UNAVAILABLE ? failureCount < 6 : failureCount < 3;
}

/**
 * Query `retryDelay`: an AE Studio restart waits what the server's
 * Retry-After says when the error carries it, else 5 s; anything else backs
 * off like TanStack's default.
 */
export function queryRetryDelay(failureCount: number, error: unknown): number {
  if (apiErrorCode(error) === AE_STUDIO_UNAVAILABLE) {
    return carriedRetryAfterMs(error) ?? AE_STUDIO_UNAVAILABLE_RETRY_MS;
  }
  return Math.min(1000 * 2 ** failureCount, 30_000);
}
