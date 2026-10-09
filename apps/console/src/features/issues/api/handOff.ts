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

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";
import { issueDetailKey, issuesListKey } from "./issues";

// Handing an issue to the coding agent from its card: aep-api adopts it into
// the deployed version's milestone and starts a run over it (the same
// promotion the issue's own agent makes), arms it, and starts a run over it.
// It answers 202 and works on out of band. It refuses with 409 when there is
// no deployed version ("Deploy a version first: …"), the issue was closed
// meanwhile, or it is not one the coding agent takes on; the card shows those
// words as they are.

/** Every other failure, said the same way. */
export const HAND_OFF_FAILED = "Couldn't hand it to the coding agent. Try again.";

/** aep-api refused the hand-off for a reason the person can act on; the message is its words. */
export class HandOffRefusedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "HandOffRefusedError";
  }
}

export async function handToCodingAgent(projectName: string, issueNumber: number, componentName: string): Promise<void> {
  const result = await client
    .POST("/projects/{projectName}/tasks/{issueNumber}/promote-from-issue", {
      params: { path: { projectName, issueNumber } },
      body: { componentName },
      // Any 2xx is handed over: its body is never parsed as JSON, so one the
      // client could not read does not turn a hand-off into a failure.
      parseAs: "text",
    })
    .catch(() => null);
  if (!result) throw new Error(HAND_OFF_FAILED);
  const { error, response } = result;
  if (!error) return;
  if (response.status === 409) throw new HandOffRefusedError(apiErrorMessage(error, HAND_OFF_FAILED));
  throw new Error(HAND_OFF_FAILED);
}

/** Hand the issue to the coding agent with the component it is about; the issue is read again once it is. */
export function useHandToCodingAgent(projectName: string, issueNumber: number) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (componentName: string) => handToCodingAgent(projectName, issueNumber, componentName),
    // Not awaited: the card says it was handed over at once, and the reads catch up.
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: issuesListKey(projectName), exact: true });
      void queryClient.invalidateQueries({ queryKey: issueDetailKey(projectName, issueNumber) });
    },
  });
}
