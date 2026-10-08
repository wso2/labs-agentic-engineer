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

import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { ApiRequestError } from "../../../api/errors";

// What a project can depend on, read and written through aep-api's
// dependencies operations. Copied from the old console's marketplace and
// settings queries.

type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type RegisterExternalResourceRequest = components["schemas"]["RegisterExternalResourceRequest"];
type PromoteExternalResourceRequest = components["schemas"]["PromoteExternalResourceRequest"];

const resourceKeys = {
  platform: ["dependencies", "platform-resource-types"] as const,
  external: ["dependencies", "external-resources"] as const,
  endpoints: ["dependencies", "org-endpoints"] as const,
};

/** The platform's resource types: what it provisions on a project's behalf. */
export function usePlatformResourceTypes(enabled = true) {
  return useQuery({
    queryKey: resourceKeys.platform,
    queryFn: async () => {
      const { data, error } = await client.GET("/dependencies/platform-resource-types");
      if (error) throw new ApiRequestError(error, "Couldn't load the platform's resources");
      return data ?? [];
    },
    staleTime: 60_000,
    enabled,
  });
}

/** The organization's Registered External resources and those projects hold for themselves. */
export function useExternalResources(enabled = true) {
  return useQuery({
    queryKey: resourceKeys.external,
    queryFn: async () => {
      const { data, error } = await client.GET("/dependencies/external-resources");
      if (error) throw new ApiRequestError(error, "Couldn't load the external resources");
      return data ?? [];
    },
    staleTime: 30_000,
    enabled,
  });
}

/** What other projects offer: their components' endpoints, visible to the organization. */
export function useOrgEndpoints(enabled = true) {
  return useQuery({
    queryKey: resourceKeys.endpoints,
    queryFn: async () => {
      const { data, error } = await client.GET("/dependencies/org-endpoints");
      if (error) throw new ApiRequestError(error, "Couldn't load what other projects offer");
      return data ?? [];
    },
    staleTime: 30_000,
    enabled,
  });
}

/**
 * The record a write returned, in the list at once (so the card it navigates
 * to finds it) in place of the row `replaces` picks; then the list is read
 * again.
 */
function putRecord(queryClient: QueryClient, record: ExternalResourceDTO, replaces: (r: ExternalResourceDTO) => boolean) {
  // What the write does not echo (who uses it, its instances) is kept from the row it replaces.
  queryClient.setQueryData<ExternalResourceDTO[]>(resourceKeys.external, (list) => {
    if (!list) return list;
    const old = list.find(replaces);
    return [...list.filter((r) => !replaces(r)), { ...old, ...record, consumers: record.consumers ?? old?.consumers ?? [] }];
  });
  void queryClient.invalidateQueries({ queryKey: resourceKeys.external });
}

const sameRecord = (name: string) => (r: ExternalResourceDTO) => r.name === name && r.scope !== "project";

export function useRegisterExternalResource() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: RegisterExternalResourceRequest) => {
      const { data, error } = await client.POST("/dependencies/external-resources", { body });
      if (error) throw new ApiRequestError(error, "Couldn't register the resource");
      return data;
    },
    onSuccess: (record) => putRecord(queryClient, record, sameRecord(record.name)),
  });
}

export function useUpdateExternalResource(name: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: RegisterExternalResourceRequest) => {
      const { data, error } = await client.PUT("/dependencies/external-resources/{name}", { params: { path: { name } }, body });
      if (error) throw new ApiRequestError(error, "Couldn't save the resource");
      return data;
    },
    onSuccess: (record) => putRecord(queryClient, record, sameRecord(name)),
  });
}

export function useDeleteExternalResource(name: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { error } = await client.DELETE("/dependencies/external-resources/{name}", { params: { path: { name } } });
      if (error) throw new ApiRequestError(error, "Couldn't delete the resource");
    },
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: resourceKeys.external }),
  });
}

/**
 * Promote to organization: the organization takes the project's resource as
 * its record, with the instructions and values it adds, and the project's
 * dependency becomes a copy of that record.
 */
export function usePromoteExternalResource(project: string, name: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: PromoteExternalResourceRequest) => {
      const { data, error } = await client.POST("/projects/{projectName}/dependencies/external-resources/{name}/promote", {
        params: { path: { projectName: project, name } },
        body,
      });
      if (error) throw new ApiRequestError(error, "Couldn't promote the resource");
      return data;
    },
    onSuccess: (record) =>
      putRecord(queryClient, { ...record, scope: "org" }, (r) => r.name === name && (r.scope !== "project" || r.project === project)),
  });
}
