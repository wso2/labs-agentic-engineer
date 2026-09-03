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
import { chatKeyFor } from "./chatStore.js";
import { peekPlan, subscribePlan, type PlanSnapshot } from "./planStore.js";

/**
 * The project's declared plan (#576), or null when no plan is live and no
 * wreckage stands. The spec rail renders its checklist and count from this;
 * SpecView steers follow-the-write (ADR-0026) by its `writingPath`.
 */
export function usePlan(
  org: string,
  projectName: string | undefined,
): PlanSnapshot | null {
  const chatKey = projectName ? chatKeyFor(org, projectName) : null;
  return useSyncExternalStore(
    useCallback(
      (fn: () => void) => (chatKey ? subscribePlan(chatKey, fn) : () => {}),
      [chatKey],
    ),
    () => (chatKey ? peekPlan(chatKey) : null),
  );
}
