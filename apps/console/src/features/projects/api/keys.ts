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

export const projectKeys = {
  all: ["projects"] as const,
  lists: () => [...projectKeys.all, "list"] as const,
  list: (search: string, limit?: number) =>
    [...projectKeys.lists(), { search, limit }] as const,
  details: () => [...projectKeys.all, "detail"] as const,
  detail: (name: string) => [...projectKeys.details(), name] as const,
  status: (name: string) => [...projectKeys.detail(name), "status"] as const,
  components: (name: string) =>
    [...projectKeys.detail(name), "components"] as const,
  componentOpenapi: (name: string, component: string) =>
    [...projectKeys.components(name), component, "openapi"] as const,
  componentDeployments: (name: string, component: string) =>
    [...projectKeys.components(name), component, "deployments"] as const,
  tasks: (name: string) => [...projectKeys.detail(name), "tasks"] as const,
  tags: (name: string) => [...projectKeys.detail(name), "tags"] as const,
  buildPreflight: (name: string) =>
    [...projectKeys.detail(name), "build-preflight"] as const,
  workloadDependencies: (name: string) =>
    [...projectKeys.detail(name), "workload-dependencies"] as const,
};
