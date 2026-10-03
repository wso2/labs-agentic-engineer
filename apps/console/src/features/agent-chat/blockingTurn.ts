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

// A send the pod refused because a turn is already running (409
// turn_in_progress): who is running, so the console can attach to it instead of
// reporting a failure. Shared by the chat's send and the anchored send.

import type { QueryClient } from "@tanstack/react-query";
import { projectKeys } from "../projects/api/keys.js";
import type { ChatScope } from "./chatScope.js";
import { readTurnStatus, type TurnStatus } from "./api/turns.js";

/**
 * The turn that refused a send, when it is still running; null when it ended
 * in the meantime (a send would go through now) or cannot be found.
 *
 * For a project the running turn is also written into the active-turn cache,
 * which is what a mounted chat's watch attaches from. A marketplace chat has no
 * active-turn read, so the caller attaches the returned turn itself.
 */
export async function findBlockingTurn(
  queryClient: QueryClient,
  scope: ChatScope,
  activeTurnId: string | undefined,
): Promise<TurnStatus | null> {
  let blocking: TurnStatus | null | undefined;
  if (activeTurnId) {
    const read = await readTurnStatus(scope, activeTurnId);
    blocking = read.kind === "status" ? read.status : null;
    if (scope.kind === "project" && blocking?.status === "running") {
      queryClient.setQueryData(projectKeys.activeTurn(scope.project), blocking);
    }
  } else if (scope.kind === "project") {
    const key = projectKeys.activeTurn(scope.project);
    await queryClient.refetchQueries({ queryKey: key });
    blocking = queryClient.getQueryData<TurnStatus | null>(key);
  }
  return blocking?.status === "running" ? blocking : null;
}
