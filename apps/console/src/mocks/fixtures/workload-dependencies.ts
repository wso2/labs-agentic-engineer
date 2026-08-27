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

import type { components } from "../../generated/aep-api";

type WorkloadDependencyDTO = components["schemas"]["WorkloadDependencyDTO"];
type ApiError = components["schemas"]["Error"];

// Scenario switch (api-guidelines: mocks must produce empty AND error
// states). Default empty is honest — nothing is deployed until it is.
// Toggle in the browser devtools:
//   localStorage.setItem('aep:mock:workload-dependencies',
//     'empty' | 'some' | 'error')
export type WorkloadDependenciesScenario = "empty" | "some" | "error";

export const emptyWorkloadDependencies: WorkloadDependencyDTO[] = [];

// A platform resource, an external resource, and a cross-project
// org-service with provider coords (gym-tracker / gym-api).
export const someWorkloadDependencies: WorkloadDependencyDTO[] = [
  {
    kind: "resource",
    tag: "platform",
    ref: "postgres-cnpg",
    name: "postgres-cnpg",
  },
  {
    kind: "resource",
    tag: "external",
    ref: "stripe",
    name: "stripe",
  },
  {
    kind: "org-service",
    name: "gym-api",
    project: "gym-tracker",
    component: "gym-api",
  },
];

export const workloadDependenciesError: ApiError = {
  code: "internal_error",
  message: "Mock error scenario for workload dependencies",
};
