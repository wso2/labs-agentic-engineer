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

// Cross-component "Used by" for the design.json Spec cards (#252 Task 15).
// Task 14 lifted the ComponentType!=service preflight guard, surfacing the
// SAME dependency-duplication regression the build dependency drawer had: a
// project-scoped shared dependency (the canonical case: `thunder-app`
// end-user auth, declared identically on both a web-application and its
// backing service) is declared independently on each component's own
// design.json. Unlike the drawer (one project-wide items list, so the
// duplicate literally renders twice in one list), DesignView only ever
// renders ONE component's design.json at a time — there is no single screen
// where the same card appears twice today. What this DOES fix: viewing one
// component's card for a shared dependency with no indication another
// component ALSO depends on it, which is the same "surprise duplication"
// risk in a different shape. computeDependencyUsedBy, applied at SpecView's
// aggregation layer (it already fetches every component's dependencies via
// useDesignDependencies), annotates the CURRENTLY VIEWED component's cards
// with every other component sharing the same dependency. Equivalence uses
// the dependency kind plus identity (`resourceType`+`name` for a platform
// resource, `name` alone otherwise).

import type { components } from "../../../generated/aep-api";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type Dependency = components["schemas"]["Dependency"];

function dependencyIdentity(dep: Dependency): string {
  return dep.kind === "platform-resource"
    ? `platform-resource:${dep.resourceType ?? ""}:${dep.name}`
    : `${dep.kind}:${dep.name}`;
}

/**
 * For `componentName`'s own dependency list, returns the sorted list of
 * EVERY component (including `componentName` itself) that declares an
 * equivalent dependency — keyed by `componentName`'s own dependency `name`
 * (safe: names are unique within one component's own declared list). A
 * dependency only `componentName` declares gets no entry, so
 * `usedBy[dep.name]` is exactly the signal for "render a Used-by line".
 */
export function computeDependencyUsedBy(
  all: ComponentDependencies[],
  componentName: string,
): Record<string, string[]> {
  const consumersByIdentity = new Map<string, Set<string>>();
  for (const comp of all) {
    for (const dep of comp.dependencies ?? []) {
      const key = dependencyIdentity(dep);
      const consumers = consumersByIdentity.get(key) ?? new Set<string>();
      consumers.add(comp.componentName);
      consumersByIdentity.set(key, consumers);
    }
  }

  const own =
    all.find((c) => c.componentName === componentName)?.dependencies ?? [];
  const result: Record<string, string[]> = {};
  for (const dep of own) {
    const consumers = consumersByIdentity.get(dependencyIdentity(dep));
    if (consumers && consumers.size > 1) {
      result[dep.name] = [...consumers].sort();
    }
  }
  return result;
}
