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
import type { PickRow } from "../../builds/model/picker";
import type { DesignComment } from "../../design/api/designModel";
import { deployLeg, projectTrack, type TrackInput } from "./track";

// The track follows the Acme Expenses walk: each leg's lamp and line read the
// same state as the feature rows, the design card and the build picker.

type Feature = TrackInput["features"][number];
const feature = (name: string, over: Partial<Feature> = {}): Feature => ({
  name,
  stage: "Interviewed",
  blocking: [],
  toConfirm: 0,
  ...over,
});
const question = { question: "One Xero organisation?", options: [] };

// As seeded: two features ready for design, Payroll export held by a
// question, Approvals with two lines to confirm, two stubs.
const acme: Feature[] = [
  feature("Submit expenses"),
  feature("Approvals", { toConfirm: 2 }),
  feature("Payroll export", { blocking: [question] }),
  feature("Spending reports", { stage: "Not interviewed" }),
  feature("Mileage claims", { stage: "Not interviewed" }),
];

const row = (id: string, state: PickRow["state"]): PickRow => ({
  kind: "feature",
  id,
  name: id,
  state,
  detail: [],
  warn: false,
});

const comment = (status: DesignComment["status"]) => ({ status });

const beforeDesign: TrackInput = {
  features: acme,
  design: { toDesign: ["F1", "F2"], outOfDate: [] },
  designedFrom: {},
  review: { running: null, comments: [] },
  builds: [],
  latestOutcome: null,
  offer: { version: "v1", rows: [row("F1", "disabled"), row("F2", "disabled")] },
  deploy: { status: "none", version: "" },
};

const designed: TrackInput = {
  ...beforeDesign,
  features: acme.map((f, i) => (i < 2 ? { ...f, stage: "Designed" as const } : f)),
  design: { toDesign: [], outOfDate: [] },
  designedFrom: { F1: "…", F2: "…" },
  offer: { version: "v1", rows: [row("F1", "offered"), row("F2", "offered")] },
};

describe("the spec leg", () => {
  it("is being written while the project has no features yet", () => {
    expect(projectTrack({ ...beforeDesign, features: [] }).spec).toEqual({ state: "live", summary: "Writing the spec" });
  });

  it("is live while a feature is being interviewed", () => {
    const features = acme.map((f) => (f.name === "Spending reports" ? { ...f, stage: "Interviewing" as const } : f));
    expect(projectTrack({ ...beforeDesign, features }).spec).toEqual({ state: "live", summary: "Interviewing Spending reports" });
  });

  it("waits on a blocking question first, then lines to confirm, then interviews", () => {
    expect(projectTrack(beforeDesign).spec).toEqual({ state: "waiting", summary: "1 question to answer" });
    const answered = acme.map((f) => ({ ...f, blocking: [] }));
    expect(projectTrack({ ...beforeDesign, features: answered }).spec).toEqual({ state: "waiting", summary: "2 lines to confirm" });
    const confirmed = answered.map((f) => ({ ...f, toConfirm: 0 }));
    expect(projectTrack({ ...beforeDesign, features: confirmed }).spec).toEqual({
      state: "waiting",
      summary: "2 features to interview",
    });
  });

  it("is done once every feature is interviewed and nothing waits", () => {
    const features = acme.map((f) => ({ ...f, stage: "Interviewed" as const, blocking: [], toConfirm: 0 }));
    expect(projectTrack({ ...beforeDesign, features }).spec).toEqual({ state: "done", summary: "5 features" });
  });
});

