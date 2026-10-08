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

import { useCallback, useMemo, useRef, useState } from "react";
import { chatViewFor } from "../agent-chat/chatView";
import type { ChatPanelControls, ComposeRequest, ComposeTarget } from "./chatPanel";
import type { ShellScope } from "./scope";

// What the shell keeps for the chat panel: whether it is open is the shell's
// own; here, the request to fill a composer. The panel holds the thread of
// the page in view, so a caller that moves the user to another thread
// navigates and names it; the request waits until that thread's composer is
// in the panel and applies it there.

export function useChatControls(scope: ShellScope, openChat: () => void) {
  const project = scope.kind === "project" ? scope : null;

  // The request lives here, not in the composer: the composer mounts when the
  // chat opens, after `compose` has been asked. It targets the chat the caller
  // names (a move to another page names where it is going), else the view and
  // project in focus when asked, and is cleared once a composer applies it.
  const [composeRequest, setComposeRequest] = useState<ComposeRequest | null>(null);
  const composeNonce = useRef(0);
  const inFocus = useRef<ComposeTarget | null>(null);
  inFocus.current = project
    ? {
        projectName: project.projectName,
        view: chatViewFor(project.page, project.card, project.issueNumber),
        ...(project.issueNumber !== null ? { issueNumber: project.issueNumber } : {}),
      }
    : null;
  const opener = useRef(openChat);
  opener.current = openChat;

  const controls = useMemo<ChatPanelControls>(
    () => ({
      open: () => opener.current(),
      compose: (text, explicit) => {
        const target = explicit ?? inFocus.current;
        if (target) setComposeRequest({ text, ...target, nonce: ++composeNonce.current });
        opener.current();
      },
    }),
    [],
  );
  const clearComposeRequest = useCallback(
    (nonce: number) => setComposeRequest((current) => (current?.nonce === nonce ? null : current)),
    [],
  );

  return { controls, composeRequest, clearComposeRequest };
}
