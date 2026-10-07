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

import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";
import type { components } from "../../../generated/aep-api";
import { deploymentsAreMoving } from "../model/pipeline";

// The Deploy Page's reads, copied from the old console
// (features/projects/api/queries.ts, features/spec/api/roles.ts) and trimmed
// to what the page, its Configure card and Try it show. Same calls; promotion
// has none, because the platform has no promote operation yet.

export type DeployStage = components["schemas"]["DeployStage"];
export type ProjectRolesView = components["schemas"]["ProjectRolesView"];
type Deployment = components["schemas"]["Deployment"];
type DependencyReadiness = components["schemas"]["ProjectDependencyReadiness"];

// A deployment converging is watched closely; a settled one is re-read now
// and then, since a build elsewhere can redeploy it.
const MOVING_POLL_MS = 5_000;
const SETTLED_POLL_MS = 30_000;

const deployKeys = {
  environments: () => ["environments"] as const,
  status: (projectName: string) => ["projects", projectName, "status"] as const,
  components: (projectName: string) => ["projects", projectName, "components"] as const,
  deployments: (projectName: string, componentName: string) =>
    ["projects", projectName, "components", componentName, "deployments"] as const,
  openapi: (projectName: string, componentName: string) =>
    ["projects", projectName, "components", componentName, "openapi"] as const,
  readiness: (projectName: string, environment: string) =>
    ["projects", projectName, "dependencies", "readiness", environment] as const,
  designDependencies: (projectName: string) => ["projects", projectName, "design", "dependencies"] as const,
  roles: (projectName: string) => ["projects", projectName, "roles"] as const,
};

/**
 * The org's environments, as the platform's pipeline lists them. Changes only
 * when a platform admin edits the pipeline, hence the long stale time.
 */
export function useEnvironments() {
  return useQuery({
    queryKey: deployKeys.environments(),
    queryFn: async () => {
      const { data, error } = await client.GET("/dependencies/environments");
      if (error) throw new Error(apiErrorMessage(error, "Couldn't load the environments"));
      return data ?? [];
    },
    staleTime: 5 * 60 * 1000,
  });
}

/**
 * The project's status, for its deploy aggregate: the version live in the
 * pipeline's first environment and how its rollout stands. Polled closely
 * while it deploys.
 */
export function useProjectStatus(projectName: string) {
  return useQuery({
    queryKey: deployKeys.status(projectName),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/status", {
        params: { path: { projectName } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Couldn't load the project's status"));
      return data;
    },
    refetchInterval: (query) =>
      query.state.data?.deploy.status === "deploying" ? MOVING_POLL_MS : SETTLED_POLL_MS,
  });
}

/** The project's components, in the design's order. */
export function useProjectComponents(projectName: string) {
  return useQuery({
    queryKey: deployKeys.components(projectName),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/components", {
        params: { path: { projectName } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Couldn't load the components"));
      return data.items ?? [];
    },
  });
}

/**
 * Every component's deployments, in every environment: one read per
 * component. A component whose read fails is left out and counted, so the
 * board draws what loaded.
 */
export function useComponentsDeployments(projectName: string, componentNames: string[]) {
  return useQueries({
    queries: componentNames.map((componentName) => ({
      queryKey: deployKeys.deployments(projectName, componentName),
      queryFn: async () => {
        const { data, error } = await client.GET("/projects/{projectName}/components/{componentName}/deployments", {
          params: { path: { projectName, componentName } },
        });
        if (error || data === undefined) throw new Error(apiErrorMessage(error, "Couldn't load the deployments"));
        return data.items ?? [];
      },
      refetchInterval: (query: { state: { data: Deployment[] | undefined } }) =>
        !query.state.data || deploymentsAreMoving(query.state.data) ? MOVING_POLL_MS : SETTLED_POLL_MS,
    })),
    combine: (results) => ({
      isPending: results.some((r) => r.isPending),
      deployments: results.flatMap((r) => r.data ?? []),
      failedCount: results.filter((r) => r.isError).length,
    }),
  });
}

