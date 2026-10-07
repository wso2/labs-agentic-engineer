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

import { useQueryClient } from "@tanstack/react-query";
import { PROTOTYPE_COMMAND, prototypeCommand } from "@aep/contracts/commands";
import type { ProjectChat } from "../agent-chat/chatStore";
import { canSend, chatStore, useProjectChat } from "../agent-chat/useProjectChat";
import type { PrototypeFeedback, TurnScope } from "../agent-chat/turnScope";
import { designKey } from "../design/api/designModel";
import { useChatPanel } from "../shell/chatPanel";

/** Why a turn cannot start now, in the words a disabled button shows; null when one can. */
function waitingFor(chat: ProjectChat): string | null {
  if (canSend(chat)) return null;
  if (chat.status === "error") return "The chat couldn't load. Reopen it to try again.";
  if (chat.status !== "ready") return "Loading the chat…";
  return "The agent is busy with a turn. This is available once it finishes.";
}

/**
 * The prototype's two agent turns: Make (or Update) prototype, and a review's
 * Send all. Each is a `/prototype` turn in the project's chat, as the design
 * turns are: the chat opens (that is where the agent says what it does), the
 * message goes, and the design data is read again once the server has the
 * turn (the Design and Prototype cards show it running) and again when it
 * ends (the shell's useRefreshOnTurnEnd, for every turn). While a turn runs
 * they wait, as the composer does.
 */
export function usePrototypeTurns(projectName: string): {
  /** Make or update one web application's prototype, or every one's (none named). */
  make: (component?: string) => void;
  /** Send a review's requests as one revision; resolves false when it was not sent. */
  sendFeedback: (feedback: PrototypeFeedback) => Promise<boolean>;
  /** Whether one can start now: the chat is loaded and no turn is running. */
  ready: boolean;
  /** Why one cannot start now; null when it can. */
  waiting: string | null;
} {
  const chat = useProjectChat(projectName);
  const panel = useChatPanel();
  const queryClient = useQueryClient();
  const send = async (line: string, scope: TurnScope) => {
    panel.open();
    const sent = await chatStore.send(projectName, line, scope);
    if (sent) void queryClient.invalidateQueries({ queryKey: designKey(projectName) });
    return sent;
  };
  return {
    ready: canSend(chat),
    waiting: waitingFor(chat),
    make: (component) => void send(component ? prototypeCommand(component) : PROTOTYPE_COMMAND, { kind: "prototype" }),
    sendFeedback: (feedback) => send(prototypeCommand(feedback.component), { kind: "prototype", feedback }),
  };
}
