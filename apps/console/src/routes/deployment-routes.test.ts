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

import { describe, expect, it, vi } from "vitest";

// URL semantics only — the pages are stubbed out (see builds-routes.test.ts).
vi.mock("../features/projects/components/DeploymentEnvironmentPage", () => ({
  DeploymentEnvironmentPage: () => null,
}));
import { Route as environmentIndexRoute } from "./projects.$projectName.deployments.$environment.index";

// Vite-native absence checks (the console tsconfig only carries vite/client
// types, so node:fs would fail `tsc`) — same pattern as
// settings.resources.absent.test.ts.
const tryOutRouteFile = import.meta.glob(
  "./projects.$projectName.deployments.$environment.try-out.tsx",
);
const versionRouteFile = import.meta.glob(
  "./projects.$projectName.deployments.$environment.$version.tsx",
);
const routeTreeRaw = import.meta.glob("../generated/routeTree.gen.ts", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const gen = Object.values(routeTreeRaw)[0] ?? "";

/**
 * The routing half of §6 — the environment's own page. `/deployments/$environment`
 * used to redirect to a Try Out page; the environment page now lives AT that URL,
 * with Try Out as one of its four sections. The bare URL is what every card, link
 * and bookmark already points at, so it must serve the page itself.
 */
describe("deployment routes (§6)", () => {
  it("serves the environment page at the bare environment URL, with no redirect", () => {
    expect(environmentIndexRoute.options.component).toBeDefined();
    expect(environmentIndexRoute.options.beforeLoad).toBeUndefined();
  });

  it("no longer knows a try-out URL", () => {
    expect(Object.keys(tryOutRouteFile)).toHaveLength(0);
    expect(gen).not.toContain("try-out");
    expect(gen).toContain("'/projects/$projectName/deployments/$environment/'");
  });

  it("knows no per-version URL — a superseded version is a row, not a page", () => {
    expect(Object.keys(versionRouteFile)).toHaveLength(0);
    expect(gen).not.toContain("deployments/$environment/$version");
  });
});