async function readReadiness(projectName: string, environment: string): Promise<DependencyReadiness> {
  const { data, error } = await client.GET("/projects/{projectName}/dependencies/readiness", {
    params: { path: { projectName }, query: { environment } },
  });
  if (error || data === undefined) throw new Error(apiErrorMessage(error, "Couldn't load the dependencies' values"));
  return data;
}

/**
 * Whether each external dependency the project supplies has its values, per
 * environment: the deploy gate's own read, so the board and the gate agree.
 * Not polled: a save from the Configure card is what changes it, and it
 * refreshes this.
 */
export function useReadinessByEnvironment(projectName: string, environments: string[]) {
  return useQueries({
    queries: environments.map((environment) => ({
      queryKey: deployKeys.readiness(projectName, environment),
      queryFn: () => readReadiness(projectName, environment),
      staleTime: 30_000,
    })),
    combine: (results) =>
      new Map(environments.map((environment, i) => [environment, results[i]?.data] as const)),
  });
}

/** One environment's readiness, for its Configure card. */
export function useReadiness(projectName: string, environment: string) {
  return useQuery({
    queryKey: deployKeys.readiness(projectName, environment),
    queryFn: () => readReadiness(projectName, environment),
    staleTime: 30_000,
  });
}

/** Every component's dependencies as the design declares them, with their config keys. */
export function useDesignDependencies(projectName: string) {
  return useQuery({
    queryKey: deployKeys.designDependencies(projectName),
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/design/dependencies", {
        params: { path: { projectName } },
      });
      if (error) throw new Error(apiErrorMessage(error, "Couldn't load the design's dependencies"));
      return data ?? [];
    },
    staleTime: 30_000,
  });
}

/**
 * Give an external dependency its values in one environment. Write-only:
 * secrets go to the secret manager and nothing is echoed back. The platform
 * re-provisions the dependency with them.
 */
export function useSaveDependencyValues(projectName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ name, environment, values }: { name: string; environment: string; values: Record<string, string> }) => {
      const { error } = await client.POST("/projects/{projectName}/dependencies/external-resources/{name}/values", {
        params: { path: { projectName, name } },
        body: { environments: { [environment]: values } },
      });
      // On `error` alone: a success may come back with an empty body.
      if (error) throw new Error(apiErrorMessage(error, `Couldn't save ${name}'s values`));
    },
    onSuccess: (_data, { environment }) =>
      queryClient.invalidateQueries({ queryKey: deployKeys.readiness(projectName, environment) }),
  });
}

/** A service component's OpenAPI document, read only when it is shown. */
export function useComponentOpenApi(projectName: string, componentName: string, enabled: boolean) {
  return useQuery({
    queryKey: deployKeys.openapi(projectName, componentName),
    enabled,
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/components/{componentName}/openapi", {
        params: { path: { projectName, componentName } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Couldn't load the API"));
      return data.spec;
    },
    staleTime: 30_000,
  });
}

/** The project's roles view: its test users, and how a client outside it signs in. */
export function useProjectRoles(projectName: string, enabled: boolean) {
  return useQuery({
    queryKey: deployKeys.roles(projectName),
    enabled,
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/roles", {
        params: { path: { projectName } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, "Couldn't load the test users"));
      return data;
    },
  });
}

/**
 * A test user's password. A deliberate, momentary disclosure: a POST whose
 * answer is never cached.
 */
export function useRevealTestUserPassword(projectName: string) {
  return useMutation({
    mutationFn: async (username: string) => {
      const { data, error } = await client.POST("/projects/{projectName}/roles/test-users/{username}/reveal", {
        params: { path: { projectName, username } },
      });
      if (error || data === undefined) throw new Error(apiErrorMessage(error, `Couldn't read ${username}'s password`));
      return data.password;
    },
  });
}
