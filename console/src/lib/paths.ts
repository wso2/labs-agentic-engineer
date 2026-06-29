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

export function organizationOverviewPath(orgId: string): string {
  return `/organizations/${orgId}`;
}

export function projectCreatePath(orgId: string): string {
  return `/organizations/${orgId}/projects/new`;
}

export function projectOverviewPath(orgId: string, projectId: string): string {
  return `/organizations/${orgId}/projects/${projectId}`;
}

export function projectRequirementsPath(orgId: string, projectId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/requirements`;
}

export function projectArchitecturePath(orgId: string, projectId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/architecture`;
}

export function projectSpecPath(orgId: string, projectId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/spec`;
}

/** @deprecated Design is now part of the spec wizard. Redirects to the spec path. */
export function projectDesignPath(orgId: string, projectId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/spec`;
}

export function projectTasksPath(orgId: string, projectId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/tasks`;
}

export function projectTaskDetailPath(orgId: string, projectId: string, taskId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/tasks/${taskId}`;
}

export function componentDetailPath(orgId: string, projectId: string, componentId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/components/${componentId}`;
}

export function componentBuildPath(orgId: string, projectId: string, componentId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/components/${componentId}/build`;
}

export function componentDeployPath(orgId: string, projectId: string, componentId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/components/${componentId}/deploy`;
}

export function componentConfigsPath(orgId: string, projectId: string, componentId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/components/${componentId}/configs`;
}

export function componentTestPath(orgId: string, projectId: string, componentId: string): string {
  return `/organizations/${orgId}/projects/${projectId}/components/${componentId}/test`;
}
