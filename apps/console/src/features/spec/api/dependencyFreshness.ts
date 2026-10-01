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

// Turn-end freshness (#252 Task 5 — closes the room→git race described in
// the feature's "Freshness" design note). A resolution edit lands in the Yjs
// room instantly but only reaches git HEAD on the collab service's debounced
// committer (components/dataplane/ae-system-project/ae-studio/ae-collab/src/env.ts: `commitDebounceMs`, default 60s
// quiet period) — the dependencies/preflight reads are git-backed, so they
// can lag a running chat turn by up to that long.
//
// `invalidateDependencyFreshness` marks both reads stale immediately, for the
// deterministic path (useTurnEndFlush, after a successful forced room flush).
// `scheduleFreshnessPoll` is the accepted-residual fallback (brief: "refetch-
// on-turn-done + a short poll") for when a deterministic flush isn't
// available (flush error, or no live room — e.g. the chat panel is open away
// from the Spec view): invalidate now, then once more after the quiet period
// so a commit that lands just after the first invalidate is still caught,
// with no new backend endpoint and no sleeps in the caller.

import type { QueryClient } from "@tanstack/react-query";
import { projectKeys } from "../../projects/api/keys";
import { specKeys } from "./keys";

export const FRESHNESS_POLL_DELAY_MS = 65_000; // quiet period (60s) + slack

export function invalidateDependencyFreshness(
  queryClient: QueryClient,
  projectName: string,
): void {
  void queryClient.invalidateQueries({
    queryKey: specKeys.dependencies(projectName),
  });
  void queryClient.invalidateQueries({
    queryKey: projectKeys.buildPreflight(projectName),
  });
}

/**
 * Invalidate now, then once more after `delayMs` (default: the committer's
 * quiet period + slack). Returns a cancel function that clears the pending
 * follow-up — callers should call it on unmount / before scheduling another
 * poll, so timers don't pile up across a long chat session.
 *
 * `immediate: false` (fix wave 1, Important #1) skips the up-front
 * invalidate and only arms the delayed follow-up — for
 * `useTurnEndDependencyRefresh` when a deterministic flush owner
 * (`useTurnEndFlush`) is live for the same chat key and will invalidate
 * itself after its own forced room flush; this hook's delayed backstop still
 * runs regardless, in case that flush fails.
 */
export function scheduleFreshnessPoll(
  queryClient: QueryClient,
  projectName: string,
  delayMs: number = FRESHNESS_POLL_DELAY_MS,
  { immediate = true }: { immediate?: boolean } = {},
): () => void {
  if (immediate) invalidateDependencyFreshness(queryClient, projectName);
  const timer = setTimeout(
    () => invalidateDependencyFreshness(queryClient, projectName),
    delayMs,
  );
  return () => clearTimeout(timer);
}
