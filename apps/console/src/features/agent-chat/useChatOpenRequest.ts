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
import { chatKeyFor, getChatOpenRequests, subscribeChatOpen } from "./chatStore.js";

/**
 * The project's monotonic open-request counter (#485 live-testing round).
 * AppLayout opens the chat panel whenever it advances — the same reactive
 * open pattern as useHasPendingSeed, for surfaces (SpecView's first-run
 * arrival) that own no reference to the panel's open state. A counter, not a
 * flag: a later request must re-open even after the user closed the panel.
 */
export function useChatOpenRequest(
  org: string,
  projectName: string | undefined,
): number {
  const chatKey = projectName ? chatKeyFor(org, projectName) : null;
  return useSyncExternalStore(
    useCallback(
      (fn: () => void) => (chatKey ? subscribeChatOpen(chatKey, fn) : () => {}),
      [chatKey],
    ),
    () => (chatKey ? getChatOpenRequests(chatKey) : 0),
  );
}
