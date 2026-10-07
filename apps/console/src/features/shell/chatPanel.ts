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

import { createContext, useContext } from "react";
import type { ChatView } from "../agent-chat/chatView";

// What a page inside a project can ask of the chat panel beside it. The shell
// owns whether the panel is open; a page only asks.

export interface ChatPanelControls {
  /** Open the chat, at phone width too: something the user started is happening there. */
  open: () => void;
  /**
   * Open the chat and put `text` in the composer of the view in focus, focused
   * with the cursor at the end. Nothing is sent: the user finishes the message.
   */
  compose: (text: string) => void;
}

/**
 * A request to fill one composer: the chat of `view` in `projectName`, nobody
 * else's. Single-use: the composer that applies it reports the nonce and the
 * shell clears it, so it is not re-applied when the chat is reopened.
 */
export interface ComposeRequest {
  text: string;
  view: ChatView;
  projectName: string;
  nonce: number;
}

export const ChatPanelContext = createContext<ChatPanelControls | null>(null);

export function useChatPanel(): ChatPanelControls {
  const controls = useContext(ChatPanelContext);
  if (!controls) throw new Error("useChatPanel outside the shell");
  return controls;
}
