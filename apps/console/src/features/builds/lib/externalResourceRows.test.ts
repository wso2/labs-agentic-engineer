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
import { externalResourceRows } from "./externalResourceRows";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];

describe("externalResourceRows", () => {
  it("projects one described card with the union of every consumer's keys", () => {
    const dependencies: ComponentDependencies[] = [
      {
        componentName: "checkout-api",
        dependencies: [
          {
            kind: "external",
            name: "stripe",
            description: "Payment provider",
            config: [
              { key: "REGION", secret: false, description: "Cloud region" },
              { key: "SHARED", secret: false },
            ],
          },
        ],
      },
      {
        componentName: "checkout-worker",
        dependencies: [
          {
            kind: "external",
            name: "stripe",
            config: [
              { key: "API_KEY", secret: true },
              { key: "SHARED", secret: true },
            ],
          },
        ],
      },
    ];

    expect(externalResourceRows(dependencies)).toEqual([
      {
        id: "external:stripe",
        name: "stripe",
        description: "Payment provider",
        config: [
          { key: "REGION", secret: false, description: "Cloud region" },
          { key: "SHARED", secret: true },
          { key: "API_KEY", secret: true },
        ],
      },
    ]);
  });

  it("merges case-variant names while keeping the first display name", () => {
    const dependencies: ComponentDependencies[] = [
      {
        componentName: "checkout-api",
        dependencies: [
          {
            kind: "external",
            name: "Stripe",
            config: [{ key: "REGION", secret: false }],
          },
        ],
      },
      {
        componentName: "checkout-worker",
        dependencies: [
          {
            kind: "external",
            name: "stripe",
            description: "Payment provider",
            config: [{ key: "API_KEY", secret: true }],
          },
        ],
      },
    ];

    expect(externalResourceRows(dependencies)).toEqual([
      {
        id: "external:stripe",
        name: "Stripe",
        description: "Payment provider",
        config: [
          { key: "REGION", secret: false },
          { key: "API_KEY", secret: true },
        ],
      },
    ]);
  });

  it("preserves a non-secret default from the design", () => {
    const dependencies: ComponentDependencies[] = [
      {
        componentName: "checkout-api",
        dependencies: [
          {
            kind: "external",
            name: "stripe",
            config: [
              { key: "REGION", secret: false, defaultValue: "us-east-1" },
            ],
          },
        ],
      },
    ];

    expect(externalResourceRows(dependencies)[0]?.config).toEqual([
      { key: "REGION", secret: false, defaultValue: "us-east-1" },
    ]);
  });

  it("excludes component and platform dependencies from Builds configuration", () => {
    const dependencies: ComponentDependencies[] = [
      {
        componentName: "checkout-api",
        dependencies: [
          { kind: "component", name: "catalog" },
          {
            kind: "platform-resource",
            name: "orders-db",
            resourceType: "postgres-cnpg",
          },
        ],
      },
    ];

    expect(externalResourceRows(dependencies)).toEqual([]);
  });
});
