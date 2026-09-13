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
import type { components } from "../../../generated/aep-api";
import { componentsWithoutSignIn } from "./signInlessComponents";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];

const signIn = {
  kind: "platform-resource",
  name: "shop-auth",
  resourceType: "thunder-app",
};

function comp(
  componentName: string,
  dependencies: ComponentDependencies["dependencies"],
): ComponentDependencies {
  return { componentName, dependencies };
}

describe("componentsWithoutSignIn", () => {
  it("names the components with no sign-in dependency", () => {
    expect(
      componentsWithoutSignIn([
        comp("storefront", [signIn]),
        comp("docs-site", []),
      ]),
    ).toEqual(["docs-site"]);
  });

  it("reads a null dependency list as no dependencies", () => {
    expect(componentsWithoutSignIn([comp("docs-site", null)])).toEqual([
      "docs-site",
    ]);
  });

  // The whole point of the rule: a project where sign-in is declared on the
  // SPA and on every protected service has nothing open to everyone.
  it("returns nothing when every component declares sign-in", () => {
    expect(
      componentsWithoutSignIn([
        comp("storefront", [signIn]),
        comp("orders-api", [signIn]),
      ]),
    ).toEqual([]);
  });

  // A platform resource of another type is not sign-in, and neither is an
  // external dependency that happens to be named after one.
  it("does not count other dependencies as sign-in", () => {
    expect(
      componentsWithoutSignIn([
        comp("orders-api", [
          { kind: "platform-resource", name: "orders-db", resourceType: "postgres" },
          { kind: "external", name: "thunder-app" },
        ]),
      ]),
    ).toEqual(["orders-api"]);
  });

  it("keeps the design's order and returns [] for no components", () => {
    expect(
      componentsWithoutSignIn([
        comp("zeta", []),
        comp("alpha", []),
        comp("storefront", [signIn]),
      ]),
    ).toEqual(["zeta", "alpha"]);
    expect(componentsWithoutSignIn([])).toEqual([]);
  });
});
