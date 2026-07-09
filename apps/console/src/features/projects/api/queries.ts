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

import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { useConfig } from "../../settings/api/queries";
import { projectKeys } from "./keys";

type CreateProjectRequest = components["schemas"]["CreateProjectRequest"];

export function useProjectsList(search = "", limit?: number) {
  return useInfiniteQuery({
    queryKey: projectKeys.list(search, limit),
    queryFn: async ({ pageParam }) => {
      const { data, error } = await client.GET("/projects", {
        params: {
          query: {
            ...(search && { search }),
            ...(pageParam && { cursor: pageParam }),
            ...(limit && { limit }),
          },
        },
      });
      if (error) {
        throw new Error(error.detail ?? error.title ?? "Failed to load projects");
      }
      return data;
    },
    initialPageParam: "",
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? null,
    // Keep the previous result visible while a new search resolves — no
    // flicker between keystrokes.
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });
}

export function useProject(projectName: string) {
  return useQuery({
    queryKey: projectKeys.detail(projectName),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}", {
        params: { path: { projectName } },
      });
      if (error) {
        throw new Error(error.detail ?? error.title ?? "Failed to load project");
      }
      return data;
    },
    staleTime: 30_000,
  });
}

// The overview watches the pipeline move, so its reads poll (issue #77
// decision: 10s while visible; SSE deferred).
const OVERVIEW_POLL_MS = 10_000;

function useProjectResource<T>(
  queryKey: readonly unknown[],
  fetcher: () => Promise<{ data?: T; error?: unknown }>,
  what: string,
) {
  return useQuery({
    queryKey,
    queryFn: async () => {
      const { data, error } = await fetcher();
      if (error || data === undefined) {
        const e = error as { detail?: string; title?: string } | undefined;
        throw new Error(e?.detail ?? e?.title ?? `Failed to load ${what}`);
      }
      return data;
    },
    refetchInterval: OVERVIEW_POLL_MS,
  });
}

export function useProjectStatus(projectName: string) {
  return useProjectResource(
    projectKeys.status(projectName),
    () =>
      client.GET("/projects/{projectName}/status", {
        params: { path: { projectName } },
      }),
    "project status",
  );
}

export function useProjectComponents(projectName: string) {
  return useProjectResource(
    projectKeys.components(projectName),
    () =>
      client.GET("/projects/{projectName}/components", {
        params: { path: { projectName } },
      }),
    "components",
  );
}

export function useProjectTasks(projectName: string) {
  return useProjectResource(
    projectKeys.tasks(projectName),
    () =>
      client.GET("/projects/{projectName}/tasks", {
        params: { path: { projectName } },
      }),
    "tasks",
  );
}

// Spec version tags (#117). The BE hasn't implemented /tags yet, so a failed
// read degrades to "no tags" instead of an error card — the version chips
// simply don't render until the endpoint lands.
export function useProjectTags(projectName: string) {
  return useQuery({
    queryKey: projectKeys.tags(projectName),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/tags", {
        params: { path: { projectName } },
      });
      if (error || data === undefined) return null;
      return data;
    },
    refetchInterval: OVERVIEW_POLL_MS,
  });
}

export function useCreateProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: CreateProjectRequest) => {
      const { data, error } = await client.POST("/projects", { body });
      if (error) {
        throw new Error(
          error.detail ?? error.title ?? "Failed to create project",
        );
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: projectKeys.lists() });
    },
  });
}

// Delete a project (#107). The BFF cascade destroys the OC project, its
// deployments, and the GitHub repo; the confirm dialog owns the warning.
// Invalidates every list page so the card leaves the grid on success.
export function useDeleteProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (projectName: string) => {
      const { error } = await client.DELETE("/projects/{projectName}", {
        params: { path: { projectName } },
      });
      if (error) {
        throw new Error(
          error.detail ?? error.title ?? "Failed to delete project",
        );
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: projectKeys.lists() });
    },
  });
}

// The connected GitHub org, for the repo-URL preview in the create flow.
// GitHub connection state now lives on the org config (issue #96 moved it
// off the old /org/credentials/github onto GET /config's gitProvider
// section), so this rides the settings feature's shared useConfig query
// instead of a second, independent fetch of the same endpoint. gitProvider
// is nullable (not connected yet), hence the optional chaining.
export function useGithubOrg() {
  const { data } = useConfig();
  return {
    data: data?.gitProvider?.githubLogin ?? data?.gitProvider?.identityLogin ?? null,
  };
}
