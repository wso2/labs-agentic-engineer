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
import { isStudioToolsUnavailable, setAeStudioUrls } from "../../../api/aeStudio";
import { apiErrorMessage } from "../../../api/errors";

export const aeStudioKeys = { all: ["ae-studio"] as const };

// The org's AE Studio: its state and, once ready, its three public URLs.
// Polled every 2 s while provisioning, because the console is waiting on it,
// and every 5 s while the read itself fails, so a blip does not leave the
// gate on a stale answer until the next focus; otherwise refetched on window
// focus, which matters beyond freshness — a GET is what starts a converge on
// the backend.
//
// Each answer points the pod clients (api/aeStudio.ts) at its URLs, or drops
// them, before any consumer sees it: a component that reads `ready` can call
// studioTools() in the same render. A failed read leaves them as they were.
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
 * Whether the pods can be called. Every pod-backed read gates `enabled` on it,
 * so nothing asks a pod that is not there; a read already answered keeps its
 * data while AE Studio restarts.
 */
export function useAeStudioReady(): boolean {
  return useAeStudio().data?.state === "ready";
}

/**
 * Re-read AE Studio whenever a pod read finds the pod not serving (a 503 or
 * no answer): a restart then shows as the banner, and the reads wait for
 * `ready` again instead of failing one by one. Mounted once, by the gate.
 */
export function useReReadAeStudioOnOutage(): void {
  const queryClient = useQueryClient();
  useEffect(
    () =>
      queryClient.getQueryCache().subscribe((event) => {
        if (event.type !== "updated") return;
        const { action } = event;
        if ((action.type === "failed" || action.type === "error") && isStudioToolsUnavailable(action.error)) {
          void queryClient.invalidateQueries({ queryKey: aeStudioKeys.all }, { cancelRefetch: false });
        }
      }),
    [queryClient],
  );
}
