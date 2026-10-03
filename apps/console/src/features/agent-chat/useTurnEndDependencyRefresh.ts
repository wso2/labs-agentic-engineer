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

import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { hasDeterministicFlush, subscribeTurnEnd } from "./chatStore.js";
import { scheduleFreshnessPoll } from "../spec/api/dependencyFreshness.js";

/**
 * Universal turn-end freshness fallback (#252 Task 5 — see "Freshness" in
 * the feature design). Mounted alongside the chat panel (AgentChatPanel,
 * present on every project route via AppLayout), so it fires even when the
 * chat is used away from the Spec view — the only place with a live collab
 * connection (useTurnEndFlush) that can force a deterministic room flush.
 * Wherever the chat happens to be, this always runs a light
 * refetch-on-turn-done + a short poll so the dependencies/preflight reads
 * don't sit stale for the whole 60s debounce quiet period.
 *
 * Coordination (fix wave 1, Important #1): `notifyTurnEnd` dispatches
 * synchronously, but `useTurnEndFlush`'s connected branch invalidates only
 * after an async `collab.flush()` resolves. When both hooks are mounted for
 * the same chat key (chat open on the Spec route), this hook's immediate
 * invalidate used to race ahead of that flush and briefly show stale data.
 * `hasDeterministicFlush` lets this hook skip its own immediate invalidate
 * when a deterministic flush owner is live for the key — the delayed
 * backstop poll still runs regardless, in case that flush fails.
 */
export function useTurnEndDependencyRefresh(
  chatKey: string,
  projectName: string | undefined,
): void {
  const queryClient = useQueryClient();
  const cancelPollRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    // The marketplace chat has no project whose dependencies could go stale.
    if (projectName === undefined) return;
    const unsubscribe = subscribeTurnEnd(chatKey, () => {
      cancelPollRef.current?.();
      cancelPollRef.current = scheduleFreshnessPoll(
        queryClient,
        projectName,
        undefined,
        { immediate: !hasDeterministicFlush(chatKey) },
      );
    });
    return () => {
      unsubscribe();
      cancelPollRef.current?.();
      cancelPollRef.current = null;
    };
  }, [chatKey, projectName, queryClient]);
}
