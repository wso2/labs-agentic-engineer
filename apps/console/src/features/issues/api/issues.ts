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

import { useQueries, useQuery } from "@tanstack/react-query";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";
import type { IssueInfo } from "../model/issues";

// A project's issues and the RCA agent's reports, copied from the old console
// (features/issues/api/queries.ts, features/alerts/api/queries.ts). The issue
// reads are GitHub's, each a walk of the repository's issues, so they are
// shared by the Issues Page, the Issue card and the Alerts through one cache
// entry per project and re-read only now and then.

const ISSUES_STALE_MS = 30_000;

const issueKeys = {
  list: (projectName: string) => ["projects", projectName, "issues"] as const,
  detail: (projectName: string, issueNumber: number) => ["projects", projectName, "issues", issueNumber] as const,
  reports: () => ["rca-agent", "reports"] as const,
};

function issuesQuery(projectName: string) {
  return {
    queryKey: issueKeys.list(projectName),
    queryFn: async (): Promise<IssueInfo[]> => {
      const { data, error } = await client.GET("/projects/{projectName}/issues", {
        params: { path: { projectName } },
      });
      if (error) throw new Error(apiErrorMessage(error, "Couldn't load the issues"));
      return data ?? [];
    },
    staleTime: ISSUES_STALE_MS,
  };
}

/** The project's GitHub issues, open and closed, newest first, each with whether it needs attention. */
export function useProjectIssues(projectName: string) {
  return useQuery(issuesQuery(projectName));
}

/**
 * Every project's issues, for the Alerts: one read per project, re-read every
 * `refetchInterval`. A project whose read fails is counted, not dropped
 * silently: its attention is unknown, not none.
 */
export function useIssuesByProject(projectNames: readonly string[], refetchInterval: number) {
  return useQueries({
    queries: projectNames.map((projectName) => ({ ...issuesQuery(projectName), refetchInterval })),
    combine: (results) => ({
      isPending: results.some((r) => r.isPending),
      byProject: new Map(projectNames.flatMap((name, i) => (results[i]?.data ? [[name, results[i].data] as const] : []))),
      unread: projectNames.filter((_, i) => results[i]?.isError && !results[i]?.data),
    }),
  });
}

/**
 * One issue as the platform's task read has it: the same issue, with its
 * newest comments (the newest's first line is its status line). Any issue
 * answers, a task's or not.
 */
export function useIssueDetail(projectName: string, issueNumber: number) {
  return useQuery({
    queryKey: issueKeys.detail(projectName, issueNumber),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/tasks/{issueNumber}", {
        params: { path: { projectName, issueNumber } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, `Couldn't load issue #${issueNumber}`));
      return data;
    },
    staleTime: ISSUES_STALE_MS,
  });
}

/** The RCA agent's reports across the org, newest first: one page, as the old console's bell read it. */
export function useRcaReports(refetchInterval: number) {
  return useQuery({
    queryKey: issueKeys.reports(),
    queryFn: async () => {
      const { data, error } = await client.GET("/rca-agent/reports", { params: { query: { limit: 50 } } });
      if (error) throw new Error(apiErrorMessage(error, "Couldn't load the incident reports"));
      return data.items ?? [];
    },
    refetchInterval,
  });
}
