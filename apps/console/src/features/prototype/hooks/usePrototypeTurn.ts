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

import { useCallback, useSyncExternalStore } from "react";
import { PROTOTYPE_COMMAND } from "@aep/contracts/commands";
import { useSession } from "../../../auth/SessionContext";
import {
  chatKeyFor,
  getMessages,
  requestChatOpen,
  setPendingSeed,
  subscribe,
  type ChatMessage,
} from "../../agent-chat/chatStore";
import { answerableQuestionIds } from "../../agent-chat/questionCards";
import { agentEngaged } from "../../agent-chat/useAgentEngaged";
import { useConversationLog } from "../../agent-chat/useConversationLog";

export interface PrototypeTurn {
  /** Send `/prototype`. While blocked, opens the chat on the exchange that
   *  holds it instead — the panel would drop the seed, and a click must not
   *  vanish. */
  run: () => void;
  /** Why the turn cannot be sent right now, or "" when it can. */
  blockedReason: string;
}

/**
 * Why the panel would drop a guarded `/prototype` seed now, or "" when it would
 * send it: exactly `agentEngaged`, the panel's own backstop, worded for the
 * one of its two states the log is in.
 */
export function prototypeTurnGate(messages: ChatMessage[]): string {
  if (!agentEngaged(messages)) return "";
  return answerableQuestionIds(messages).size > 0
    ? "The agent is waiting on your answer in the chat — reply there first"
    : "An agent is still working — available once it finishes";
}

/**
 * Generate / Regenerate prototype (#813, #818): send `/prototype` as a flow
 * turn through the project chat's seed slot, which the agent panel consumes
 * and sends exactly once.
 *
 * GUARDED, like every injected flow command: nobody typed it, so the panel
 * drops it rather than send it into an exchange that is waiting on the user.
 * The hook reads the same log the panel decides from (and keeps it filled), so
 * its surfaces can disable the action with the reason instead of letting the
 * click be dropped.
 */
export function usePrototypeTurn(projectName: string): PrototypeTurn {
  const org = useSession().orgHandle ?? "default";
  useConversationLog(org, projectName);
  const chatKey = chatKeyFor(org, projectName);
  const blockedReason = useSyncExternalStore(
    useCallback((fn: () => void) => subscribe(chatKey, fn), [chatKey]),
    () => prototypeTurnGate(getMessages(chatKey)),
  );
  const run = useCallback(() => {
    if (prototypeTurnGate(getMessages(chatKey))) {
      requestChatOpen(chatKey);
      return;
    }
    setPendingSeed(chatKey, PROTOTYPE_COMMAND, true);
  }, [chatKey]);
  return { run, blockedReason };
}
