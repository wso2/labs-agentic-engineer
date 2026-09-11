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

// One external dependency, one state. The dependencies read model is per
// component (every consumer carries the hydrated edge), but the dependency's
// definition is one file, so its state is one answer — this folds the
// per-component rows into one entry per name, for the rail, the page and the
// Build drawer to read alike.

import type { components } from "../../../generated/aep-api";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type Dependency = components["schemas"]["Dependency"];

export interface DependencyState {
  /** The hydrated edge — identical across consumers, taken from the first. */
  dependency: Dependency;
  /** Every component that references it, sorted. */
  usedBy: string[];
  /** True while the build gate would refuse the version for this dependency. */
  blocking: boolean;
  /** What the user must do, in their words; empty when resolved. */
  todo: string;
  /** Qualifiers on a resolved dependency, in the words the page shows. */
  flags: string[];
}

const FLAG_LABEL: Record<string, string> = {
  registered: "Registered",
  assumed: "Assumed",
  derived: "Derived from docs",
  "sdk-only": "SDK only",
};

/** The one thing the user must do for an external dependency, or "". */
export function dependencyTodo(dep: Dependency): string {
  if (dep.kind !== "external") return "";
  switch (dep.status) {
    case "unresolved":
      switch (dep.reason) {
        case "needs-contract":
          return "Needs a contract";
        case "needs-acceptance":
          return "Needs your acceptance";
        default:
          // No provider chosen yet — the definition offers Select a provider.
          return "Choose a provider";
      }
    default:
      return "";
  }
}

export function flagLabels(dep: Dependency): string[] {
  return (dep.flags ?? []).map((f) => FLAG_LABEL[f] ?? f);
}

export function computeDependencyStates(
  all: ComponentDependencies[],
): Record<string, DependencyState> {
  const out: Record<string, DependencyState> = {};
  for (const comp of all) {
    for (const dep of comp.dependencies ?? []) {
      if (dep.kind !== "external") continue;
      const existing = out[dep.name];
      if (existing) {
        if (!existing.usedBy.includes(comp.componentName)) {
          existing.usedBy.push(comp.componentName);
          existing.usedBy.sort();
        }
        continue;
      }
      const todo = dependencyTodo(dep);
      out[dep.name] = {
        dependency: dep,
        usedBy: [comp.componentName],
        blocking: todo !== "",
        todo,
        flags: flagLabels(dep),
      };
    }
  }
  return out;
}
