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

import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../../../api/client";
import { isPodUnavailable, onPodOutage, setAeStudioUrls } from "../../../api/aeStudio";
import { apiErrorCode, apiErrorMessage } from "../../../api/errors";

export const aeStudioKeys = { all: ["ae-studio"] as const };

// The org's AE Studio: its state and, once ready, its three public URLs.
// Polled every 2 s while provisioning, because the console is waiting on it,
// and every 5 s while the read itself fails, so a blip does not leave the
// gate on a stale answer until the next focus; otherwise refetched on window
// focus, which matters beyond freshness — a GET is what starts a converge on
// the backend.
//
// Each answer points the design-agent client (api/aeStudio.ts) at its URL, or
// drops it, before any consumer sees it: a component that reads `ready` can
// call designAgent() in the same render. A failed read leaves it as it was.
export function useAeStudio() {
  return useQuery({
    queryKey: aeStudioKeys.all,
    queryFn: async () => {
      const { data, error } = await client.GET("/ae-studio");
      if (error) throw new Error(apiErrorMessage(error, "Failed to load AE Studio"));
      setAeStudioUrls(data.state === "ready" && data.urls ? data.urls : null);
      return data;
    },
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    refetchInterval: (q) =>
      q.state.data?.state === "provisioning" ? 2000 : q.state.status === "error" ? 5000 : false,
  });
}

/**
 * Whether a failure says AE Studio is not serving: the pod's 503 or no answer
 * from it, or aep-api's 503 `ae_studio_unavailable` for a read it serves
 * through AE Studio (spec state, versions, reports, issues, tasks, skills).
 */
function isAeStudioOutage(error: unknown): boolean {
  return isPodUnavailable(error) || apiErrorCode(error) === "ae_studio_unavailable";
}

/**
 * Re-read AE Studio whenever a request finds it not serving: a failed query
 * (isAeStudioOutage), or a design-agent call outside any query (the chat's,
 * through the pod client's outage channel). A restart then shows as the
 * banner, and the reads wait for `ready` again instead of failing one by one.
 * Mounted once, by the gate.
 */
export function useReReadAeStudioOnOutage(): void {
  const queryClient = useQueryClient();
  useEffect(() => {
    const reRead = () =>
      void queryClient.invalidateQueries({ queryKey: aeStudioKeys.all }, { cancelRefetch: false });
    const offQueries = queryClient.getQueryCache().subscribe((event) => {
      if (event.type !== "updated") return;
      const { action } = event;
      if ((action.type === "failed" || action.type === "error") && isAeStudioOutage(action.error)) reRead();
    });
    const offPod = onPodOutage(reRead);
    return () => {
      offQueries();
      offPod();
    };
  }, [queryClient]);
}
