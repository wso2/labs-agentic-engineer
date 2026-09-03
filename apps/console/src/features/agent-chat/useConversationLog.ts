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

// Filling this browser's chat log from server truth (#606).
//
// Extracted from `useAgentChat`, where it used to run only while the chat panel
// was mounted. That made `chatStore` mean "what the panel has seen", and every
// surface reading it inherited that: a member who opened a spec link without the
// panel had NO log, so `useRoomQuestion` had nothing to mirror into the room and
// the workspace showed "Nothing written yet" while the agent stood waiting on
// three questions. The overview's spec card read "nothing has started" for the
// same reason.
//
// `chatStore` now means "this browser's view of the project conversation", and
// any surface that needs one fills it. The chat panel still folds live turns —
// that is its own job and stays there; this is only the rehydrate.
//
// It is a REPLACE, not a merge, for the reason it always was (#430 D6): a
// project-scoped thread must show a teammate's messages, and a merge cannot
// remove what the server no longer has.

import { useCallback, useEffect } from "react";
import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
  canReplaceLog,
  chatKeyFor,
  getMessages,
  replaceMessages,
} from "./chatStore.js";
import { projectableHistory } from "./history.js";
import { rehydratePlanFromHistory } from "./planStore.js";
import { conversationKeys, fetchCurrentConversationId } from "./api/conversations.js";
import { getConversationMessages, type ConversationMessage } from "./api/turns.js";

/**
 * Fetch the thread's history THROUGH the shared cache entry.
 *
 * Exported for `useAgentChat`, whose rehydrate is imperative (it runs before an
 * attach, and on its own poll) rather than observer-driven. Routing it here
 * rather than calling `getConversationMessages` directly is what makes the
 * panel and the non-panel surfaces share one in-flight request instead of
 * racing two.
 *
 * `staleTime: 0` because every caller here is a freshness trigger: they ask
 * precisely when they have reason to believe the answer moved.
 */
export function fetchConversationHistory(
  queryClient: QueryClient,
  projectName: string,
  conversationId: string,
): Promise<ConversationMessage[] | null> {
  return queryClient.fetchQuery({
    queryKey: conversationKeys.messages(projectName, conversationId),
    queryFn: () => getConversationMessages(projectName, conversationId),
    staleTime: 0,
  });
}

/**
 * Write server history into `chatKey`'s log, keeping the rows the server has
 * no record of.
 *
 * LOCAL-ONLY rows survive: a failed send's user row (the typed text) and its
 * error row exist nowhere server-side, and washing them out would destroy the
 * one copy of a message the user still needs to retry. They are cleared by the
 * next successful send or by a rotation.
 *
 * `null` means the read FAILED — keep painting the local cache. "This thread is
 * empty" never arrives as null (see `getConversationMessages`).
 */
export function applyConversationHistory(
  chatKey: string,
  history: ConversationMessage[] | null,
): void {
  if (history === null) return;
  if (!canReplaceLog(chatKey)) return;
  const localOnly = getMessages(chatKey).filter(
    (m) => m.role === "error" || (m.role === "user" && m.status === "failed"),
  );
  replaceMessages(chatKey, [...projectableHistory(history), ...localOnly]);
  // The declared plan's residue rehydrates from the same replayed history the
  // question cards do (#576 decision 6) — a no-op while a live fold owns it.
  rehydratePlanFromHistory(chatKey, history);
}

export interface ConversationLog {
  /** True once the server has been ASKED — not "the read succeeded". Gates the
   *  guarded-seed backstop, which needs the exchange to be knowable. */
  historyReady: boolean;
  /** Re-read the thread now. For triggers the query cannot see for itself —
   *  chiefly the agent peer leaving the collab room, i.e. a turn just ended. */
  resync: () => void;
}

/**
 * Keep this browser's log for `(org, projectName)` in step with the server.
 *
 * Mount it on any surface that reads the log. Mounting it more than once is
 * free: the surfaces share one cache entry, so the extra readers cost no extra
 * request.
 *
 * Refreshes on mount and on tab refocus. Callers with a sharper signal call
 * `resync` — `SpecView` does, when the agent peer leaves the room, which is
 * turn-end observed without polling.
 */
export function useConversationLog(
  org: string,
  projectName: string | undefined,
): ConversationLog {
  const queryClient = useQueryClient();
  const chatKey = projectName ? chatKeyFor(org, projectName) : null;

  // Same query as `useAgentChat`'s, so the id resolves once per project however
  // many surfaces are mounted. `refetchOnWindowFocus: "always"` is the recovery
  // path when the resolve failed outright: with no id there is nothing to read.
  const conversation = useQuery({
    queryKey: conversationKeys.current(projectName ?? ""),
    queryFn: () => fetchCurrentConversationId(projectName!),
    enabled: Boolean(projectName),
    staleTime: Infinity,
    refetchOnWindowFocus: "always",
  });
  const conversationId = conversation.data;

  const history = useQuery({
    queryKey: conversationKeys.messages(projectName ?? "", conversationId ?? ""),
    queryFn: () => getConversationMessages(projectName!, conversationId!),
    enabled: Boolean(projectName && conversationId),
    // The thread only moves when a turn ends, and every surface that mounts
    // this has a trigger for that. A time-based staleness would refetch on
    // every remount of a workspace the user is tabbing around in.
    staleTime: Infinity,
    refetchOnWindowFocus: "always",
  });

  // Apply on every settled read — including a repeat of identical data, which
  // costs one `replaceMessages` and keeps the rule in one place rather than
  // splitting "did it change" between here and the store.
  const data = history.data;
  useEffect(() => {
    if (!chatKey || data === undefined) return;
    applyConversationHistory(chatKey, data);
  }, [chatKey, data]);

  const resync = useCallback(() => {
    if (!projectName || !conversationId) return;
    void queryClient.invalidateQueries({
      queryKey: conversationKeys.messages(projectName, conversationId),
    });
  }, [queryClient, projectName, conversationId]);

  return {
    // `isFetched` rather than `isSuccess`: a read that FAILED still answers
    // "we have asked", and holding a guarded seed forever on a history the
    // server will not serve would strand the only recovery a stalled project
    // has.
    historyReady: Boolean(conversationId) && history.isFetched,
    resync,
  };
}
