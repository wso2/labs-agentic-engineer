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

import { interviewCommand } from "@aep/contracts/commands";
import { useChatPanel } from "../shell/chatPanel";
import type { SpecFeature } from "../spec/api/specModel";
import { useOpenSpecTarget } from "../spec/useSpecWorkspace";
import { featureScope } from "./turnScope";
import { canSend, chatStore, useProjectChat } from "./useProjectChat";

/**
 * Start a feature's interview, from wherever it is offered: a stub's page,
 * Next up, or the chat's offer of the next feature. All three do the same
 * thing: open the feature's file (the scope is where the user is, so the
 * interview happens beside the document it fills), open the chat, and send
 * the request scoped to that feature. While a turn runs it waits, as the
 * composer does.
 */
export function useStartInterview(projectName: string): {
  start: (feature: Pick<SpecFeature, "id" | "name" | "path">) => void;
  /** Whether it can start now: the chat is loaded and no turn is running. */
  ready: boolean;
  /** A turn is running: the interview waits for it, as the composer does. */
  waiting: boolean;
} {
  const chat = useProjectChat(projectName);
  const panel = useChatPanel();
  const openTarget = useOpenSpecTarget(projectName);
  return {
    ready: canSend(chat),
    waiting: chat.turn.phase !== "idle",
    start: (feature) => {
      openTarget({ file: feature.id });
      panel.open();
      void chatStore.send(projectName, interviewCommand(feature.id), featureScope(feature));
    },
  };
}