describe("the design leg", () => {
  it("is not yet while nothing is designed or ready to design", () => {
    const input = { ...beforeDesign, features: [], design: { toDesign: [], outOfDate: [] } };
    expect(projectTrack(input).design).toEqual({ state: "notyet", summary: "Not yet" });
  });

  it("waits on the features to design before Design runs", () => {
    expect(projectTrack(beforeDesign).design).toEqual({ state: "waiting", summary: "2 features to design" });
  });

  it("is live while the design turn runs", () => {
    const running = { ...beforeDesign, review: { running: { kind: "design" as const, features: ["F1", "F2"] }, comments: [] } };
    expect(projectTrack(running).design).toEqual({ state: "live", summary: "Designing 2 features" });
  });

  it("is done once Design has run", () => {
    expect(projectTrack(designed).design).toEqual({ state: "done", summary: "2 features designed" });
  });

  it("says a design is out of date once its spec changed, and live again while Update design runs", () => {
    const edited = { ...designed, design: { toDesign: ["F2"], outOfDate: ["F2"] } };
    expect(projectTrack(edited).design).toEqual({ state: "waiting", summary: "1 feature out of date" });
    const updating = { ...edited, review: { running: { kind: "design" as const, features: ["F2"] }, comments: [] } };
    expect(projectTrack(updating).design).toEqual({ state: "live", summary: "Designing 1 feature" });
  });

  it("counts a new feature and an out-of-date one together as features to design", () => {
    const both = { ...designed, design: { toDesign: ["F2", "F3"], outOfDate: ["F2"] } };
    expect(projectTrack(both).design).toEqual({ state: "waiting", summary: "2 features to design" });
  });

  it("waits on open comments, then on addressed ones to check", () => {
    const pinned = { ...designed, review: { running: null, comments: [comment("open"), comment("resolved")] } };
    expect(projectTrack(pinned).design).toEqual({ state: "waiting", summary: "1 comment to address" });
    const addressing = { ...pinned, review: { running: { kind: "address" as const, features: [] }, comments: pinned.review.comments } };
    expect(projectTrack(addressing).design).toEqual({ state: "live", summary: "Addressing comments" });
    const addressed = { ...designed, review: { running: null, comments: [comment("addressed"), comment("addressed")] } };
    expect(projectTrack(addressed).design).toEqual({ state: "waiting", summary: "2 comments to check" });
  });
});

describe("the build leg", () => {
  it("is not yet while nothing can be built", () => {
    expect(projectTrack(beforeDesign).build).toEqual({ state: "notyet", summary: "Not yet" });
  });

  it("is ready to build once the picker offers something", () => {
    expect(projectTrack(designed).build).toEqual({ state: "waiting", summary: "Ready to build" });
  });

  it("is live while a build runs", () => {
    const building = { ...designed, builds: [{ version: "v1", status: "building" as const }] };
    expect(projectTrack(building).build).toEqual({ state: "live", summary: "Building v1" });
  });

  it("is done with the last version built, and ready again when there is more to build", () => {
    const built = {
      ...designed,
      builds: [{ version: "v1", status: "built" as const }],
      offer: { version: "v2", rows: [row("F1", "fixed"), row("F2", "fixed")] },
    };
    expect(projectTrack(built).build).toEqual({ state: "done", summary: "v1 built" });
    const more = { ...built, offer: { version: "v2", rows: [row("F1", "fixed"), row("F2", "offered")] } };
    expect(projectTrack(more).build).toEqual({ state: "waiting", summary: "Ready to build v2" });
  });

  it("says how the last version validated, and waits on it while a scenario fails", () => {
    const built = {
      ...designed,
      builds: [{ version: "v1", status: "built" as const }],
      offer: { version: "v2", rows: [row("F1", "fixed"), row("F2", "fixed")] },
    };
    const failing = { featureId: "F2", featureName: "Approvals", story: "F2.4", name: "A deputy approves", standing: null };
    expect(projectTrack({ ...built, latestOutcome: { passed: 10, total: 11, failing: [failing] } }).build).toEqual({
      state: "waiting",
      summary: "v1 built · F2.4 failing",
    });
    const fixed = { ...built, builds: [...built.builds, { version: "v1.1", status: "built" as const }] };
    expect(projectTrack({ ...fixed, latestOutcome: { passed: 11, total: 11, failing: [] } }).build).toEqual({
      state: "done",
      summary: "v1.1 built · 11/11 passing",
    });
  });

  it("waits on a failed build", () => {
    const failed = { ...designed, builds: [{ version: "v1", status: "failed" as const }] };
    expect(projectTrack(failed).build).toEqual({ state: "waiting", summary: "v1 failed" });
  });
});

describe("the deploy leg", () => {
  it("is not yet until a build has deployed", () => {
    expect(projectTrack(beforeDesign).deploy).toEqual({ state: "notyet", summary: "Not yet" });
  });

  it("follows the newest build's rollout to the first environment", () => {
    expect(deployLeg({ status: "deploying", version: "v2" })).toEqual({ state: "live", summary: "Deploying v2" });
    expect(deployLeg({ status: "deployed", version: "v2" })).toEqual({ state: "done", summary: "v2 deployed" });
  });

  it("waits on the user when the rollout failed", () => {
    expect(deployLeg({ status: "failed", version: "v2" })).toEqual({ state: "waiting", summary: "v2 failed to deploy" });
  });
});
