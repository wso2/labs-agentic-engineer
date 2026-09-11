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
import { blockingDependencies } from "./blockingDependencies";

type PreflightItem = components["schemas"]["PreflightItem"];

const NO_PROVIDER: PreflightItem = {
  component: "orders-api",
  dependency: "currency-service",
  kind: "external-unresolved",
  description: "No provider chosen yet — choose which one to use.",
};
const SAME_DEPENDENCY_OTHER_COMPONENT: PreflightItem = {
  ...NO_PROVIDER,
  component: "reports-web",
};
const NO_INTERFACE: PreflightItem = {
  component: "orders-api",
  dependency: "tax-service",
  kind: "external-spec",
  description: "No interface yet — provide the API document to continue.",
};
const ORG_SERVICE: PreflightItem = {
  component: "orders-api",
  dependency: "billing-service",
  kind: "org-service",
  description: "Billing service endpoint",
};
// Collected on the Builds page and enforced at the deploy gate (ADR-0023), so
// it never holds a version back.
const CONFIG_VALUES: PreflightItem = {
  component: "orders-api",
  dependency: "stripe",
  kind: "external-config",
  description: "Stripe API credentials",
  config: [{ key: "STRIPE_API_KEY", secret: true }],
};
const PLATFORM_RESOURCE: PreflightItem = {
  component: "orders-api",
  dependency: "postgres-cnpg",
  kind: "platform-resource",
  description: "Postgres database",
  resourceType: "postgres-cnpg",
};

describe("blockingDependencies", () => {
  it("keeps only the kinds the design itself cannot answer", () => {
    const rows = blockingDependencies([
      NO_PROVIDER,
      NO_INTERFACE,
      ORG_SERVICE,
      CONFIG_VALUES,
      PLATFORM_RESOURCE,
    ]);
    expect(rows.map((r) => r.name)).toEqual([
      "billing-service",
      "currency-service",
      "tax-service",
    ]);
  });

  it("says a shared dependency once, and names who waits on it", () => {
    const rows = blockingDependencies([
      NO_PROVIDER,
      SAME_DEPENDENCY_OTHER_COMPONENT,
    ]);
    expect(rows).toHaveLength(1);
    expect(rows[0]!.usedBy).toEqual(["orders-api", "reports-web"]);
    expect(rows[0]!.description).toBe(NO_PROVIDER.description);
  });

  it("is empty when nothing blocks", () => {
    expect(blockingDependencies([CONFIG_VALUES, PLATFORM_RESOURCE])).toEqual([]);
  });
});
