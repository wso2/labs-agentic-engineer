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

import { useCallback, useEffect, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { chatKeyFor, getMessages, replaceMessages, subscribe } from "./chatStore.js";
import { getConversationMessages } from "./api/turns.js";
import { conversationKeys, fetchCurrentConversationId } from "./api/conversations.js";
import { projectableHistory } from "./history.js";

/**
 * Seed an EMPTY local chat log from the project's server thread (#485
 * live-testing round). The spec body's question form is fed by mirroring the
 * chat LOG into the collab room (useRoomQuestion) — but only useAgentChat
 * rehydrated the log, so on a fresh browser the form appeared only after the
 * chat panel had been opened once. SpecView mounts this hook so a pending
 * question surfaces on arrival, chat rail open or not.
 *
 * One-shot by design: a non-empty log means either a previous session's cache
 * (already mirrorable) or an attached panel actively folding — both supersede
 * this. The panel's own D6 rehydrate later REPLACES the log with the same
 * position-stable rows, so the two writers converge instead of fighting.
 */
export function useChatLogBootstrap(org: string, projectName: string): void {
  const chatKey = chatKeyFor(org, projectName);
  const empty = useSyncExternalStore(
    useCallback((fn: () => void) => subscribe(chatKey, fn), [chatKey]),
    () => getMessages(chatKey).length === 0,
  );

  // Same cache entry useAgentChat resolves — an open panel or a prior visit
  // makes this free.
  const conversation = useQuery({
    queryKey: conversationKeys.current(projectName),
    queryFn: () => fetchCurrentConversationId(projectName),
    staleTime: Infinity,
    enabled: empty,
    retry: false,
  });
  const conversationId = conversation.data;

  useEffect(() => {
    if (!empty || !conversationId) return;
    let cancelled = false;
    void getConversationMessages(projectName, conversationId)
      .then((history) => {
        if (cancelled || !history) return;
        // Re-check at write time: an attached panel may have started folding
        // (or its rehydrate landed) while this fetch was in flight — the live
        // log always wins over a bootstrap.
        if (getMessages(chatKey).length > 0) return;
        replaceMessages(chatKey, projectableHistory(history));
      })
      .catch(() => undefined); // best-effort — the panel's rehydrate remains
    return () => {
      cancelled = true;
    };
  }, [empty, conversationId, chatKey, projectName]);
}
