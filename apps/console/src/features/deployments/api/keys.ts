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

export const deploymentKeys = {
  all: (projectName: string) =>
    ["projects", "detail", projectName, "deployments"] as const,
  /** Every deployment across every environment — the board and its table. */
  list: (projectName: string, environment?: string, days?: number) =>
    [
      ...deploymentKeys.all(projectName),
      "list",
      environment ?? "all",
      days ?? 30,
    ] as const,
  /** One deployment and the components it put in the environment. */
  detail: (projectName: string, deploymentId: string) =>
    [...deploymentKeys.all(projectName), "detail", deploymentId] as const,
  /** A running component's log over a window. Keyed by the window, so
   *  changing the picker is a new query rather than a refetch that
   *  discards the cached one. */
  runtimeLog: (
    projectName: string,
    componentName: string,
    environment: string,
    windowSeconds: number,
  ) =>
    [
      ...deploymentKeys.all(projectName),
      "runtime-log",
      componentName,
      environment,
      windowSeconds,
    ] as const,
};
