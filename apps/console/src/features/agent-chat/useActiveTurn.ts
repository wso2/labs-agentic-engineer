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

import { useQuery } from "@tanstack/react-query";
import { getActiveTurn } from "./api/turns.js";

/** Matches useAgentChat's foreign-turn poll — the turn is server-driven, so
 *  every surface learns about it at the same cadence. */
export const ACTIVE_TURN_POLL_MS = 12_000;

/** One query per project, so every reader shares the single in-flight read. */
export const activeTurnQueryKey = (projectName: string) =>
  ["agent-active-turn", projectName] as const;

export interface ActiveTurnSignal {
  /** A turn is running for this project RIGHT NOW, per the server. */
  active: boolean;
  /**
   * The read has answered at least once. Until then `active` is not knowledge,
   * it is a default — and a caller that acts on it fires into whatever is
   * already running. Gates hold on `!resolved` the way they hold on an
   * unresolved thread id.
   */
  resolved: boolean;
}

/**
 * The project's running turn, server-sourced (#485).
 *
 * `GET /turns/active` answers exactly the question aep-api asks when it accepts
 * or rejects a send: the D18 guard is one active turn PER PROJECT, and this
 * endpoint returns that same row. So a caller that holds while `active` is the
 * caller that never collects the 409 — including in this browser's blind spot,
 * a turn the BACKEND started (#485 starts `/start` at project creation), which
 * no local chat log can know about on a fresh tab or a cold cache.
 *
 * A failed read resolves as "no turn": recovery from a kickoff that never
 * started must stay possible, and an endpoint that cannot be reached is not
 * evidence of a running turn.
 *
 * `enabled: false` issues no request and reports resolved-and-idle — the
 * caller opted out, so this signal gates nothing. Pass the predicate that says
 * whether anyone is waiting on the answer (a pending send, a fresh project's
 * card), so settled surfaces never pay the poll.
 */
export function useActiveTurn(
  projectName: string,
  enabled = true,
): ActiveTurnSignal {
  const query = useQuery({
    queryKey: activeTurnQueryKey(projectName),
    queryFn: () => getActiveTurn(projectName),
    enabled,
    refetchInterval: ACTIVE_TURN_POLL_MS,
    // 404s while the repo provisions are expected — the interval is the retry.
    retry: false,
  });
  return {
    active: enabled && query.data?.status === "running",
    resolved: !enabled || query.isFetched,
  };
}
