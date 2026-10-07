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
import { legStateLabel, trackLegs } from "./trackLegs";
import type { ProjectTrack } from "./model/track";

const acme: ProjectTrack = {
  spec: { state: "done", summary: "5 features" },
  design: { state: "waiting", summary: "2 features to design" },
  build: { state: "notyet", summary: "Not yet" },
  deploy: { state: "notyet", summary: "Not yet" },
};

describe("trackLegs", () => {
  it("draws Spec, Design, Build, Deploy in order, each opening its card or Page", () => {
    expect(trackLegs(acme, null).map((l) => [l.step, l.title, l.opens])).toEqual([
      [1, "Spec", { kind: "spec" }],
      [2, "Design", { kind: "design" }],
      [3, "Build", { kind: "builds" }],
      [4, "Deploy", { kind: "deploy" }],
    ]);
  });

  it("opens the latest build's card from the Build leg once there is one", () => {
    expect(trackLegs(acme, "v2")[2]?.opens).toEqual({ kind: "build", version: "v2" });
  });

  it("carries each leg's state and state line from the track", () => {
    const [spec, design] = trackLegs(acme, null);
    expect(spec).toMatchObject({ state: "done", stateLabel: "Done", summary: "5 features" });
    expect(design).toMatchObject({
      state: "waiting",
      stateLabel: "Waiting on you",
      summary: "2 features to design",
    });
  });

  it("carries Deploy's state from the track", () => {
    const shipped: ProjectTrack = {
      spec: { state: "done", summary: "" },
      design: { state: "done", summary: "" },
      build: { state: "done", summary: "v1 built" },
      deploy: { state: "done", summary: "v1 deployed" },
    };
    expect(trackLegs(shipped, "v1")[3]).toMatchObject({ state: "done", summary: "v1 deployed" });
  });
});

describe("a leg's accessible name", () => {
  it("says the state in words, then the state line", () => {
    expect(trackLegs(acme, null)[1]?.accessibleName).toBe("Design: Waiting on you. 2 features to design");
  });

  it("does not repeat a state line that only restates the state", () => {
    expect(trackLegs(acme, null)[2]?.accessibleName).toBe("Build: Not yet");
  });
});

describe("legStateLabel", () => {
  it("names every lamp in words", () => {
    expect(legStateLabel("done")).toBe("Done");
    expect(legStateLabel("live")).toBe("In progress");
    expect(legStateLabel("waiting")).toBe("Waiting on you");
    expect(legStateLabel("notyet")).toBe("Not yet");
  });
});
