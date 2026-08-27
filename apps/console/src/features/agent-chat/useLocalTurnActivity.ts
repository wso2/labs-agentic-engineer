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
import { chatKeyFor, hasLocalTurnActivity, subscribeLocalTurnActivity } from "./chatStore.js";

/**
 * True while THIS browser holds live evidence of a turn for the project — a
 * seeded message waiting to send, a dispatch awaiting its turn id, or a
 * stream being folded (#635). Covers the window where `spec.agent` still
 * reads idle because the turn's row does not exist server-side yet, which is
 * exactly when a surface deciding "is anything running" from status alone
 * would wrongly offer Retry against work in flight.
 */
export function useLocalTurnActivity(
  org: string,
  projectName: string | undefined,
): boolean {
  const chatKey = projectName ? chatKeyFor(org, projectName) : null;
  return useSyncExternalStore(
    useCallback(
      (fn: () => void) => (chatKey ? subscribeLocalTurnActivity(chatKey, fn) : () => {}),
      [chatKey],
    ),
    () => (chatKey ? hasLocalTurnActivity(chatKey) : false),
  );
}
