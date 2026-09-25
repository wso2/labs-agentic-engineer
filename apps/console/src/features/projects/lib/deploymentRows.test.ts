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
import {
  deploymentsAreMoving,
  groupDeploymentCards,
  statusKind,
} from "./deploymentRows";
import type { components } from "../../../generated/aep-api";

type Component = components["schemas"]["Component"];
type Deployment = components["schemas"]["Deployment"];

function component(name: string, displayName?: string): Component {
  return {
    name,
    displayName: displayName ?? name,
    description: "",
    type: "service",
    status: "active",
  };
}

const devReady: Deployment = {
  componentName: "catalog-api",
  environment: "development",
  status: "Ready",
  releaseName: "demo-catalog-abc",
  endpointUrl: "https://catalog.dev.example.io",
  createdAt: "2026-07-12T05:00:00Z",
};

const prodReady: Deployment = {
  componentName: "catalog-api",
  environment: "production",
  status: "Ready",
  releaseName: "demo-catalog-abc",
  endpointUrl: "https://catalog.example.io",
  createdAt: "2026-07-12T06:00:00Z",
};

describe("statusKind", () => {
  it("maps Ready to success", () => {
    expect(statusKind("Ready")).toBe("success");
  });

  it("maps the distinguished Undeployed value to undeployed", () => {
    expect(statusKind("Undeployed")).toBe("undeployed");
  });

  it("maps failure-ish reasons to error", () => {
    expect(statusKind("ReleaseFailed")).toBe("error");
    expect(statusKind("DeploymentError")).toBe("error");
  });

  it("maps anything else to transitional", () => {
    expect(statusKind("Progressing")).toBe("transitional");
    expect(statusKind("ResourcesCreated")).toBe("transitional");
  });

  it("maps absent status to unknown", () => {
    expect(statusKind(undefined)).toBe("unknown");
    expect(statusKind("")).toBe("unknown");
  });
});

describe("groupDeploymentCards", () => {
  // The pipeline's entry environment — where a build lands, and so the one
  // column that also accounts for components with nothing bound.
  const ENTRY = "development";

  it("routes bindings to their environment's column", () => {
    const board = groupDeploymentCards(
      [component("catalog-api", "Catalog API")],
      [devReady, prodReady],
      ENTRY,
    );
    const development = board.get("development") ?? [];
    const production = board.get("production") ?? [];
    expect(development).toHaveLength(1);
    expect(development[0]?.deployment?.environment).toBe("development");
    expect(production).toHaveLength(1);
    expect(production[0]?.deployment?.environment).toBe("production");
    expect(production[0]?.displayName).toBe("Catalog API");
  });

  it("gives every component an entry-environment card, greyed Not deployed when unbound", () => {
    const board = groupDeploymentCards(
      [component("storefront", "Storefront"), component("catalog-api")],
      [devReady],
      ENTRY,
    );
    const development = board.get("development") ?? [];
    expect(development).toHaveLength(2);
    const storefront = development.find((c) => c.componentName === "storefront");
    expect(storefront?.kind).toBe("notDeployed");
    expect(storefront?.deployment).toBeUndefined();
    expect(board.get("production")).toBeUndefined();
  });

  it("keeps a component deployed only in production visible as Not deployed in the entry environment", () => {
    const board = groupDeploymentCards([component("catalog-api")], [prodReady], ENTRY);
    expect(board.get("production")).toHaveLength(1);
    const development = board.get("development") ?? [];
    expect(development).toHaveLength(1);
    expect(development[0]?.kind).toBe("notDeployed");
  });

  it("gives an environment between the two its own column, not development's", () => {
    // Intent changed with the N-environment board (task 6): staging used to
    // be folded into the development column because the console knew only two
    // environments. It is now a column of its own.
    const staging: Deployment = {
      componentName: "catalog-api",
      environment: "staging",
      status: "Ready",
    };
    const board = groupDeploymentCards([component("catalog-api")], [devReady, staging], ENTRY);
    expect([...board.keys()]).toEqual(["development", "staging"]);
    expect(board.get("development")?.map((c) => c.deployment?.environment)).toEqual([
      "development",
    ]);
    expect(board.get("staging")?.map((c) => c.deployment?.environment)).toEqual(["staging"]);
  });

  it("holds only the bindings when no entry environment is named", () => {
    const board = groupDeploymentCards([component("catalog-api"), component("storefront")], [
      prodReady,
    ]);
    expect([...board.keys()]).toEqual(["production"]);
    expect(board.get("production")).toHaveLength(1);
  });

  it("shows deployments whose component is missing from the list", () => {
    const development = groupDeploymentCards([], [devReady], ENTRY).get("development") ?? [];
    expect(development).toHaveLength(1);
    expect(development[0]?.componentName).toBe("catalog-api");
    expect(development[0]?.displayName).toBe("catalog-api");
  });

  it("keeps the components list's own order, a stray binding last", () => {
    const board = groupDeploymentCards(
      [component("zeta"), component("alpha")],
      [
        { componentName: "alpha", environment: "development", status: "Ready" },
        { componentName: "gone", environment: "development", status: "Ready" },
        { componentName: "alpha", environment: "production", status: "Ready" },
        { componentName: "zeta", environment: "production", status: "Ready" },
      ],
      ENTRY,
    );
    expect(board.get("development")?.map((c) => c.componentName)).toEqual([
      "zeta",
      "alpha",
      "gone",
    ]);
    expect(board.get("production")?.map((c) => c.componentName)).toEqual(["zeta", "alpha"]);
  });

  it("marks intentionally undeployed bindings", () => {
    const board = groupDeploymentCards(
      [component("orders-api")],
      [
        {
          componentName: "orders-api",
          environment: "development",
          status: "Undeployed",
        },
      ],
      ENTRY,
    );
    expect(board.get("development")?.[0]?.kind).toBe("undeployed");
  });
});

describe("deploymentsAreMoving", () => {
  it("is true while any binding is transitional", () => {
    expect(
      deploymentsAreMoving([devReady, { ...devReady, status: "Progressing" }]),
    ).toBe(true);
  });

  it("is false when everything is settled (ready, failed, or undeployed)", () => {
    expect(
      deploymentsAreMoving([
        devReady,
        { ...devReady, status: "ReleaseFailed" },
        { ...devReady, status: "Undeployed" },
      ]),
    ).toBe(false);
  });

  it("treats unknown status as moving (still being evaluated)", () => {
    const noStatus: Deployment = {
      componentName: "catalog-api",
      environment: "development",
    };
    expect(deploymentsAreMoving([noStatus])).toBe(true);
  });

  it("is false for no deployments", () => {
    expect(deploymentsAreMoving([])).toBe(false);
    expect(deploymentsAreMoving(undefined)).toBe(false);
  });
});
