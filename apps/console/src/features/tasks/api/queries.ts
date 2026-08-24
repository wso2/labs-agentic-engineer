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
import { client } from "../../../api/client";
import { taskKeys } from "./keys";
import { apiErrorMessage } from "../../../api/errors";

// This read is GitHub-backed, so it is priced differently from the DB-only run
// reads: it polls ONLY while a run is live, and the caller says when that is.
// An idle project must cost zero GitHub calls.
const TASKS_POLL_MS = 5_000;

/**
 * A version's issues, live from GitHub (state=all — a merged PR auto-closes
 * its issue, so `open` would hide everything that landed).
 *
 * `tag` scopes the read to one version by MILESTONE MEMBERSHIP; it is also
 * what makes bare ledger issues visible, since they carry no label to query
 * on. Omitted = every version's agent work and gates, ledger excluded.
 *
 * `live` is the run state, passed down: the run row is the page's single
 * liveness driver now, so this list no longer decides for itself whether to
 * keep polling by inspecting a task's derivedStatus — after the flip that
 * only says whether a GitHub issue is open.
 */
export function useAllTasks(
  projectName: string,
  tag?: string,
  opts: { live?: boolean } = {},
) {
  const { live = false } = opts;
  return useQuery({
    queryKey: taskKeys.list(projectName, tag),
    queryFn: async () => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/tasks",
        {
          params: {
            path: { projectName },
            query: { state: "all", ...(tag && { tag }) },
          },
        },
      );
      if (error || data === undefined) {
        throw new Error(apiErrorMessage(error, "Failed to load tasks"));
      }
      return data ?? [];
    },
    refetchInterval: live ? TASKS_POLL_MS : false,
  });
}

// One task with its execution history — the task page's initial state; the
// log stream (useTaskLog) takes over from there.
export function useTask(projectName: string, issueNumber: number) {
  return useQuery({
    queryKey: taskKeys.detail(projectName, issueNumber),
    queryFn: async () => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/tasks/{issueNumber}",
        { params: { path: { projectName, issueNumber } } },
      );
      if (error || data === undefined) {
        throw new Error(apiErrorMessage(error, "Failed to load the task"));
      }
      return data;
    },
    staleTime: 30_000,
    // A caller that holds no number yet holds 0, which is never an issue: the
    // validation surface reads the number off a run cycle that has not minted one
    // until it validates. Fetching anyway costs a live GitHub read that 404s by
    // construction, so the invariant lives here rather than in every caller.
    enabled: issueNumber > 0,
  });
}
