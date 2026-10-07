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
import type { ProductWideItem } from "../../spec/api/specModel";
import type { LineBlock } from "../../spec/model/ids";
import type { ProjectBuild } from "../api/builds";
import { builtLines } from "./changes";
import {
  buildOffer,
  canStart,
  defaultSelection,
  nextUpBuild,
  pickerSummary,
  pickWarnings,
  selectionNames,
  togglePick,
  type PickerInput,
} from "./picker";

const li = (text: string): LineBlock => ({ kind: "listItem", text, emphasis: [] });
const h1 = (text: string): LineBlock => ({ kind: "heading", level: 1, text, emphasis: [] });

const path = (id: string) => `specs/requirements/features/${id}.md`;
const PW = "specs/requirements/product-wide.md";

type Feature = PickerInput["features"][number];
const feature = (id: string, name: string, over: Partial<Feature> = {}): Feature => ({
  id,
  name,
  path: path(id),
  stage: "Designed",
  blocking: [],
  toConfirm: 0,
  designOutOfDate: false,
  ...over,
});

const question = { question: "One Xero organisation?", options: [] };

// Acme Expenses once Design has run: Submit expenses and Approvals designed,
// Payroll export held by a question and waiting on Xero, two stubs.
const features: Feature[] = [
  feature("F1", "Submit expenses"),
  feature("F2", "Approvals", { toConfirm: 2 }),
  feature("F3", "Payroll export", { stage: "Interviewed", blocking: [question] }),
  feature("F4", "Spending reports", { stage: "Not interviewed" }),
  feature("F5", "Mileage claims", { stage: "Not interviewed" }),
];

const productWide: ProductWideItem[] = [
  { id: "P1", appliesTo: "all" },
  { id: "P2", appliesTo: "all" },
  { id: "P3", appliesTo: ["F1", "F3"] },
  { id: "P4", appliesTo: "all" },
];

const lines = new Map<string, LineBlock[]>([
  [path("F1"), [h1("Submit expenses"), li("F1.1 I photograph a receipt.")]],
  [path("F2"), [h1("Approvals"), li("F2.1 I see pending claims."), li("F2.2 I approve with a reason.")]],
  [PW, [h1("Product-wide"), li("P1 Every approval is logged."), li("P5 An auditor can read every claim.")]],
]);

function input(over: Partial<PickerInput> = {}): PickerInput {
  return {
    features,
    designedFrom: { F1: "basis", F2: "basis" },
    productWide,
    lines,
    dependencies: [{ featureId: "F3", needs: "Xero", answer: null }],
    comments: [
      { artifactId: "flow-approve", status: "open" },
      { artifactId: "prototype", status: "addressed" },
      { artifactId: "prototype", status: "resolved" },
    ],
    artifacts: [
      { id: "flow-approve", features: ["F2"] },
      { id: "prototype", features: ["F1", "F2"] },
    ],
    builds: [],
    ...over,
  };
}

const row = (offer: ReturnType<typeof buildOffer>, id: string) => offer.rows.find((r) => r.id === id);

describe("a first build: which features can go in, and why not", () => {
  const offer = buildOffer(input());

  it("offers the designed features as new, with lines to confirm and comments open as warnings", () => {
    expect(offer.version).toBe("v1");
    expect(row(offer, "F1")).toMatchObject({ state: "offered", detail: ["new", "1 comment not resolved"], warn: true });
    expect(row(offer, "F2")).toMatchObject({
      state: "offered",
      detail: ["new", "2 to confirm, builds as assumed", "2 comments not resolved"],
      warn: true,
    });
  });

  it("holds a feature waiting on something outside the product, saying what, before its question", () => {
    expect(row(offer, "F3")).toMatchObject({ state: "disabled", detail: ["waiting on Xero", "blocked by a question"] });
  });

  it("offers no box for a feature not interviewed yet, only why", () => {
    expect(row(offer, "F4")).toMatchObject({ state: "fixed", detail: ["not interviewed yet"], warn: false });
  });

  it("holds a feature whose design is behind its spec, and one interviewed but not designed", () => {
    const later = buildOffer(
      input({
        features: [feature("F1", "Submit expenses", { stage: "Interviewed" }), feature("F2", "Approvals", { designOutOfDate: true })],
        designedFrom: { F2: "basis" },
      }),
    );
    expect(row(later, "F1")).toMatchObject({ state: "disabled", detail: ["not designed yet"] });
    expect(row(later, "F2")).toMatchObject({ state: "disabled", detail: ["design out of date"] });
  });

  it("offers a feature once its dependency is settled and it is designed", () => {
    const settled = buildOffer(
      input({
        features: [feature("F3", "Payroll export")],
        designedFrom: { F3: "basis" },
        dependencies: [{ featureId: "F3", needs: "Xero", answer: "Use Xero's published API" }],
      }),
    );
    expect(row(settled, "F3")).toMatchObject({ state: "offered", detail: ["new"] });
  });

  it("opens with every offered feature ticked, and starts only with a feature", () => {
    const selection = defaultSelection(offer);
    expect(selection).toEqual({ features: ["F1", "F2"], productWide: [] });
    expect(canStart(selection)).toBe(true);
    expect(canStart({ features: [], productWide: [] })).toBe(false);
  });

  it("ignores a tick on a row that cannot be picked", () => {
    const selection = defaultSelection(offer);
    expect(togglePick(offer, selection, "F3", true)).toBe(selection);
  });

  it("says what else the build runs, and that open comments do not block it", () => {
    expect(pickerSummary(offer)).toEqual([
      { label: "Foundation", text: "P1 · P2 · P4, built first, in v1" },
      { label: "Comments", text: "2 not resolved (this doesn't block the build)" },
      { label: "Validation", text: "every scenario of the picked features, tagged by story" },
    ]);
  });

  it("is Next up's build item while designed features wait, and names what it builds", () => {
    expect(nextUpBuild(offer)).toMatchObject({ kind: "build", label: "Build v1", why: "Submit expenses, Approvals" });
    expect(selectionNames(offer, defaultSelection(offer))).toEqual(["Submit expenses", "Approvals"]);
  });
});

