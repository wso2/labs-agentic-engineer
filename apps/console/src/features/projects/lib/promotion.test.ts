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
import {
  allConnectionsSet,
  canPromote,
  configuredCount,
  connectionIsSet,
  connectionRows,
  seedValues,
} from "./promotion";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];

const deps: ComponentDependencies[] = [
  {
    componentName: "storefront",
    dependencies: [
      { kind: "component", name: "orders-api" },
      {
        kind: "platform-resource",
        name: "shop-auth",
        resourceType: "thunder-app",
        config: [
          { key: "TENANT_DOMAIN", defaultValue: "auth.dev" },
          { key: "CLIENT_SECRET", secret: true, defaultValue: "s3cret" },
        ],
      },
    ],
  },
  {
    componentName: "orders-api",
    dependencies: [
      { kind: "platform-resource", name: "shop-db", resourceType: "postgres-cnpg" },
      // The SAME shared auth app, declared again by its second consumer.
      {
        kind: "platform-resource",
        name: "shop-auth",
        resourceType: "thunder-app",
        config: [
          { key: "TENANT_DOMAIN", defaultValue: "auth.dev" },
          { key: "CLIENT_SECRET", secret: true, defaultValue: "s3cret" },
        ],
      },
      {
        kind: "external",
        name: "stripe",
        config: [{ key: "STRIPE_SECRET_KEY", secret: true }],
      },
    ],
  },
];

describe("connectionRows", () => {
  it("dedupes shared connections and drops component wiring", () => {
    const rows = connectionRows(deps);
    expect(rows.map((r) => r.name)).toEqual(["shop-auth", "shop-db", "stripe"]);
  });

  it("marks config-less connections as platform-provisioned", () => {
    const rows = connectionRows(deps);
    expect(rows.find((r) => r.name === "shop-db")?.provisioned).toBe(true);
    expect(rows.find((r) => r.name === "stripe")?.provisioned).toBe(false);
  });

  it("keeps the original first-row schema for a shared production connection", () => {
    const rows = connectionRows([
      {
        componentName: "checkout-api",
        dependencies: [
          {
            kind: "external",
            name: "stripe",
            description: "First declaration",
            config: [{ key: "FIRST_KEY", secret: false }],
          },
        ],
      },
      {
        componentName: "checkout-worker",
        dependencies: [
          {
            kind: "external",
            name: "stripe",
            description: "Second declaration",
            config: [{ key: "SECOND_KEY", secret: true }],
          },
        ],
      },
    ]);

    expect(rows).toHaveLength(1);
    expect(rows[0]?.config).toEqual([{ key: "FIRST_KEY", secret: false }]);
    expect(rows[0]).not.toHaveProperty("description");
  });
});

describe("readiness over entered values", () => {
  const rows = connectionRows(deps);

  it("seeds values from config defaults, so a defaulted connection is set", () => {
    const values = seedValues(rows);
    const auth = rows.find((r) => r.name === "shop-auth")!;
    expect(connectionIsSet(auth, values)).toBe(true);
  });

  it("counts provisioned + set connections; the gate wants all of them", () => {
    const values = seedValues(rows);
    // shop-db (provisioned) + shop-auth (defaults) = 2 of 3; stripe is open.
    expect(configuredCount(rows, values)).toBe(2);
    expect(allConnectionsSet(rows, values)).toBe(false);

    const stripe = rows.find((r) => r.name === "stripe")!;
    const done = { ...values, [stripe.id]: { STRIPE_SECRET_KEY: "sk_live_1" } };
    expect(allConnectionsSet(rows, done)).toBe(true);
  });

  it("ignores whitespace-only values", () => {
    const stripe = rows.find((r) => r.name === "stripe")!;
    expect(connectionIsSet(stripe, { [stripe.id]: { STRIPE_SECRET_KEY: "  " } })).toBe(
      false,
    );
  });
});

describe("canPromote", () => {
  it("requires a live dev deployment", () => {
    expect(canPromote({ status: "deploying", validation: "passed" })).toBe(false);
    expect(canPromote({ status: "deployed", validation: "passed" })).toBe(true);
  });

  it("blocks on an unearned or failing verdict, not on the absence of one", () => {
    for (const blocked of ["running", "awaiting-fix", "failed", "unreported"]) {
      expect(canPromote({ status: "deployed", validation: blocked })).toBe(false);
    }
    for (const open of ["none", "skipped", "passed", "partial", "inconclusive"]) {
      expect(canPromote({ status: "deployed", validation: open })).toBe(true);
    }
  });
});
