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

import type { components } from "../generated/aep-api";

type Project = components["schemas"]["Project"];

// Projects made through New project in mock mode, with what the handlers need
// beyond the contract's Project: the repository name each claimed (the BFF
// conflicts on it, and Project carries none) and the prompt (the project
// conversation's first message). Kept in localStorage so a created project
// survives a reload instead of 404ing.
export interface CreatedProject {
  project: Project;
  repoName: string;
  prompt?: string;
}

const KEY = "aep:mock:createdProjects";

export function createdProjects(): CreatedProject[] {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as CreatedProject[]) : [];
  } catch {
    return [];
  }
}

export function recordCreatedProject(created: CreatedProject): void {
  try {
    localStorage.setItem(KEY, JSON.stringify([...createdProjects(), created]));
  } catch {
    /* quota: non-fatal in mock mode */
  }
}
