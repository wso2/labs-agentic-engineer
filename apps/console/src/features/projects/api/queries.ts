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
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { useConfig } from "../../settings/api/queries";
import { firstEndpointUrl } from "../lib/deploymentUrl";
import { deploymentsAreMoving } from "../lib/deploymentRows";
import { projectKeys } from "./keys";
import { apiErrorMessage } from "../../../api/errors";

type CreateProjectRequest = components["schemas"]["CreateProjectRequest"];
type BuildRequest = components["schemas"]["BuildRequest"];

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
        throw new Error(apiErrorMessage(error, "Failed to load projects"));
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
        throw new Error(apiErrorMessage(error, "Failed to load project"));
      }
      return data;
    },
    staleTime: 30_000,
  });
}

// Spec-view reads still poll at a flat interval (tags below).
const OVERVIEW_POLL_MS = 10_000;

// Status polling is adaptive (#183): fast while any stage is moving, slow
// when settled — idle polling stays on because spec pushes happen on GitHub
// and must flip the v1+ chip without a reload.
const STATUS_ACTIVE_POLL_MS = 5_000;
const STATUS_IDLE_POLL_MS = 30_000;

type ProjectStatus = components["schemas"]["ProjectStatus"];
type Deployment = components["schemas"]["Deployment"];

function statusIsMoving(status: ProjectStatus): boolean {
  return (
    status.build.status === "running" ||
    status.deploy.status === "deploying" ||
    status.repoStatus === "pending" ||
    status.repoStatus === "cloning"
  );
}

function useProjectResource<T>(
  queryKey: readonly unknown[],
  fetcher: () => Promise<{ data?: T; error?: unknown }>,
  what: string,
  refetchInterval?: number | ((data: T | undefined) => number | false),
  enabled = true,
) {
  return useQuery({
    queryKey,
    enabled,
    queryFn: async () => {
      const { data, error } = await fetcher();
      if (error || data === undefined) {
        throw new Error(apiErrorMessage(error, `Failed to load ${what}`));
      }
      return data;
    },
    ...(refetchInterval !== undefined && {
      refetchInterval:
        typeof refetchInterval === "function"
          ? (query: { state: { data: T | undefined } }) =>
              refetchInterval(query.state.data)
          : refetchInterval,
    }),
  });
}

// The page's only poller (#183): the whole pipeline renders from this one
// aggregate (ADR-0006), so nothing else on the overview needs an interval.
//
// `enabled` exists for callers that want this aggregate on SOME routes only —
// the shell's first-run read (#485) asks for it on the spec view and nowhere
// else, rather than adding a standing poll to every project page. Callers on
// the same route share one query by key, so asking twice costs nothing.
export function useProjectStatus(projectName: string, enabled = true) {
  return useProjectResource(
    projectKeys.status(projectName),
    () =>
      client.GET("/projects/{projectName}/status", {
        params: { path: { projectName } },
      }),
    "project status",
    (status) =>
      !status || statusIsMoving(status)
        ? STATUS_ACTIVE_POLL_MS
        : STATUS_IDLE_POLL_MS,
    enabled,
  );
}

// No standing interval (#183): the overview refetches this when the status
// poll shows a build/deploy transition (components only change then).
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

// The Deployments board's pollers (#216): one list-deployments read per
// component — reusing the endpoint (and cache keys) the overview's
// "Open app" link already hits, so no new contract surface. Each query
// polls on the adaptive status-poll regime, judged by its own bindings.
// Failures degrade per-component: the board renders what loaded and
// reports how many components couldn't be read.
export function useComponentsDeployments(
  projectName: string,
  componentNames: string[],
) {
  return useQueries({
    queries: componentNames.map((componentName) => ({
      queryKey: projectKeys.componentDeployments(projectName, componentName),
      queryFn: async () => {
        const { data, error } = await client.GET(
          "/projects/{projectName}/components/{componentName}/deployments",
          { params: { path: { projectName, componentName } } },
        );
        if (error || data === undefined) {
          throw new Error(apiErrorMessage(error, "Failed to load deployments"));
        }
        return data;
      },
      refetchInterval: (query: {
        state: { data: { items: Deployment[] | null } | undefined };
      }) =>
        !query.state.data || deploymentsAreMoving(query.state.data.items)
          ? STATUS_ACTIVE_POLL_MS
          : STATUS_IDLE_POLL_MS,
    })),
    combine: (results) => ({
      isPending: results.some((r) => r.isPending),
      deployments: results.flatMap((r) => r.data?.items ?? []),
      failedCount: results.filter((r) => r.isError).length,
    }),
  });
}

// A web app's public URL (#196): read from its deployments — the dev
// binding's resolved endpointUrl rides on list-deployments, while
// list-components never fills Component.endpointUrl (noted contract drift).
// Fetched only for web-application rows; refreshes with the components list
// (same no-standing-interval regime — a deploy transition invalidates both).
export function useComponentEndpointUrl(
  projectName: string,
  componentName: string,
) {
  return useQuery({
    queryKey: projectKeys.componentDeployments(projectName, componentName),
    queryFn: async () => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/components/{componentName}/deployments",
        { params: { path: { projectName, componentName } } },
      );
      if (error || data === undefined) {
        throw new Error(apiErrorMessage(error, "Failed to load deployments"));
      }
      return data;
    },
    select: (data) => firstEndpointUrl(data.items),
  });
}

