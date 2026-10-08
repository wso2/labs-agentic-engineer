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
import type { SpecFeature } from "../api/specModel";
import type { LineBlock } from "./ids";
import {
  countAssumed,
  featureChips,
  isSmallProduct,
  nextUp,
  railState,
  sectionItems,
  withBuild,
  type FeatureView,
  type NextUpItem,
} from "./workspace";

function feature(id: string, name: string, over: Partial<FeatureView> = {}): FeatureView {
  const base: SpecFeature = { id, name, path: `specs/requirements/features/${id}.md`, purpose: "", stage: "Interviewed" };
  return { ...base, chips: [], blocking: [], toConfirm: 0, designOutOfDate: false, ...over };
}

const li = (text: string, emphasis: LineBlock["emphasis"] = []): LineBlock => ({ kind: "listItem", text, emphasis });
const h2 = (text: string): LineBlock => ({ kind: "heading", level: 2, text, emphasis: [] });

describe("countAssumed", () => {
  const tagged = "A rejected claim goes back. assumed";
  const tag = { start: tagged.indexOf("assumed"), end: tagged.length };

  it("counts the lines that close with the italic assumed tag", () => {
    expect(countAssumed([li(tagged, [tag]), li(tagged), li("F2.4 A deputy approves. assumed", [{ start: 24, end: 31 }])])).toBe(2);
  });

});

describe("sectionItems", () => {
  it("reads the entries under one heading, up to the next", () => {
    const lines = [h2("Features"), li("F1 Submit"), h2("Fog"), li("Corporate cards."), li("Per-diem rates."), h2("Out of Scope"), li("A mobile app.")];
    expect(sectionItems(lines, "Fog")).toEqual(["Corporate cards.", "Per-diem rates."]);
    expect(sectionItems(lines, "Nothing")).toEqual([]);
  });
});

describe("chips and rail state", () => {
  it("marks a blocked feature and one with lines to confirm", () => {
    expect(featureChips(true, 2)).toEqual([
      { tone: "warning", label: "blocked" },
      { tone: "warning", label: "2 to confirm" },
    ]);
    expect(featureChips(false, 0)).toEqual([]);
  });

  it("marks a design that is out of date", () => {
    expect(featureChips(false, 0, true)).toEqual([{ tone: "primary", label: "design out of date" }]);
  });

  it("shows the first chip in the rail, else the stage", () => {
    expect(railState({ stage: "Interviewed", chips: featureChips(true, 0) })).toEqual({ tone: "warning", label: "blocked" });
    expect(railState({ stage: "Interviewed", chips: [] })).toEqual({ tone: null, label: "interviewed" });
    expect(railState({ stage: "Not interviewed", chips: [] })).toEqual({ tone: null, label: "not yet" });
    expect(railState({ stage: "Designed", chips: [] })).toEqual({ tone: "success", label: "designed" });
  });
});

describe("nextUp", () => {
  const features = [
    feature("F1", "Submit expenses"),
    feature("F2", "Approvals", { toConfirm: 2 }),
    feature("F3", "Payroll export", { blocking: [{ question: "One Xero organisation?", options: [] }] }),
    feature("F4", "Spending reports", { stage: "Not interviewed" }),
    feature("F5", "Mileage claims", { stage: "Not interviewed", blocking: [{ question: "Rate per mile?", options: [] }] }),
  ];

  it("orders blocking questions, lines to confirm, interviews, design, comments, then Fog", () => {
    const items = nextUp({
      features,
      design: { toDesign: ["F1", "F2"], label: "Design 2 features" },
      openComments: 1,
      fog: ["Corporate cards."],
    });
    expect(items.map((i) => [i.kind, i.label])).toEqual([
      ["blocking", 'Answer "One Xero organisation?"'],
      ["blocking", 'Answer "Rate per mile?"'],
      ["confirm", "Confirm 2 lines in Approvals"],
      ["interview", "Interview Spending reports"],
      ["design", "Design 2 features"],
      ["comments", "Address 1 comment"],
      ["fog", 'Shape "Corporate cards."'],
    ]);
  });

  it("sends each item where the work is", () => {
    const items = nextUp({
      features,
      design: { toDesign: ["F1"], label: "Design 1 feature" },
      openComments: 0,
      fog: ["Corporate cards."],
    });
    expect(items.find((i) => i.kind === "blocking")?.target).toEqual({ card: "spec", file: "F3", at: "blocking" });
    expect(items.find((i) => i.kind === "confirm")?.target).toEqual({ card: "spec", file: "F2", at: "assumed" });
    expect(items.find((i) => i.kind === "interview")?.target).toEqual({ card: "spec", file: "F4" });
    expect(items.find((i) => i.kind === "design")).toMatchObject({ why: "Submit expenses", target: { card: "design" } });
    expect(items.find((i) => i.kind === "fog")?.target).toEqual({ card: "spec", file: "prd", at: "fog" });
  });

  it("puts the build in after everything but the Fog", () => {
    const items = nextUp({
      features,
      design: { toDesign: [], label: null },
      openComments: 1,
      fog: ["Corporate cards."],
    });
    const build: NextUpItem = { kind: "build", label: "Build v1", why: "Submit expenses", urgent: false, target: { card: "builds" } };
    expect(withBuild(items, build).map((i) => i.kind).slice(-3)).toEqual(["comments", "build", "fog"]);
    expect(withBuild(items.filter((i) => i.kind !== "fog"), build).at(-1)).toBe(build);
    expect(withBuild(items, null)).toBe(items);
  });

  it("marks only what blocks work as urgent", () => {
    const items = nextUp({ features, design: { toDesign: [], label: null }, openComments: 0, fog: [] });
    expect(items.filter((i) => i.urgent).map((i) => i.kind)).toEqual(["blocking", "blocking"]);
  });
});

describe("isSmallProduct", () => {
  it("is small with two features or fewer", () => {
    expect(isSmallProduct(2)).toBe(true);
    expect(isSmallProduct(3)).toBe(false);
  });
});
