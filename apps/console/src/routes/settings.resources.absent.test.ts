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

import { describe, expect, it } from "vitest";

// Vite-native checks (console tsconfig only includes vite/client types, so
// node:fs/path/url imports fail `tsc`). Same assertions as the plan: no
// settings.resources route file, and routeTree.gen.ts must not list
// '/settings/resources' (forbids both a page and a redirect).
const settingsResourcesRoute = import.meta.glob("./settings.resources.tsx");
const routeTreeRaw = import.meta.glob("../generated/routeTree.gen.ts", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const gen = Object.values(routeTreeRaw)[0] ?? "";

describe("/settings/resources", () => {
  it("is not a registered route (404, not a Resources page or redirect)", () => {
    expect(Object.keys(settingsResourcesRoute)).toHaveLength(0);

    expect(gen).not.toContain("'/settings/resources'");
    expect(gen).toContain("'/settings/credentials'");
    expect(gen).toContain("'/settings/skills'");
    expect(gen).toContain("'/settings/usage'");
    expect(gen).toContain("'/resources'");
  });
});
