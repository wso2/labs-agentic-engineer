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

// Which components a visitor reaches without signing in.
//
// security.json v1 listed them by hand in `publicComponents`; v2 has no such
// field, and does not need one — the answer was always derivable from the
// architecture. `thunder-app` is the ONLY thing that provisions sign-in, and
// the architecture rule is to declare it on the SPA and on every protected
// service under one shared name (skills/architecture/SKILL.md). A component
// that declares no such dependency therefore has no sign-in to offer, and
// whatever it serves it serves to everyone.
//
// The data is the Deployments page's own design-dependencies read — no new
// contract surface, and no second opinion about a component's dependencies.

import type { components } from "../../../generated/aep-api";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type Dependency = components["schemas"]["Dependency"];

/** The platform resource type that provisions an end-user sign-in client. */
const SIGN_IN_RESOURCE_TYPE = "thunder-app";

function isSignIn(dep: Dependency): boolean {
  return (
    dep.kind === "platform-resource" && dep.resourceType === SIGN_IN_RESOURCE_TYPE
  );
}

/**
 * The components with no sign-in dependency, in the order the design declares
 * them. Deliberately unsorted: this reads as a list of the project's own
 * components, and the design's order is the one a reader recognises.
 */
export function componentsWithoutSignIn(
  all: readonly ComponentDependencies[],
): string[] {
  return all
    .filter((c) => !(c.dependencies ?? []).some(isSignIn))
    .map((c) => c.componentName);
}
