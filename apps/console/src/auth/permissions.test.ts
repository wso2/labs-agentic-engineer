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
import { ALL_PERMISSIONS, permissionsFromScope } from "./permissions";

describe("permissionsFromScope", () => {
  it("keeps ae:* entries and drops the OIDC scopes beside them", () => {
    expect(
      permissionsFromScope("openid profile email ae:build ae:design-view"),
    ).toEqual(new Set(["ae:build", "ae:design-view"]));
  });

  it("survives the shapes a missing or malformed claim arrives in", () => {
    for (const scope of [undefined, null, "", "   ", 42, { ae: "build" }]) {
      expect(permissionsFromScope(scope)).toEqual(new Set());
    }
  });

  it("tolerates the separator Thunder actually uses being any run of space", () => {
    expect(permissionsFromScope("  ae:build \n ae:build-view  ")).toEqual(
      new Set(["ae:build", "ae:build-view"]),
    );
  });

  it("admits every key in the generated vocabulary", () => {
    expect(permissionsFromScope(ALL_PERMISSIONS.join(" "))).toEqual(
      new Set(ALL_PERMISSIONS),
    );
  });

  // The regression this filter exists to make impossible. A key the console's
  // vocabulary does not carry — a permission added to aeperms against a
  // console built before it — used to be discarded here, because the filter
  // was an allowlist keyed on ALL_PERMISSIONS. The BFF would allow the call
  // and the console would withhold the surface from everyone, silently. It
  // now passes through: unrecognized, unasked-about, and harmless.
  it("passes through an ae:* key this build does not know", () => {
    expect(permissionsFromScope("openid ae:build ae:not-yet-shipped")).toEqual(
      new Set(["ae:build", "ae:not-yet-shipped"]),
    );
  });

  // Prefix, not substring: a scope belonging to some other resource server
  // that merely mentions "ae:" is not an AE grant.
  it("does not admit a key that only contains the prefix", () => {
    expect(permissionsFromScope("other:ae:build maeve:read")).toEqual(new Set());
  });
});
