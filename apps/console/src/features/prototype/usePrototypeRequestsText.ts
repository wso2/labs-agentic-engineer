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

import { useMemo } from "react";
import type { PrototypeFeedback } from "../agent-chat/turnScope";
import { feedbackSummary } from "./model/summary";
import { usePrototypes } from "./usePrototypes";

/**
 * A prototype review's requests as the chat row reads them, named from the
 * prototype's manifest when the room has it (ids otherwise).
 */
export function usePrototypeRequestsText(projectName: string, feedback: PrototypeFeedback): string {
  const prototypes = usePrototypes(projectName);
  const manifest = prototypes?.find((p) => p.component === feedback.component)?.files?.manifest ?? null;
  return useMemo(() => feedbackSummary(feedback, manifest), [feedback, manifest]);
}
