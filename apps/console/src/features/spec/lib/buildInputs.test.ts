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
import { approvalInputsFor } from "./buildInputs";

type PreflightItem = components["schemas"]["PreflightItem"];

describe("approvalInputsFor", () => {
  it("emits an approved platform-resource input carrying the design's parameters", () => {
    const items: PreflightItem[] = [
      {
        component: "checkout-api",
        dependency: "orders-db",
        kind: "platform-resource",
        description: "Postgres database",
        resourceType: "postgres-cnpg",
        parameters: { instances: 1 },
      },
    ];

    expect(approvalInputsFor(items)).toEqual([
      {
        component: "checkout-api",
        dependency: "orders-db",
        kind: "platform-resource",
        approved: true,
        parameters: { instances: 1 },
      },
    ]);
  });

  it("omits `parameters` entirely when the design authored none", () => {
    const items: PreflightItem[] = [
      {
        component: "checkout-api",
        dependency: "cache",
        kind: "platform-resource",
        description: "Redis",
        resourceType: "redis",
      },
    ];

    expect(approvalInputsFor(items)[0]).not.toHaveProperty("parameters");
  });

  it("emits an approved org-service input", () => {
    const items: PreflightItem[] = [
      {
        component: "checkout-api",
        dependency: "billing",
        kind: "org-service",
        description: "Billing endpoint",
      },
    ];

    expect(approvalInputsFor(items)).toEqual([
      {
        component: "checkout-api",
        dependency: "billing",
        kind: "org-service",
        approved: true,
      },
    ]);
  });

  // Values are collected on the Builds page and enforced at the deploy gate;
  // the BFF ignores any external-config entry a build request carries, so the
  // console must not pretend to send one.
  it("drops external-config items", () => {
    const items: PreflightItem[] = [
      {
        component: "checkout-api",
        dependency: "stripe",
        kind: "external-config",
        description: "Stripe credentials",
        config: [{ key: "STRIPE_KEY", secret: true }],
      },
    ];

    expect(approvalInputsFor(items)).toEqual([]);
  });

  it("drops resolution blockers — there is no input that represents one", () => {
    const items: PreflightItem[] = [
      {
        component: "checkout-api",
        dependency: "crm",
        kind: "external-unresolved",
        description: "No provider chosen yet — choose which one to use.",
      },
      {
        component: "checkout-api",
        dependency: "weather-api",
        kind: "external-unresolved",
        description: "Needs information only you can provide.",
      },
      {
        component: "checkout-api",
        dependency: "partner-api",
        kind: "external-spec",
        description: "No API spec yet.",
      },
    ];

    expect(approvalInputsFor(items)).toEqual([]);
  });

  it("emits one input per consuming component of a shared dependency", () => {
    const items: PreflightItem[] = [
      {
        component: "web-frontend",
        dependency: "thunder-app",
        kind: "platform-resource",
        description: "End-user authentication",
        resourceType: "auth",
      },
      {
        component: "auth-service",
        dependency: "thunder-app",
        kind: "platform-resource",
        description: "End-user authentication",
        resourceType: "auth",
      },
    ];

    expect(approvalInputsFor(items).map((i) => i.component)).toEqual([
      "web-frontend",
      "auth-service",
    ]);
  });
});
