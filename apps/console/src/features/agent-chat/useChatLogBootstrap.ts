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

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  chatKeyFor,
  getMessages,
  hasChatLogOwner,
  replaceMessages,
} from "./chatStore.js";
import { getConversationMessages } from "./api/turns.js";
import { conversationKeys, fetchCurrentConversationId } from "./api/conversations.js";
import { projectableHistory } from "./history.js";

/** Matches useAgentChat's foreign-turn poll — the thread is server-driven, so
 *  every reader of it learns about a change at the same cadence. */
const THREAD_POLL_MS = 12_000;

/**
 * Keep the local chat log current from the project's server thread whenever no
 * chat panel is doing it (#485).
 *
 * The spec body's question form is fed by mirroring the chat LOG into the
 * collab room (useRoomQuestion) — but only useAgentChat writes that log, and it
 * exists only while the chat rail is OPEN (Collapse unmountOnExit). So the form
 * appeared only after the chat had been opened once, and — once the rail was
 * closed again — a question the agent asked in the meantime never arrived at
 * all: the log kept whatever it had, the room was never told, and the spec view
 * sat on its working state. SpecView mounts this hook so the question path
 * holds with the rail open, closed, or never opened.
 *
 * It stands down while a panel owns the log (`hasChatLogOwner`): that hook's
 * own rehydrate covers freshness, and overwriting a live fold mid-turn would
 * drop streamed partials. Both writers project the SAME server history through
 * `projectableHistory`, whose ids are position-stable, so handing the log back
 * and forth changes nothing on screen.
 */
export function useChatLogBootstrap(org: string, projectName: string): void {
  const chatKey = chatKeyFor(org, projectName);

  // Same cache entry useAgentChat resolves — an open panel or a prior visit
  // makes this free.
  const conversation = useQuery({
    queryKey: conversationKeys.current(projectName),
    queryFn: () => fetchCurrentConversationId(projectName),
    staleTime: Infinity,
    retry: false,
  });
  const conversationId = conversation.data;

  useEffect(() => {
    if (!conversationId) return;
    let cancelled = false;
    const pull = async () => {
      if (hasChatLogOwner(chatKey)) return;
      const history = await getConversationMessages(projectName, conversationId).catch(
        () => null, // best-effort — the next tick retries
      );
      // Re-check at write time: a panel may have attached (or started folding)
      // while this fetch was in flight — the live writer always wins.
      if (cancelled || !history || hasChatLogOwner(chatKey)) return;
      // LOCAL-ONLY rows survive the replace, exactly as useAgentChat's D6
      // rehydrate keeps them: a failed send's user row and its error row exist
      // nowhere server-side, and washing them out would destroy the only copy
      // of a message the user still needs to retry.
      const localOnly = getMessages(chatKey).filter(
        (m) => m.role === "error" || (m.role === "user" && m.status === "failed"),
      );
      replaceMessages(chatKey, [...projectableHistory(history), ...localOnly]);
    };
    void pull();
    const poll = setInterval(() => void pull(), THREAD_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(poll);
    };
  }, [conversationId, chatKey, projectName]);
}
