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
import { chatKeyFor, peekChatOpenRequest, subscribeChatOpen } from "./chatStore.js";

/**
 * How many times the project's chat panel has been asked for (#666).
 *
 * Monotonic: the VALUE is meaningless, a CHANGE is the signal. AppLayout owns
 * `chatOpen`, and an anchored Discuss is fired from the spec document — a
 * subtree that does not share that state — so the panel has to open reactively
 * from a store signal, the same way it already does for a pending seed.
 *
 * Distinct from `useHasPendingSeed` because a Discuss has ALREADY sent its
 * turn, anchor attached. A seed would send its text a second time.
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
    () => (chatKey ? peekChatOpenRequest(chatKey) : 0),
  );
}
