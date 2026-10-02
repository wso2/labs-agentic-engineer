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

export const aeStudioKeys = { all: ["ae-studio"] as const };

// The org's AE Studio: its state and, once ready, its three public URLs.
// Polled every 2 s while provisioning, because the console is waiting on it;
// otherwise refetched on window focus, which matters beyond freshness — a
// GET is what starts a converge on the backend.
export function useAeStudio() {
  return useQuery({
    queryKey: aeStudioKeys.all,
    queryFn: async () => {
      const { data, error } = await client.GET("/ae-studio");
      if (error) throw new Error(apiErrorMessage(error, "Failed to load AE Studio"));
      return data;
    },
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    refetchInterval: (q) => (q.state.data?.state === "provisioning" ? 2000 : false),
  });
}
