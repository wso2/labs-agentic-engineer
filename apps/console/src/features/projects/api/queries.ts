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
import { githubKeys, projectKeys } from "./keys";

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

export function useProjectBoard(projectName: string) {
  return useProjectResource(
    projectKeys.board(projectName),
    () =>
      client.GET("/projects/{projectName}/board", {
        params: { path: { projectName } },
      }),
    "build board",
  );
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
// Read from the consolidated org-config projection (GET /config replaced
// /org/credentials/github when the contract consolidated org config).
export function useGithubOrg() {
  return useQuery({
    queryKey: githubKeys.status,
    queryFn: async () => {
      const { data, error } = await client.GET("/config");
      if (error || !data) return null;
      return data.gitProvider.githubLogin ?? null;
    },
    staleTime: 5 * 60_000,
    retry: false,
  });
}
