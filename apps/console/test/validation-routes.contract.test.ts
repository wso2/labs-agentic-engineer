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

// OUTSIDE src/ for the same reason the fixture contract test is: it reads a
// file, and `tsconfig.json` pins `types` to `vite/client` precisely so app code
// cannot reach for node's APIs and still typecheck.

import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const CONSOLE = resolve(dirname(fileURLToPath(import.meta.url)), "..");

/**
 * Validations' URL contract, read off the tree the router is actually built
 * from — the route modules themselves carry only what `createFileRoute` was
 * handed, so their paths are not assertable in isolation.
 *
 * The URL follows the name: `/validations`, plural, beside `/builds` and
 * `/deployments` and matching the API path the page reads. The singular
 * `/validation` — first a page pinned to the newest milestone, then the
 * ledger — was deliberately not kept: nothing outside the console ever linked
 * to it, and a redirect would exist only for a developer's own bookmarks.
 */
describe("the validation routes", () => {
  const tree = readFileSync(join(CONSOLE, "src/generated/routeTree.gen.ts"), "utf8");

  it("serves the ledger and the per-version page under /validations", () => {
    expect(tree).toContain("/projects/$projectName/validations/");
    expect(tree).toContain("/projects/$projectName/validations/$tag");
  });

  // Exact route ids, so `/validations/` cannot satisfy them.
  it("serves nothing at the singular address", () => {
    expect(tree).not.toContain("'/validation/'");
    expect(tree).not.toContain("'/validation/$tag'");
    expect(tree).not.toContain("'/projects/$projectName/validation'");
  });
});
