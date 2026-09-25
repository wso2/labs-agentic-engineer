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

import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { client } from "../../../api/client";
import { alertKeys } from "./keys";
import { apiErrorMessage } from "../../../api/errors";

// Bell badge poll interval (#154 decision: 60s, matches the ~3-4 min
// RCA→handoff completion time from the SRE-handoff runbook). Every query
// behind the bell badge shares it (see useAttentionUnread).
export const BELL_POLL_MS = 60_000;

// Last N reports the bell dropdown shows/counts (#154 decision: 50, no pagination).
export const BELL_LIMIT = 50;

// Top-nav bell (#154): last N reports, no pagination — a single page is the
// entire surface the dropdown shows.
export function useRecentAlerts(limit: number = BELL_LIMIT) {
  return useQuery({
    queryKey: alertKeys.recent(limit),
    queryFn: async () => {
      const { data, error } = await client.GET("/rca-agent/reports", {
        params: { query: { limit } },
      });
      if (error) {
        throw new Error(apiErrorMessage(error, "Failed to load alerts"));
      }
      return data.items ?? [];
    },
    refetchInterval: BELL_POLL_MS,
  });
}

// Alerts list page (#155): cursor-based infinite scroll, mirrors
// useProjectsList's pagination shape.
export function useAlertsInfinite(limit?: number) {
  return useInfiniteQuery({
    queryKey: alertKeys.list(limit),
    queryFn: async ({ pageParam }) => {
      const { data, error } = await client.GET("/rca-agent/reports", {
        params: {
          query: {
            ...(pageParam && { cursor: pageParam }),
            ...(limit && { limit }),
          },
        },
      });
      if (error) {
        throw new Error(apiErrorMessage(error, "Failed to load alerts"));
      }
      return data;
    },
    initialPageParam: "",
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? null,
    staleTime: 30_000,
  });
}

// Alert detail (#154's detail view, #155's stepper stages).
export function useAlertReport(reportId: string) {
  return useQuery({
    queryKey: alertKeys.detail(reportId),
    queryFn: async () => {
      const { data, error } = await client.GET("/rca-agent/reports/{reportId}", {
        params: { path: { reportId } },
      });
      if (error) {
        throw new Error(apiErrorMessage(error, "Failed to load the alert"));
      }
      return data;
    },
  });
}