describe("a later build: what changed since, and a new product-wide item", () => {
  const v1: ProjectBuild = {
    version: "v1",
    status: "built",
    features: [
      { id: "F1", name: "Submit expenses", lines: builtLines(lines.get(path("F1"))!) },
      { id: "F2", name: "Approvals", lines: builtLines(lines.get(path("F2"))!) },
    ],
    productWide: ["P1", "P2", "P3", "P4"],
  };
  const edited = new Map(lines);
  edited.set(path("F2"), [h1("Approvals"), li("F2.1 I see pending claims, oldest first."), li("F2.2 I approve with a reason.")]);
  const withP5: ProductWideItem[] = [...productWide, { id: "P5", appliesTo: ["F2", "F3"] }];
  const later = (over: Partial<PickerInput> = {}) =>
    buildOffer(input({ builds: [v1], lines: edited, productWide: withP5, comments: [], ...over }));

  it("says what changed in a built feature, and shows an unchanged one without a box", () => {
    const offer = later({ productWide });
    expect(offer.version).toBe("v2");
    expect(row(offer, "F2")).toMatchObject({ state: "offered", detail: ["changed since v1: 1 edited", "2 to confirm, builds as assumed"] });
    expect(row(offer, "F1")).toMatchObject({ state: "fixed", detail: ["built in v1, unchanged"] });
  });

  it("gives a new product-wide item its own row, first, pulling in the built features it applies to", () => {
    const offer = later();
    expect(offer.rows[0]).toMatchObject({
      kind: "product-wide",
      id: "P5",
      text: "An auditor can read every claim.",
      state: "offered",
      pullsIn: ["F2"],
      detail: ["pulls in: Approvals"],
    });
    expect(row(offer, "F2")?.detail[0]).toBe("changed since v1: 1 edited; affected by P5");
    expect(offer.foundation).toEqual(["P1", "P2", "P4"]);
  });

  it("ticks what the item pulls in, and warns when one of them is unticked", () => {
    const offer = later();
    let selection = togglePick(offer, { features: [], productWide: [] }, "P5", true);
    expect(selection).toEqual({ features: ["F2"], productWide: ["P5"] });
    expect(pickWarnings(offer, selection)).toEqual([]);
    selection = togglePick(offer, selection, "F2", false);
    expect(pickWarnings(offer, selection)).toEqual(["P5 would apply to only part of the product."]);
    expect(selectionNames(offer, togglePick(offer, selection, "F2", true))).toEqual(["P5", "Approvals"]);
  });

  it("holds the item while a built feature it applies to cannot be rebuilt, and says which", () => {
    const offer = later({ features: features.map((f) => (f.id === "F2" ? { ...f, designOutOfDate: true } : f)) });
    expect(offer.rows[0]).toMatchObject({ state: "disabled", detail: ["pulls in: Approvals", "Approvals: design out of date"] });
  });

  it("re-checks everything built so far, over a foundation built in v1", () => {
    expect(pickerSummary(later()).map((l) => l.text)).toEqual([
      "P1 · P2 · P4, built in v1",
      "all resolved",
      "re-checks everything built so far",
    ]);
  });

  it("numbers the next version past a repair build, which is a point release of what it fixed", () => {
    const v11: ProjectBuild = { ...v1, version: "v1.1", fixes: "v1" };
    const offer = later({ builds: [v1, v11], productWide });
    expect(offer.version).toBe("v2");
    expect(row(offer, "F1")).toMatchObject({ state: "fixed", detail: ["built in v1.1, unchanged"] });
  });

  it("offers nothing while a build runs", () => {
    expect(nextUpBuild(later({ builds: [{ ...v1, status: "building" }] }))).toBeNull();
  });
});

// Seen on the live walk: a v1 whose build failed (its planning never got past
// a gate) left nothing to pick — every feature read "built in v1, unchanged".
describe("after a version whose build failed", () => {
  it("offers its features again, under the same version, as a first build", () => {
    const failed = {
      version: "v1",
      status: "failed" as const,
      features: [{ id: "F1", name: "Submit expenses", lines: builtLines(lines.get(path("F1"))!) }],
      productWide: ["P1"],
    };
    const offer = buildOffer(input({ builds: [failed] }));
    expect(offer.version).toBe("v1");
    expect(offer.firstBuild).toBe(true);
    expect(row(offer, "F1")).toMatchObject({ state: "offered", detail: expect.arrayContaining(["new"]) });
  });
});