// A service component's OpenAPI contract, for the in-app viewer dialog. Lazy
// (`enabled`) — only fetched when the user opens the contract, not on every
// overview render. Goes through the authenticated client so the Bearer token
// rides along; the old plain <a href> to this JWT-guarded endpoint 401'd
// because a browser navigation carries no Authorization header.
export function useComponentOpenApi(
  projectName: string,
  componentName: string,
  enabled: boolean,
) {
  return useQuery({
    queryKey: projectKeys.componentOpenapi(projectName, componentName),
    enabled,
    queryFn: async () => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/components/{componentName}/openapi",
        { params: { path: { projectName, componentName } } },
      );
      if (error || data === undefined) {
        throw new Error(
          apiErrorMessage(error, "Failed to load the API contract"),
        );
      }
      return data;
    },
    staleTime: 30_000,
  });
}

// Re-collect an external connection's values (#395: dummy values at build
// time, real ones later). POST …/external-resources/{name}/values re-splits
// plain/secret by the design's schema, rewrites secrets to the secret
// manager and re-authors the OC resource — values never echo back, so this
// is write-only by design. No automatic retry (a failed write is surfaced).
export function useSaveConnectionValues(projectName: string) {
  return useMutation({
    mutationFn: async ({
      name,
      environment,
      values,
    }: {
      name: string;
      environment: string;
      values: Record<string, string>;
    }) => {
      const { data, error } = await client.POST(
        "/projects/{projectName}/dependencies/external-resources/{name}/values",
        {
          params: { path: { projectName, name } },
          body: { environments: { [environment]: values } },
        },
      );
      // Gate on `error` alone (#401 review): the contract declares a JSON 200,
      // and an empty-body success must not read as a failure.
      if (error) {
        throw new Error(apiErrorMessage(error, "Failed to save the connection's values"));
      }
      return data;
    },
  });
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
          apiErrorMessage(error, "Failed to create project"),
        );
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: projectKeys.lists() });
    },
  });
}

// Delete a project (#107). The BFF cascade destroys the OC project and its
// deployments, ends the run supervisors, and purges the platform's own repo
// record, executions and run ledger. It does NOT delete the GitHub repository —
// that survives, and the confirm dialog says so. Invalidates every list page so
// the card leaves the grid on success.
export function useDeleteProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (projectName: string) => {
      const { error } = await client.DELETE("/projects/{projectName}", {
        params: { path: { projectName } },
      });
      if (error) {
        throw new Error(
          apiErrorMessage(error, "Failed to delete project"),
        );
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: projectKeys.lists() });
    },
  });
}

// Checks whether a project is ready to build — missing/unresolved dependency
// inputs surface here so the build drawer (#164) can render them before the
// user commits to a build. Disabled by default: the drawer triggers it
// on-demand (refetch) rather than on mount.
export function useBuildPreflight(projectName: string) {
  return useQuery({
    queryKey: projectKeys.buildPreflight(projectName),
    enabled: false,
    retry: false,
    queryFn: async () => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/build/preflight",
        {
          params: { path: { projectName } },
        },
      );
      if (error || data === undefined) {
        throw new Error(apiErrorMessage(error, "Failed to check build readiness"));
      }
      return data;
    },
  });
}

// Trigger a project build (#162): the single-tag flow — the BFF validates,
// tags v<N>, and runs the dev workflow, returning the tag. The Spec view
// commits the room first (collab flush-on-demand) so this tags the current
// HEAD. Carries the drawer's resolved dependency inputs (#164); defaults to
// an empty list for callers that haven't been rewired to the drawer yet.
// Invalidates the project's reads since status/tasks/tags shift once the
// build starts.
export function useBuildProject(projectName: string) {
  const queryClient = useQueryClient();
  const mutation = useMutation({
    mutationFn: async (body: BuildRequest) => {
      const { data, error } = await client.POST("/projects/{projectName}/build", {
        params: { path: { projectName } },
        body,
      });
      if (error || data === undefined) {
        const err = new Error(apiErrorMessage(error, "Failed to start the build"));
        // The build gate's 422 carries per-file detail rows ({field, message})
        // — surface them so the console can render the refusal as a checklist
        // (#372) instead of one flattened string.
        const details = (error as { details?: unknown } | undefined)?.details;
        if (Array.isArray(details)) {
          (err as Error & { details?: unknown }).details = details;
        }
        throw err;
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: projectKeys.detail(projectName),
      });
    },
  });
  // TanStack infers the mutation's variables type from mutationFn's own
  // parameter, which stays required even with a default value (a default
  // only makes a *plain* function's parameter optional, not a generically
  // inferred one) — so callers that predate the drawer's inputs and still
  // call mutate()/mutateAsync() with no arguments need the default applied
  // at this thin wrapper instead.
  return {
    ...mutation,
    mutate: (
      body: BuildRequest = { inputs: [] },
      options?: Parameters<typeof mutation.mutate>[1],
    ) => mutation.mutate(body, options),
    mutateAsync: (
      body: BuildRequest = { inputs: [] },
      options?: Parameters<typeof mutation.mutateAsync>[1],
    ) => mutation.mutateAsync(body, options),
  };
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

// Retry the server-side spec kickoff (#485). The console NEVER starts the spec
// interview by sending a chat message — it asks the backend to do its own job
// again, which is why this is a mutation and not a seeded `/start`. The
// response IS the new state, and a refused attempt comes back as a 200 body
// (status failed with a reason), so the card renders the same way whether the
// state arrived from here or from the status poll. The status invalidation
// keeps the two in step.
export function useRetrySpecKickoff(projectName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { data, error } = await client.POST(
        "/projects/{projectName}/spec/kickoff",
        { params: { path: { projectName } } },
      );
      if (error || data === undefined) {
        throw new Error(apiErrorMessage(error, "Failed to retry the spec interview"));
      }
      return data;
    },
    onSettled: () => {
      void queryClient.invalidateQueries({
        queryKey: projectKeys.status(projectName),
      });
    },
  });
}
