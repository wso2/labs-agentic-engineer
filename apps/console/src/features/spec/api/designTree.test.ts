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
  buildValidationSection,
  componentOf,
  dependencyOf,
  followSelection,
  isDependencyDefinition,
  isFlow,
  selectionKey,
} from "./designTree";
import type { RailPlanEntry } from "../lib/railSections";
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
    : path.startsWith("specs/validation/acceptance/")
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

describe("buildDesignSection — a component's artifacts lead with the overview", () => {
  // Path order is not reading order. `agent.afm.md` sorts above `design.json`
  // on path alone, so an ai-agent listed its Agent Spec before the Design
  // Overview while a service listed design.json first — by the accident of
  // "d" < "o". The overview is the document that says what the component IS,
  // so it leads for every type. Same reasoning the Requirements group already
  // applies to the PRD.
  it("puts Design Overview before an ai-agent's Agent Spec", () => {
    const section = buildDesignSection([
      { path: "specs/design/components/trip-agent/agent.afm.md", sha: "a", group: "designs" },
      { path: "specs/design/components/trip-agent/design.json", sha: "b", group: "designs" },
    ]);

    expect(section.components[0]?.files.map((f) => f.path)).toEqual([
      "specs/design/components/trip-agent/design.json",
      "specs/design/components/trip-agent/agent.afm.md",
    ]);
  });

  it("keeps Design Overview before a service's API Spec", () => {
    const section = buildDesignSection([
      { path: "specs/design/components/plants-api/openapi.yaml", sha: "a", group: "designs" },
      { path: "specs/design/components/plants-api/design.json", sha: "b", group: "designs" },
    ]);

    expect(section.components[0]?.files.map((f) => f.path)).toEqual([
      "specs/design/components/plants-api/design.json",
      "specs/design/components/plants-api/openapi.yaml",
    ]);
  });

  it("leaves anything else in path order behind them", () => {
    const section = buildDesignSection([
      { path: "specs/design/components/x/zebra.md", sha: "a", group: "designs" },
      { path: "specs/design/components/x/agent.afm.md", sha: "b", group: "designs" },
      { path: "specs/design/components/x/alpha.md", sha: "c", group: "designs" },
      { path: "specs/design/components/x/design.json", sha: "d", group: "designs" },
    ]);

    expect(section.components[0]?.files.map((f) => f.path)).toEqual([
      "specs/design/components/x/design.json",
      "specs/design/components/x/agent.afm.md",
      "specs/design/components/x/alpha.md",
      "specs/design/components/x/zebra.md",
    ]);
  });
});

// followSelection is the single definition of "where does a written path open",
// so the write-watcher can never land somewhere a rail click would not have
// gone. With no per-capability row, an acceptance path has to route to the set.
describe("followSelection — acceptance paths open the set", () => {
  it("routes any capability to the one entry", () => {
    expect(followSelection("specs/validation/acceptance/bought-items.feature")).toEqual({
      kind: "acceptance",
    });
    expect(followSelection("specs/validation/acceptance/adding-items.feature")).toEqual({
      kind: "acceptance",
    });
  });

  it("does not claim a nested or differently-suffixed path", () => {
    for (const path of [
      "specs/validation/acceptance/nested/deep.feature",
      "specs/validation/acceptance/notes.md",
      "specs/design/flows/checkout.feature",
    ]) {
      expect(followSelection(path), path).toEqual({ kind: "file", path });
    }
  });

  it("gives the set a stable identity of its own", () => {
    expect(selectionKey({ kind: "acceptance" })).toBe("acceptance");
    expect(selectionKey({ kind: "acceptance" })).not.toBe(
      selectionKey({ kind: "file", path: "specs/validation/acceptance/bought-items.feature" }),
    );
  });
});

// The Validation section is not just a file list: ONE rail entry stands for
// every specs/validation/acceptance/*.feature (ADR-0031). Which files keep an ordinary row
// and whether that standing-in entry appears are two halves of one fact, and
// deriving them apart is how they come to disagree — so they are derived here,
// together, and tested here rather than through the rail.
describe("buildValidationSection", () => {
  const file = (path: string): SpecFileEntry => ({ path, sha: "sha", group: "validation" });
  const ghost = (path: string): SpecFileEntry => ({ path, sha: "", group: "validation" });
  const BOUGHT = "specs/validation/acceptance/bought-items.feature";
  const ADDING = "specs/validation/acceptance/adding-items.feature";
  const planned = (path: string, status: RailPlanEntry["status"]): RailPlanEntry => ({
    path,
    status,
    section: "validation",
  });

  it("keeps the capabilities out of the rows and puts one entry in their place", () => {
    const section = buildValidationSection(
      [file(BOUGHT), file(ADDING)],
      new Set([BOUGHT, ADDING]),
      [],
    );
    expect(section.files).toEqual([]);
    expect(section.hasAcceptance).toBe(true);
  });

  // Only the oracle's folder reaches this group (mapping's ACCEPTANCE_PREFIX),
  // and inside it only `.feature` files are the capability set. Something else
  // in that same folder is therefore the one thing this filter still has to
  // spare, and it keeps its own row rather than being swallowed by the entry.
  it("leaves a validation document that is not a capability its own row", () => {
    const OTHER = "specs/validation/acceptance/README.md";
    const section = buildValidationSection(
      [file(OTHER), file(BOUGHT)],
      new Set([OTHER, BOUGHT]),
      [],
    );
    expect(section.files.map((f) => f.path)).toEqual([OTHER]);
    expect(section.hasAcceptance).toBe(true);
  });

  // The two halves agreeing is the whole point of deriving them together: every
  // path dropped from `files` must be covered by the entry, or files vanish from
  // the rail with nothing to catch it.
  it("covers everything it hides", () => {
    const all = [file(BOUGHT), file(ADDING)];
    const section = buildValidationSection(all, new Set(all.map((f) => f.path)), []);
    const hidden = all.filter((f) => !section.files.includes(f));
    expect(hidden.length).toBeGreaterThan(0);
    expect(section.hasAcceptance).toBe(true);
  });

  it("offers no entry for a project that has none", () => {
    const section = buildValidationSection([], new Set(), []);
    expect(section.hasAcceptance).toBe(false);
    expect(section.acceptanceStatusPath).toBeUndefined();
  });

  it("appears for a capability that is only planned, and not yet written", () => {
    const section = buildValidationSection(
      [ghost(BOUGHT)],
      new Set(),
      [planned(BOUGHT, "planned")],
    );
    expect(section.hasAcceptance).toBe(true);
    // A ghost: the entry selects nothing yet, so the rail disables it.
    expect(section.acceptanceStatusPath).toBe(BOUGHT);
  });

  // The case a refactor got wrong once: `allFiles` carries the ghosts, so
  // deriving "committed" from it counts a planned path as written and the entry
  // stops being a ghost while nothing exists at all.
  it("is a ghost only while NOT ONE capability is written", () => {
    const section = buildValidationSection(
      [file(BOUGHT), ghost(ADDING)],
      new Set([BOUGHT]),
      [planned(ADDING, "planned")],
    );
    // Two written and a third planned is a real entry, so no status path.
    expect(section.acceptanceStatusPath).toBeUndefined();
  });

  // `row` reads status from ONE path and this entry stands for many, so a write
  // anywhere in the set pulses it.
  it("pulses while the agent writes any capability", () => {
    const section = buildValidationSection(
      [file(BOUGHT), ghost(ADDING)],
      new Set([BOUGHT]),
      [planned(BOUGHT, "done"), planned(ADDING, "writing")],
    );
    expect(section.acceptanceStatusPath).toBe(ADDING);
  });
});

