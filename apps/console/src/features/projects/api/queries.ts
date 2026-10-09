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

import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { ApiRequestError, apiErrorMessage } from "../../../api/errors";
import { useConfig } from "../../settings/api/queries";
import { isSystemProject, withoutSystemProjects } from "../systemProject";

export type Project = components["schemas"]["Project"];
type CreateProjectRequest = components["schemas"]["CreateProjectRequest"];

// Every read of the org's project list is under `lists`, apart from each
// project's own reads (under "projects"), so a create or a delete refreshes
// the lists without re-reading every project.
const projectKeys = {
  lists: () => ["project-lists"] as const,
  list: () => [...projectKeys.lists(), "first"] as const,
  /** The Projects grid's pages for one search. */
  pages: (search: string, limit: number) => [...projectKeys.lists(), "pages", { search, limit }] as const,
  all: () => [...projectKeys.lists(), "all"] as const,
  detail: (projectName: string) => ["projects", projectName] as const,
};

/** The org's projects, first page: where the Dashboard sends an org with none to New project. */
export function useProjects() {
  return useQuery({
    queryKey: projectKeys.list(),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects");
      if (error) throw new ApiRequestError(error, "Couldn't load projects");
      return withoutSystemProjects(data).items ?? [];
    },
  });
}

/**
 * The name of every project that has a repository, every page read: what the
 * Dashboard's Alerts ask about, since issues live in a project's repository
 * (a platform project such as Agent Manager's default has none, so nothing to
 * read and nothing that could need a person). Re-read now and then, as the
 * Alerts are.
 */
export function useRepoProjectNames(refetchInterval: number) {
  return useQuery({
    queryKey: projectKeys.all(),
    queryFn: async () => {
      const names: string[] = [];
      let cursor: string | undefined;
      do {
        const { data, error } = await client.GET("/projects", { params: { query: cursor ? { cursor } : {} } });
        if (error) throw new ApiRequestError(error, "Couldn't load projects");
        names.push(...(data.items ?? []).filter((p) => p.repoUrl && !isSystemProject(p.name)).map((p) => p.name));
        cursor = data.nextCursor || undefined;
      } while (cursor);
      return names;
    },
    refetchInterval,
  });
}

/**
 * The Projects grid: the projects whose name matches `search`, a page at a
 * time, the next page on asking (View more). The previous answer stays on
 * screen while a new search is read, so typing does not flicker the grid.
 */
export function useProjectPages(search: string, limit: number) {
  return useInfiniteQuery({
    queryKey: projectKeys.pages(search, limit),
    queryFn: async ({ pageParam }) => {
      const { data, error } = await client.GET("/projects", {
        params: { query: { limit, ...(search ? { search } : {}), ...(pageParam ? { cursor: pageParam } : {}) } },
      });
      if (error) throw new ApiRequestError(error, "Couldn't load projects");
      return withoutSystemProjects(data);
    },
    initialPageParam: "",
    getNextPageParam: (last) => last.nextCursor || undefined,
    placeholderData: keepPreviousData,
  });
}

/**
 * Delete a project. The platform drops the project, its deployments and its
 * build history; the GitHub repository is kept, with its code and issues.
 */
export function useDeleteProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (projectName: string) => {
      const { error } = await client.DELETE("/projects/{projectName}", {
        params: { path: { projectName } },
      });
      if (error) throw new Error(apiErrorMessage(error, "Couldn't delete the project"));
    },
    // The caller leaves the project's pages; its own reads are left to expire
    // rather than re-read into a not-found while they are still on screen.
    onSuccess: () => queryClient.invalidateQueries({ queryKey: projectKeys.lists() }),
  });
}

/** One project: the overview's header and the chat's breadcrumb. */
export function useProject(projectName: string) {
  return useQuery({
    queryKey: projectKeys.detail(projectName),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}", {
        params: { path: { projectName } },
      });
      if (error) throw new ApiRequestError(error, "Couldn't load the project");
      return data;
    },
  });
}

/** What a project is called on screen: its display name, else its slug. */
export function projectLabel(project: Project): string {
  return project.displayName?.trim() || project.name;
}

/**
 * Create a project from New project. With a `prompt`, the platform persists it
 * as the project's brief and fires the `/start` kickoff itself (#562), unless
 * `referencesPending` says documents are coming, in which case the reference
 * upload fires it instead.
 */
export function useCreateProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: CreateProjectRequest) => {
      const { data, error } = await client.POST("/projects", { body });
      if (error) {
        // ApiRequestError, not Error: the create flow reacts to a repo-name
        // conflict specifically (#561), and the envelope's `code` is the only
        // stable way to tell it apart from any other failure.
        throw new ApiRequestError(error, "Failed to create project");
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: projectKeys.lists() });
    },
  });
}

/**
 * Upload the reference documents attached on New project to the project's
 * off-git reference store (ADR-0017). Deliberately not part of
 * useCreateProject: a failed upload must leave the created project intact, so
 * the details step can offer Retry upload or Continue without documents.
 */
export function useUploadReferences() {
  return useMutation({
    mutationFn: async ({ projectName, files }: { projectName: string; files: File[] }) => {
      // multipart, not base64-in-JSON: raw bytes keep the server's 5 MiB cap
      // honest, where a base64 body would inflate ~33%.
      const formData = new FormData();
      for (const file of files) formData.append("files", file);
      // openapi-fetch passes FormData through its default body serializer
      // untouched (the browser sets the boundary), but the generated request
      // type describes the JSON Schema shape, not the wire.
      const { error } = await client.POST("/projects/{projectName}/references", {
        params: { path: { projectName } },
        body: formData as unknown as { files: string[] },
      });
      if (error) {
        throw new Error(apiErrorMessage(error, "Failed to upload the reference documents"));
      }
    },
  });
}

/** The GitHub organization new repositories are created in, from the org's config. */
export function useGithubOrg(): string | null {
  const { data } = useConfig();
  return data?.gitProvider?.githubLogin ?? data?.gitProvider?.identityLogin ?? null;
}
