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
import { PROTOTYPE_COMMAND } from "@aep/contracts/commands";
import { useSession } from "../../../auth/SessionContext";
import { chatKeyFor, setPendingSeed } from "../../agent-chat/chatStore";

/**
 * Generate / Regenerate prototype (#813, #818): send `/prototype` as a flow
 * turn through the project chat's seed slot, which the agent panel consumes
 * and sends exactly once.
 *
 * GUARDED, like every injected flow command: nobody typed it, so the panel
 * drops it rather than send it into an exchange that is waiting on the user.
 */
export function usePrototypeTurn(projectName: string): () => void {
  const { orgHandle } = useSession();
  return useCallback(
    () => setPendingSeed(chatKeyFor(orgHandle ?? "default", projectName), PROTOTYPE_COMMAND, true),
    [orgHandle, projectName],
  );
}
