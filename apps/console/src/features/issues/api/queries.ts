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

import { queryOptions, useQuery } from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";
import { issueKeys } from "./keys";

type WireIssueInfo = components["schemas"]["IssueInfo"];

export type IssueAttentionReason =
  | "unverified_fix"
  | "no_change_verdict"
  | "escalated";

export type IssueInfo = WireIssueInfo & {
  attentionReason?: IssueAttentionReason;
};

// Shared by the Issues page and the notification bell's attention items
// (useAttentionUnread), so both read the same cache entry the same way.
export function projectIssuesQueryOptions(projectName: string, labels?: string) {
  return queryOptions({
    queryKey: issueKeys.list(projectName, labels),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/issues", {
        params: {
          path: { projectName },
          query: { ...(labels && { labels }) },
        },
      });
      if (error) {
        throw new Error(apiErrorMessage(error, "Failed to load issues"));
      }
      return (data ?? []) as IssueInfo[];
    },
    staleTime: 30_000,
  });
}

export function useProjectIssues(projectName: string, labels?: string) {
  return useQuery(projectIssuesQueryOptions(projectName, labels));
}
