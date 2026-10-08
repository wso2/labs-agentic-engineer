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

type AeStudio = components["schemas"]["AeStudio"];

// aep-api answers `failed` with reason `timeout` when AE Studio has not become
// ready within its bound. The platform cannot see why; the usual cause is a
// cluster without room for the pod, which frees up by itself. So the copy
// hedges the cause, and the console keeps reading until AE Studio is ready.
// Any other failure (reason `error`, or none from an older server) is the
// plain "couldn't start".

/** The copy for a start that is taking longer than usual (lexicon, AE Studio). */
export const SLOW_START_TITLE = "Starting AE Studio is taking longer than usual.";
export const SLOW_START_DETAIL =
  "This often means the cluster is short on room. It keeps trying by itself; if this lasts, contact your platform administrator.";

/** How often a slow start is re-read, so the page turns ready by itself. */
export const SLOW_START_REFETCH_MS = 30_000;

/** Whether an answer is a start taking longer than usual. */
export function isSlowStart(studio: AeStudio | undefined): boolean {
  return studio?.state === "failed" && studio.reason === "timeout";
}
