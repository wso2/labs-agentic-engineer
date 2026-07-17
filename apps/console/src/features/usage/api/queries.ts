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
import { usageKeys } from "./keys";

function firstError(error: unknown, fallback: string): Error {
  const e = error as { detail?: string; title?: string } | undefined;
  return new Error(e?.detail ?? e?.title ?? fallback);
}

// Per-project usage rollups (#245). Actuals accrue as executions/turns land;
// 30s staleness is plenty — the surfaces that need faster movement (a running
// build) get it from their own polling reads carrying usage inline.
export function useProjectUsage(projectName: string) {
  return useQuery({
    queryKey: usageKeys.project(projectName),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/usage", {
        params: { path: { projectName } },
      });
      if (error || data === undefined) {
        throw firstError(error, "Failed to load project usage");
      }
      return data;
    },
    staleTime: 30_000,
  });
}

// Pre-action historical-average estimate (#245). `estimate` is null on cold
// start (no historical data) — the UI says "no estimate yet", never a made-up
// number.
export function useProjectEstimate(
  projectName: string,
  action: "design" | "build",
) {
  return useQuery({
    queryKey: usageKeys.estimate(projectName, action),
    queryFn: async () => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/estimates",
        { params: { path: { projectName }, query: { action } } },
      );
      if (error || data === undefined) {
        throw firstError(error, "Failed to load estimate");
      }
      return data;
    },
    staleTime: 60_000,
  });
}

// Model pricing catalog (#245) — near-static reference data (checked-in
// defaults in aep-api), so it never goes stale within a session.
export function useModelPricing() {
  return useQuery({
    queryKey: usageKeys.pricing,
    queryFn: async () => {
      const { data, error } = await client.GET("/models/pricing", {});
      if (error || data === undefined) {
        throw firstError(error, "Failed to load model pricing");
      }
      return data;
    },
    staleTime: Infinity,
  });
}
