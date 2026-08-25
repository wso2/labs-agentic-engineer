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
import { deploymentKeys } from "./keys";
import { isDeploymentLive } from "../lib/status";

// The deployment list is a DB read on the server (the record, not the cluster),
// so a 10s poll while something is moving is affordable. It stops the moment
// nothing is deploying or validating — a settled record cannot change.
const DEPLOYMENTS_POLL_MS = 10_000;

/** The console's default log window, matching the picker's "Last 1h". */
export const DEFAULT_LOG_WINDOW_SECONDS = 3600;

/**
 * Every deployment across every environment, newest first (ADR-0020 §5).
 *
 * This is the whole Deployments page's data: both environment cards derive from
 * it (`currentDeployment`) and the table lists it. One read rather than one per
 * environment, so the two cards can never disagree about what is running.
 */
export function useProjectDeployments(
  projectName: string,
  options: { environment?: string; days?: number } = {},
) {
  const { environment, days } = options;
  return useQuery({
    queryKey: deploymentKeys.list(projectName, environment, days),
    queryFn: async () => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/deployments",
        {
          params: {
            path: { projectName },
            query: {
              ...(environment ? { environment } : {}),
              ...(days ? { days } : {}),
            },
          },
        },
      );
      if (error || data === undefined) {
        throw new Error(apiErrorMessage(error, "Failed to load deployments"));
      }
      return data.items ?? [];
    },
    refetchInterval: (query) => {
      const items = query.state.data;
      if (!items) return DEPLOYMENTS_POLL_MS; // no data yet (or errored) — keep trying
      return items.some(isDeploymentLive) ? DEPLOYMENTS_POLL_MS : false;
    },
  });
}

/**
 * One deployment and its components.
 *
 * Polls while the deployment itself is moving; a `live` or `superseded` record
 * is history and settles immediately.
 */
export function useProjectDeployment(
  projectName: string,
  deploymentId: string | undefined,
) {
  return useQuery({
    queryKey: deploymentKeys.detail(projectName, deploymentId ?? ""),
    enabled: Boolean(deploymentId),
    queryFn: async () => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/deployments/{deploymentId}",
        {
          params: { path: { projectName, deploymentId: deploymentId ?? "" } },
        },
      );
      if (error || data === undefined) {
        throw new Error(apiErrorMessage(error, "Failed to load the deployment"));
      }
      return data;
    },
    refetchInterval: (query) => {
      const detail = query.state.data;
      if (!detail) return DEPLOYMENTS_POLL_MS;
      return isDeploymentLive(detail.deployment) ? DEPLOYMENTS_POLL_MS : false;
    },
  });
}

/**
 * A running component's log over a bounded window.
 *
 * Deliberately NOT a stream (ADR-0020 §8) — the window is the control the reader
 * is given. `enabled` is what keeps this off the wire until a component row is
 * actually expanded: a deployment with six components must not fire six log
 * reads on mount.
 *
 * `retry: false` because this endpoint lands with the backend, not with the
 * frontend: while it is missing, one 404 is the answer, and three retries per
 * expanded row is just noise. The caller renders the absence as a note.
 */
export function useRuntimeLogs(
  projectName: string,
  componentName: string | undefined,
  environment: string | undefined,
  windowSeconds: number = DEFAULT_LOG_WINDOW_SECONDS,
  enabled = true,
) {
  return useQuery({
    queryKey: deploymentKeys.runtimeLog(
      projectName,
      componentName ?? "",
      environment ?? "",
      windowSeconds,
    ),
    enabled: enabled && Boolean(componentName) && Boolean(environment),
    retry: false,
    queryFn: async () => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/components/{componentName}/runtime-logs",
        {
          params: {
            path: { projectName, componentName: componentName ?? "" },
            query: { environment: environment ?? "", windowSeconds },
          },
        },
      );
      if (error || data === undefined) {
        throw new Error(apiErrorMessage(error, "Failed to load the runtime log"));
      }
      return data;
    },
  });
}
