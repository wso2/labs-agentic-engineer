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

import { apiErrorCode } from "../../../api/errors";

// aep-api reads the spec state, the versions and the reports through the
// org's AE Studio. When AE Studio cannot answer, the refusal's code says why,
// and each cause asks something different of the user.

/** Why AE Studio could not answer a read: restarting (wait), GitHub not connected (connect it), misconfigured (an administrator's fix). */
export type AeStudioUnavailable = "restarting" | "github" | "misconfigured";

const BY_CODE: Record<string, AeStudioUnavailable> = {
  ae_studio_unavailable: "restarting",
  github_not_connected: "github",
  ae_studio_misconfigured: "misconfigured",
};

/** The cause a failed read names, or null when it is not about AE Studio. */
export function aeStudioUnavailable(error: unknown): AeStudioUnavailable | null {
  const code = apiErrorCode(error);
  return (code && BY_CODE[code]) || null;
}

const RESTART_REFETCH_MS = 5_000;

/**
 * A query's `refetchInterval`, from its last error, for a read that says
 * "retrying…" while AE Studio restarts: once the query's own retries are spent
 * on a restart, it keeps reading every 5 s until AE Studio answers; any other
 * state reads as usual.
 */
export function refetchWhileAeStudioRestarts(lastError: unknown): number | false {
  return aeStudioUnavailable(lastError) === "restarting" ? RESTART_REFETCH_MS : false;
}
