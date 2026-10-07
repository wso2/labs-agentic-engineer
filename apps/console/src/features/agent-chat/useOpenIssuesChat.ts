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
import { useNavigate } from "@tanstack/react-router";
import { useChatPanel } from "../shell/chatPanel";

/**
 * Go to the Issues page and start its chat there, over the main chat: what
 * Reopen, the threads menu and a hand-off's Open do. Rejects, with the branch
 * left as it was, when the move does not happen.
 */
export function useOpenIssuesChat(projectName: string): () => Promise<void> {
  const navigate = useNavigate();
  const chatPanel = useChatPanel();
  return useCallback(async () => {
    await navigate({ to: "/projects/$projectName/issues", params: { projectName } });
    chatPanel.startBranch("issues", projectName);
  }, [navigate, chatPanel, projectName]);
}
