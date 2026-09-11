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
  buildDesignSection,
  componentOf,
  dependencyOf,
  followSelection,
  isDependencyDefinition,
  isFlow,
} from "./designTree";
import type { SpecFileEntry } from "./mapping";

// Full repo-relative paths, mirroring mapping.ts's current scheme
// (SpecFileEntry.path is the same specs/-prefixed path the Files API,
// collab doc, and agent writes all use — the unprefixed room-key scheme is
// retired). group is derived the same way mapping.toSpecEntry does.
const e = (path: string): SpecFileEntry => ({
  path,
  sha: path,
  group: path.startsWith("specs/requirements/")
    ? "requirements"
    : path.startsWith("specs/validation/")
      ? "validation"
      : "designs",
});

describe("componentOf", () => {
  it("extracts the component name from a component path", () => {
    expect(componentOf("specs/design/components/orders/design.json")).toBe("orders");
    expect(componentOf("specs/design/components/orders/wireframes.dsl")).toBe("orders");
  });
  it("returns null for non-component design paths", () => {
    expect(componentOf("specs/design/domain-model.md")).toBeNull();
    expect(componentOf("specs/requirements/prd.md")).toBeNull();
  });
});

describe("buildDesignSection", () => {
  it("splits overview files from per-component groups and finds the wireframe dsl", () => {
    const section = buildDesignSection([
      e("specs/design/domain-model.md"),
      e("specs/design/components/orders/design.json"),
      e("specs/design/components/orders/openapi.yaml"),
      e("specs/design/components/orders/wireframes.dsl"),
      e("specs/design/components/web/design.json"),
      e("specs/design/components/web/wireframes.dsl"),
      e("specs/requirements/prd.md"), // ignored: not a design file
    ]);

    expect(section.overview.map((f) => f.path)).toEqual(["specs/design/domain-model.md"]);
    expect(section.hasComponents).toBe(true);
    expect(section.components.map((c) => c.name)).toEqual(["orders", "web"]);

    const orders = section.components[0]!;
    // The raw .dsl is NOT listed as a browsable file; it drives the wireframe entry.
    expect(orders.files.map((f) => f.path)).toEqual([
      "specs/design/components/orders/design.json",
      "specs/design/components/orders/openapi.yaml",
    ]);
    expect(orders.wireframeDslPath).toBe("specs/design/components/orders/wireframes.dsl");
  });

  it("omits the wireframe entry when a component has no .dsl", () => {
    const section = buildDesignSection([e("specs/design/components/api/design.json")]);
    expect(section.components[0]!.wireframeDslPath).toBeNull();
  });

  it("returns empty section when there are no design files", () => {
    const section = buildDesignSection([e("specs/requirements/prd.md")]);
    expect(section.hasComponents).toBe(false);
    expect(section.hasCellDsl).toBe(false);
    expect(section.components).toEqual([]);
    expect(section.overview).toEqual([]);
  });

  it("keeps design.cell out of the overview and flags hasCellDsl", () => {
    const section = buildDesignSection([
      e("specs/design/design.cell"),
      e("specs/design/domain-model.md"),
    ]);
    // design.cell is rendered via the Architecture tab, never as a file row.
    expect(section.overview.map((f) => f.path)).toEqual(["specs/design/domain-model.md"]);
    expect(section.hasCellDsl).toBe(true);
  });

  it("flags hasCellDsl even before any component folders exist", () => {
    const section = buildDesignSection([e("specs/design/design.cell")]);
    expect(section.hasCellDsl).toBe(true);
    expect(section.hasComponents).toBe(false);
    expect(section.overview).toEqual([]);
  });

  it("flags hasSecurity only when security.json exists", () => {
    expect(
      buildDesignSection([e("specs/design/domain-model.md")]).hasSecurity,
    ).toBe(false);
    expect(
      buildDesignSection([e("specs/design/security.json")]).hasSecurity,
    ).toBe(true);
  });

  it("does not treat leftover security.md or roles.json as the Security entry", () => {
    const section = buildDesignSection([
      e("specs/design/domain-model.md"),
      e("specs/design/security.md"),
      e("specs/design/roles.json"),
    ]);
    expect(section.hasSecurity).toBe(false);
    expect(section.overview.map((f) => f.path)).toEqual([
      "specs/design/domain-model.md",
      "specs/design/roles.json",
      "specs/design/security.md",
    ]);
  });

  it("keeps security.json out of the overview list", () => {
    const section = buildDesignSection([
      e("specs/design/domain-model.md"),
      e("specs/design/security.json"),
    ]);
    expect(section.hasSecurity).toBe(true);
    expect(section.overview.map((f) => f.path)).toEqual(["specs/design/domain-model.md"]);
  });

  it("buckets flows into their own group, out of the overview rows, sorted by path", () => {
    const section = buildDesignSection([
      e("specs/design/domain-model.md"),
      e("specs/design/flows/view-order.md"),
      e("specs/design/flows/checkout.md"),
      e("specs/design/components/orders/design.json"),
    ]);
    expect(section.overview.map((f) => f.path)).toEqual(["specs/design/domain-model.md"]);
    expect(section.flows.map((f) => f.path)).toEqual([
      "specs/design/flows/checkout.md",
      "specs/design/flows/view-order.md",
    ]);
  });

  it("only a markdown file directly under flows/ is a flow", () => {
    expect(isFlow("specs/design/flows/checkout.md")).toBe(true);
    expect(isFlow("specs/design/flows/checkout.json")).toBe(false);
    expect(isFlow("specs/design/flows/nested/checkout.md")).toBe(false);
    expect(isFlow("specs/design/domain-model.md")).toBe(false);
  });
});

describe("dependencies — one directory, one definition", () => {
  it("extracts the dependency name from a dependency path", () => {
    expect(dependencyOf("specs/design/dependencies/stripe/dependency.json")).toBe("stripe");
    expect(dependencyOf("specs/design/dependencies/stripe/openapi.yaml")).toBe("stripe");
    expect(dependencyOf("specs/design/components/orders/design.json")).toBeNull();
  });

  it("groups a dependency's files under its node and keeps them out of the overview", () => {
    const section = buildDesignSection([
      e("specs/design/dependencies/stripe/dependency.json"),
      e("specs/design/dependencies/stripe/openapi.yaml"),
      e("specs/design/dependencies/sendgrid/dependency.json"),
      e("specs/design/domain-model.md"),
    ]);
    expect(section.overview.map((f) => f.path)).toEqual(["specs/design/domain-model.md"]);
    expect(section.dependencies.map((d) => d.name)).toEqual(["sendgrid", "stripe"]);
    // The definition leads its directory, whatever the alphabet says.
    expect(section.dependencies[1]!.files.map((f) => f.path)).toEqual([
      "specs/design/dependencies/stripe/dependency.json",
      "specs/design/dependencies/stripe/openapi.yaml",
    ]);
  });

  it("follows a dependency's files as files — the pane picks the renderer by path", () => {
    expect(followSelection("specs/design/dependencies/stripe/dependency.json")).toEqual({
      kind: "file",
      path: "specs/design/dependencies/stripe/dependency.json",
    });
    expect(followSelection("specs/design/dependencies/stripe/openapi.yaml")).toEqual({
      kind: "file",
      path: "specs/design/dependencies/stripe/openapi.yaml",
    });
    expect(isDependencyDefinition("specs/design/dependencies/stripe/dependency.json")).toBe(true);
    expect(isDependencyDefinition("specs/design/dependencies/stripe/sdk.json")).toBe(false);
    expect(isDependencyDefinition("specs/design/components/orders/design.json")).toBe(false);
  });
});
