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

import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { designCommand } from "@aep/contracts/commands";
import { canSend, chatStore, useProjectChat } from "../agent-chat/useProjectChat";
import { useSpecWorkspace } from "../spec/useSpecWorkspace";
import { useChatPanel } from "../shell/chatPanel";
import { designKey } from "./api/designModel";

/**
 * The design review's two agent turns, from wherever they are offered: the
 * design (Next up, the Design tab, the design card's header) and Address
 * comments. Each is a turn in the project's chat, scoped to the design
 * review: the design card opens (that is where the work shows), the chat
 * opens (that is where the agent says what it does), and the message goes.
 * While a turn runs they wait, as the composer does.
 */
export function useDesignTurns(projectName: string): {
  /** Send the design turn for the features that need it: `/design F1 F2`. */
  design: () => void;
  addressComments: (count: number) => void;
  /** Whether one can start now: the chat is loaded and no turn is running. */
  ready: boolean;
} {
  const chat = useProjectChat(projectName);
  const panel = useChatPanel();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const toDesign = useSpecWorkspace(projectName).workspace?.design.toDesign ?? [];

  const send = async (text: string) => {
    panel.open();
    const sent = await chatStore.send(projectName, text, { kind: "design" });
    // The design card shows the turn running as soon as the server has it.
    if (sent) void queryClient.invalidateQueries({ queryKey: designKey(projectName) });
  };

  return {
    ready: canSend(chat),
    design: () => {
      void navigate({ to: "/projects/$projectName/design", params: { projectName } });
      void send(designCommand(toDesign));
    },
    addressComments: (count) => void send(`Address comments · ${count}`),
  };
}
