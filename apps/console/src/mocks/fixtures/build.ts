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

/**
 * What the Build click meets (#749). One fixture per state the two dialogs
 * have to hold, switched with `aep:mock:build`:
 *
 *   changes  the ordinary second build — a name to keep or replace, and a
 *            list with one row of each state (default)
 *   first    a project with no version yet: every row is new. Pair it with a
 *            project scenario that has cut no tag (`aep:mock:project=spec`) —
 *            the version field checks the tags read for a collision, so a
 *            project already holding `v1` refuses the suggested `v1`, which is
 *            the collision state rather than a first build
 *   rebuild  the spec tree has not moved: the field locks, the action rebuilds
 *   blocked  a dependency has no provider and one has no interface
 */

import type { components } from "../../generated/aep-api";

type BuildPreflight = components["schemas"]["BuildPreflight"];

export const BUILD_SCENARIOS = [
  "changes",
  "first",
  "rebuild",
  "blocked",
] as const;
export type BuildScenario = (typeof BUILD_SCENARIOS)[number];

/** Names already in use, so the field can refuse a collision before submit. */
export const mockVersionTags = ["v2", "v1"];

const CHANGES: BuildPreflight = {
  needsInput: false,
  needsResolution: false,
  items: [],
  currentVersion: "v2",
  suggestedVersion: "v3",
  specUnchanged: false,
  changes: [
    { name: "orders-api", kind: "component", state: "changed" },
    { name: "reports-web", kind: "component", state: "new" },
    { name: "currency-service", kind: "external", state: "changed" },
    { name: "legacy-mailer", kind: "external", state: "removed" },
    { name: "postgres-cnpg", kind: "platform-resource", state: "new" },
  ],
};

const FIRST: BuildPreflight = {
  needsInput: false,
  needsResolution: false,
  items: [],
  currentVersion: "",
  suggestedVersion: "v1",
  specUnchanged: false,
  changes: [
    { name: "orders-api", kind: "component", state: "new" },
    { name: "storefront-web", kind: "component", state: "new" },
    { name: "currency-service", kind: "external", state: "new" },
    { name: "postgres-cnpg", kind: "platform-resource", state: "new" },
  ],
};

const REBUILD: BuildPreflight = {
  needsInput: false,
  needsResolution: false,
  items: [],
  currentVersion: "v2",
  suggestedVersion: "v3",
  specUnchanged: true,
  changes: [],
};

const BLOCKED: BuildPreflight = {
  needsInput: true,
  needsResolution: true,
  currentVersion: "v2",
  suggestedVersion: "v3",
  specUnchanged: false,
  changes: [],
  items: [
    {
      component: "orders-api",
      dependency: "currency-service",
      kind: "external-unresolved",
      description: "No provider chosen yet — choose which one to use.",
    },
    {
      component: "reports-web",
      dependency: "currency-service",
      kind: "external-unresolved",
      description: "No provider chosen yet — choose which one to use.",
    },
    {
      component: "orders-api",
      dependency: "tax-service",
      kind: "external-spec",
      description: "No interface yet — provide the API document to continue.",
    },
    // Rides along in the request and never reaches the dialog: its values are
    // collected on the Builds page and enforced at the deploy gate.
    {
      component: "orders-api",
      dependency: "postgres-cnpg",
      kind: "platform-resource",
      description: "Provisioned when the build starts.",
      resourceType: "postgres-cnpg",
    },
  ],
};

export function buildPreflight(scenario: BuildScenario): BuildPreflight {
  switch (scenario) {
    case "first":
      return FIRST;
    case "rebuild":
      return REBUILD;
    case "blocked":
      return BLOCKED;
    default:
      return CHANGES;
  }
}
