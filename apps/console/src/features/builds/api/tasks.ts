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
import { apiErrorMessage } from "../../../api/errors";

// A version's tasks: its GitHub issues, copied from the old console
// (features/tasks/api/queries.ts). The read is GitHub-backed, so unlike the
// run reads it polls only while the version's run is live, and the caller
// says when that is: an idle project costs no GitHub calls.

const TASKS_POLL_MS = 5_000;

const taskKeys = {
  version: (projectName: string, tag: string) => ["projects", projectName, "tasks", tag] as const,
};

/**
 * The issues of one version (its milestone), open and closed: a merged pull
 * request closes its issue, so open alone would hide what landed.
 */
export function useVersionTasks(projectName: string, tag: string, live: boolean) {
  return useQuery({
    queryKey: taskKeys.version(projectName, tag),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/tasks", {
        params: { path: { projectName }, query: { state: "all", tag } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Failed to load the tasks"));
      return data;
    },
    refetchInterval: live ? TASKS_POLL_MS : false,
  });
}
