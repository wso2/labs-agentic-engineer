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

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";
import { marketplaceKeys, resourceKeys } from "./keys";

type RegisterExternalResourceRequest =
  components["schemas"]["RegisterExternalResourceRequest"];

export function useOrgEndpoints() {
  return useQuery({
    queryKey: marketplaceKeys.endpoints,
    queryFn: async () => {
      const { data, error } = await client.GET("/dependencies/org-endpoints");
      if (error) {
        throw new Error(apiErrorMessage(error, "Failed to load endpoints"));
      }
      return data ?? [];
    },
    staleTime: 30_000,
  });
}

export function useOrgEnvironments() {
  return useQuery({
    queryKey: marketplaceKeys.environments,
    queryFn: async () => {
      const { data, error } = await client.GET("/dependencies/environments");
      if (error) {
        throw new Error(apiErrorMessage(error, "Failed to load environments"));
      }
      return data ?? [];
    },
    staleTime: 30_000,
  });
}

export function useRegisterExternalResource() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: RegisterExternalResourceRequest) => {
      const { data, error } = await client.POST("/dependencies/external-resources", {
        body,
      });
      if (error) {
        throw new Error(
          apiErrorMessage(error, "Failed to register the external resource"),
        );
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: resourceKeys.external });
    },
  });
}

export function useUpdateExternalResource(name: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: RegisterExternalResourceRequest) => {
      const { data, error } = await client.PUT("/dependencies/external-resources/{name}", {
        params: { path: { name } },
        body,
      });
      if (error) {
        throw new Error(
          apiErrorMessage(error, "Failed to update the external resource"),
        );
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: resourceKeys.external });
    },
  });
}

export { useExternalResources } from "../../settings/api/queries";
