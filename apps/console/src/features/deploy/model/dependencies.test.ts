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
import { configureDependencies, nameList, orgHeldDependencies, valuesComplete } from "./dependencies";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];

// Xero is declared by both components, each with some of its keys; Rates was
// copied from the organization's registered resource.
const design: ComponentDependencies[] = [
  {
    componentName: "expense-api",
    dependencies: [
      {
        kind: "external",
        name: "Xero",
        config: [
          { key: "clientId", description: "The app's client id" },
          { key: "clientSecret" },
        ],
      },
      { kind: "external", name: "Rates", resourceRef: "open-exchange-rates", config: [{ key: "appId" }] },
      { kind: "component", name: "expense-webapp" },
    ],
  },
  {
    componentName: "payroll-worker",
    dependencies: [
      {
        kind: "external",
        name: "xero",
        config: [
          { key: "clientSecret", secret: true },
          { key: "tenant" },
        ],
      },
    ],
  },
];

describe("configureDependencies", () => {
  it("asks for each dependency the project supplies once, its keys merged across the components that use it", () => {
    const readiness = { configured: false, dependencies: [{ name: "Xero", state: "unset" as const, missingKeys: ["tenant"] }] };
    expect(configureDependencies(readiness, design)).toEqual([
      {
        name: "Xero",
        state: "unset",
        keys: [
          { key: "clientId", description: "The app's client id" },
          { key: "clientSecret", secret: true },
          { key: "tenant" },
        ],
      },
    ]);
  });
});

describe("orgHeldDependencies", () => {
  it("names the dependencies whose values the organization holds", () => {
    expect(orgHeldDependencies(design)).toEqual(["Rates"]);
  });
});

describe("valuesComplete", () => {
  const keys = [{ key: "clientId" }, { key: "clientSecret", secret: true }];

  it("needs every value, since saving replaces them all", () => {
    expect(valuesComplete(keys, { clientId: "acme" })).toBe(false);
    expect(valuesComplete(keys, { clientId: "acme", clientSecret: "  " })).toBe(false);
    expect(valuesComplete(keys, { clientId: "acme", clientSecret: "s3cret" })).toBe(true);
  });
});

describe("nameList", () => {
  it("reads as a sentence", () => {
    expect(nameList(["Xero"])).toBe("Xero");
    expect(nameList(["Xero", "Stripe"])).toBe("Xero and Stripe");
    expect(nameList(["Xero", "Stripe", "Slack"])).toBe("Xero, Stripe and Slack");
  });
});
