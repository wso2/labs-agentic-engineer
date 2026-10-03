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

import { useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { usePodQueryOptions } from "../../ae-studio/api/queries";
import { projectKeys } from "../../projects/api/keys";
import { getActiveTurn, type TurnStatus } from "./turns";

// Agent activity comes from the pod that runs the turns: this one query on
// GET /v1/projects/{p}/turns/active answers "is an agent working on this
// project, on what, and for whom" for every reader — the chat panel's
// foreign-turn watch, the overview's spec leg, the spec workspace and its
// rail. Readers use `flow` (what the turn is for) and `kind === "plan"` (a
// Plan turn, which works on a build rather than the spec).

const ACTIVE_POLL_MS = 5_000;
const IDLE_POLL_MS = 12_000;

/** The cadence: 5 s while a turn runs, so its end shows promptly; else 12 s. */
export function activeTurnPollDelay(turn: TurnStatus | null | undefined): number {
  return turn ? ACTIVE_POLL_MS : IDLE_POLL_MS;
}

export interface ActiveTurnOptions {
  /**
   * This observer's own cadence, from the last answer. The chat panel asks
   * faster while it has nothing to show (a new project's kickoff is on its
   * way); every other reader takes the default.
   */
  pollDelay?: (turn: TurnStatus | null | undefined) => number;
}

/**
 * The project's running turn, or null when none runs; undefined until the
 * first answer, and while AE Studio is not `ready`. A failed read keeps the
 * last answer (the query errors, its data stays).
 */
export function useActiveTurn(
  projectName: string,
  { pollDelay = activeTurnPollDelay }: ActiveTurnOptions = {},
): UseQueryResult<TurnStatus | null> {
  const queryClient = useQueryClient();
  const pod = usePodQueryOptions();
  const queryKey = projectKeys.activeTurn(projectName);
  return useQuery<TurnStatus | null, Error>({
    ...pod,
    queryKey,
    enabled: pod.enabled && projectName !== "",
    queryFn: async () => {
      const previous = queryClient.getQueryData<TurnStatus | null>(queryKey);
      const turn = await getActiveTurn(projectName);
      // The git-derived spec facts move when a turn ends, and the status poll
      // may sit on its idle cadence by then: re-ask it now.
      if (previous && previous.turnId !== turn?.turnId) {
        void queryClient.invalidateQueries({ queryKey: projectKeys.status(projectName) }, { cancelRefetch: false });
      }
      return turn;
    },
    // The next poll is the retry.
    retry: false,
    refetchInterval: (query) => pollDelay(query.state.data),
  });
}
