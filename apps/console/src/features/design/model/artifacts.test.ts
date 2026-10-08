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
import type { DesignArtifact, DesignDependency } from "../api/designModel";
import { artifactMarker, blockingDependencies, dependencyKey, filterArtifacts, openArtifact } from "./artifacts";

function artifact(id: string, over: Partial<DesignArtifact> = {}): DesignArtifact {
  return {
    id,
    title: id,
    depth: "business",
    features: ["F1", "F2"],
    addedIn: 1,
    changedIn: 1,
    source: { kind: "security", rows: [] },
    ...over,
  };
}

const prototype = artifact("prototype");
const architecture = artifact("architecture", { depth: "technical" });
const acceptanceF1 = artifact("acceptance-F1", { features: ["F1"] });
const all = [prototype, architecture, acceptanceF1];

const xero: DesignDependency = {
  featureId: "F3",
  needs: "Xero",
  question: "How does Payroll export reach Xero?",
  why: "",
  options: ["Use Xero's published API"],
  answer: null,
};

describe("filterArtifacts", () => {
  it("shows every artifact under All, and one depth under Business or Technical, counting the rest", () => {
    expect(filterArtifacts(all, "all")).toEqual({ shown: all, hidden: 0 });
    expect(filterArtifacts(all, "business")).toEqual({ shown: [prototype, acceptanceF1], hidden: 1 });
    expect(filterArtifacts(all, "technical")).toEqual({ shown: [architecture], hidden: 2 });
  });
});

describe("artifactMarker", () => {
  it("marks what the latest design turn added or changed, and nothing after a later turn", () => {
    expect(artifactMarker(artifact("a", { addedIn: 2, changedIn: 2 }), 2, [])).toBe("new");
    expect(artifactMarker(artifact("a", { addedIn: 1, changedIn: 2 }), 2, [])).toBe("changed");
    expect(artifactMarker(artifact("a", { addedIn: 1, changedIn: 2 }), 3, [])).toBeNull();
  });

  it("marks every artifact covering an out-of-date feature, over new or changed", () => {
    expect(artifactMarker(artifact("a", { addedIn: 2, changedIn: 2 }), 2, ["F2"])).toBe("out of date");
    expect(artifactMarker(acceptanceF1, 1, ["F2"])).toBe("new");
  });
});

describe("dependencies", () => {
  it("blocks only while unanswered", () => {
    expect(blockingDependencies([xero, { ...xero, featureId: "F4", answer: "We have our own contract" }])).toEqual([xero]);
  });

  it("opens the named artifact or dependency, else the first artifact", () => {
    expect(openArtifact(all, [xero], "architecture")).toEqual({ kind: "artifact", artifact: architecture });
    expect(openArtifact(all, [xero], dependencyKey("F3"))).toEqual({ kind: "dependency", dependency: xero });
    expect(openArtifact(all, [xero], "gone")).toEqual({ kind: "artifact", artifact: prototype });
    expect(openArtifact([], [], undefined)).toBeNull();
  });
});
