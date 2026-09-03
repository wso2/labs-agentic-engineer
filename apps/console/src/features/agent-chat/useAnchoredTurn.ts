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

import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  addMessage,
  chatKeyFor,
  claimSendInFlight,
  claimStreamFold,
  clearFailedSends,
  hasStreamFold,
  requestChatOpen,
  setTurnStatus,
  settleUserMessage,
} from "./chatStore.js";
import { attachAndFoldTurn } from "./runTurn.js";
import { conversationKeys, fetchCurrentConversationId } from "./api/conversations.js";
import {
  ConversationRotatedError,
  startCollabTurn,
  type TurnAiming,
} from "./api/turns.js";
import { useCurrentAuthor } from "./currentUser.js";

// A turn fired from a DOCUMENT rather than from the chat composer (#666).
//
// Why this is not `useAgentChat().send`: the chat panel is mounted with
// `unmountOnExit` (AppLayout), so when it is closed that hook does not exist —
// and a Change is specifically the send that does NOT open it.
//
// It folds its turn's stream, exactly as the panel's send does. The first cut
// skipped the fold, and the user watched the tag on their message blink: the
// optimistic row is the only copy of the message until the server journals the
// turn AT ITS END, and with no fold claim held, any rehydrate in that window —
// a refocus, the panel opening — replaced the log with server truth that did
// not hold the row yet, then turn-end brought it back. The fold is what makes
// the row stable: a live fold blocks the replace for the whole turn, and it
// writes the agent's narration into the log on the way.
//
// "Quietly" means the panel does not take over the screen, not that the turn
// goes unrecorded.

export interface AnchoredTurn {
  /** False when the send was refused before dispatch — no thread resolved yet,
   *  or empty text. The caller keeps the user's words in that case. */
  send: (instruction: string, aiming: TurnAiming) => Promise<boolean>;
  /** False until the project's thread is known; nothing can be sent before it. */
  ready: boolean;
}

export function useAnchoredTurn(
  org: string,
  projectName: string | undefined,
): AnchoredTurn {
  const queryClient = useQueryClient();
  const author = useCurrentAuthor();
  // The same query the panel uses, by the same key — so opening the panel after
  // an anchored send finds the thread already resolved in cache rather than
  // re-asking for it.
  const conversation = useQuery({
    queryKey: conversationKeys.current(projectName ?? ""),
    queryFn: () => fetchCurrentConversationId(projectName!),
    enabled: Boolean(projectName),
    staleTime: Infinity,
  });
  const conversationId = conversation.data;

  const send = useCallback(
    async (instruction: string, aiming: TurnAiming): Promise<boolean> => {
      const text = instruction.trim();
      if (!text || !projectName || !conversationId) return false;
      const chatKey = chatKeyFor(org, projectName);

      // Discuss opens the panel; Change never does. Opened BEFORE the dispatch
      // resolves, so the user asking for a conversation gets one immediately
      // rather than after a round trip they cannot see.
      if (aiming.intent === "discuss") requestChatOpen(chatKey);

      // The row goes up now, carrying the anchor, so the log says what was
      // aimed at even though nothing on screen moved.
      const messageId = addMessage(chatKey, {
        role: "user",
        content: text,
        status: "in_flight",
        author,
        createdAt: Date.now(),
        anchor: aiming.anchor,
      });
      // Claim the log for the dispatch. Without this a rehydrate — the panel
      // mounting on a Discuss is exactly one — REPLACES the log with server
      // truth, and this row is not in it yet: the user's own message would
      // vanish inside the second it takes to answer.
      const release = claimSendInFlight(chatKey);
      try {
        const turnId = await startCollabTurn(
          projectName,
          conversationId,
          text,
          [],
          true,
          aiming,
        );
        clearFailedSends(chatKey);
        settleUserMessage(chatKey, messageId, { turnId });
        // Fold the turn to its terminal, detached — unless a fold is already
        // live for this log (the panel attached first), because two folds of
        // one turn would interleave the stream on top of itself. The panel's
        // own attach paths make the same check in the other direction.
        if (!hasStreamFold(chatKey)) {
          const releaseFold = claimStreamFold(chatKey);
          void attachAndFoldTurn(chatKey, projectName, turnId, new AbortController().signal)
            .catch(() => {
              setTurnStatus(chatKey, turnId, "failed");
              addMessage(chatKey, {
                role: "error",
                content: "Lost the agent's stream — open the panel to re-attach.",
              });
            })
            .finally(releaseFold);
        }
        return true;
      } catch (err) {
        // The row the user can already see becomes the failed one — a second
        // copy beside it would read as two sends.
        settleUserMessage(chatKey, messageId, { failed: true });
        addMessage(chatKey, {
          role: "error",
          content: err instanceof Error ? err.message : "Failed to reach the agent.",
        });
        // The box closed the moment they sent (#666), so the log is the only
        // place this failure exists — open the panel onto it. A failure that
        // lands somewhere closed is a message that silently vanished.
        requestChatOpen(chatKey);
        if (err instanceof ConversationRotatedError) {
          void queryClient.invalidateQueries({
            queryKey: conversationKeys.current(projectName),
          });
        }
        return false;
      } finally {
        release();
      }
    },
    [org, projectName, conversationId, author, queryClient],
  );

  return { send, ready: Boolean(conversationId) };
}
