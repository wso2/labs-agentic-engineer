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
import { buildCardSteps, finishedNote, nextSteps, startedNote } from "./nextSteps";
import type { FeatureResults } from "./validation";

const deputy = { featureId: "F2", featureName: "Approvals", story: "F2.4", name: "A deputy approves while the manager is on leave", standing: null };
const failing = { passed: 10, total: 11, failing: [deputy], regressions: 0 };
const passing = { passed: 11, total: 11, failing: [], regressions: 0 };
const spending = { id: "F4", name: "Spending reports" };

const group = (id: string, name: string, passed: number, total: number): FeatureResults => ({
  id,
  name,
  passed,
  judged: total,
  runs: total,
  notBuilt: false,
  scenarios: Array.from({ length: total }, (_, i) => ({
    name: `${name} ${i}`,
    story: null,
    outcome: i < passed ? "passed" : "failed",
    expected: null,
    got: null,
    excerpt: [],
    standing: null,
  })),
});

describe("what a finished build offers next", () => {
  it("offers the fix of a failing story, and the story itself in the spec", () => {
    const next = nextSteps({ version: "v1", outcome: failing, fixedBy: null, latest: "v1", nextInterview: spending });
    expect(next).toEqual({
      note: null,
      steps: [
        { kind: "fix", label: "Fix F2.4", version: "v1", stories: ["F2.4"] },
        { kind: "story", label: "Open F2.4 in Spec", story: "F2.4" },
      ],
    });
  });

  it("points at the repair build once one has started, and says whether it is done", () => {
    const fixing = nextSteps({ version: "v1", outcome: failing, fixedBy: { version: "v1.1", building: true }, latest: "v1.1", nextInterview: null });
    expect(fixing).toEqual({ note: "Fixing in v1.1…", steps: [{ kind: "open", label: "Open v1.1", version: "v1.1" }] });
    const fixed = nextSteps({ version: "v1", outcome: failing, fixedBy: { version: "v1.1", building: false }, latest: "v1.1", nextInterview: null });
    expect(fixed.note).toBe("Fixed in v1.1.");
  });

  it("offers trying the latest version in development and the next interview once everything passes", () => {
    expect(nextSteps({ version: "v1.1", outcome: passing, fixedBy: null, latest: "v1.1", nextInterview: spending }).steps).toEqual([
      { kind: "interview", label: "Interview Spending reports", featureId: "F4" },
    ]);
    expect(nextSteps({ version: "v1.1", outcome: passing, fixedBy: null, latest: "v2", nextInterview: spending })).toEqual({
      note: "v2 is the latest build.",
      steps: [{ kind: "open", label: "Open v2", version: "v2" }],
    });
  });
});

describe("the chat's two lines about a build", () => {
  it("says what is building, or what a repair fixes, and links to the card", () => {
    expect(startedNote("v1", ["Submit expenses", "Approvals"], null)).toEqual({
      text: "v1 is building: Submit expenses, Approvals. Watch it here.",
      actions: [{ kind: "open-build", label: "Open v1", version: "v1" }],
    });
    expect(startedNote("v1.1", [], { stories: ["F2.4"] }).text).toBe("v1.1 is building: it fixes F2.4. Watch it here.");
  });

  it("says which feature fell short, and which story fails", () => {
    const groups = [group("F1", "Submit expenses", 5, 5), group("F2", "Approvals", 5, 6)];
    const next = nextSteps({ version: "v1", outcome: failing, fixedBy: null, latest: "v1", nextInterview: spending });
    expect(finishedNote("v1", groups, failing, next)).toEqual({
      text: "v1 is built. Approvals: 5 of 6 scenarios pass; F2.4 fails.",
      actions: [{ kind: "open-build", label: "Open v1", version: "v1" }],
    });
  });

  it("offers the next interview once every scenario passes", () => {
    const groups = [group("F1", "Submit expenses", 5, 5), group("F2", "Approvals", 6, 6)];
    const next = nextSteps({ version: "v1.1", outcome: passing, fixedBy: null, latest: "v1.1", nextInterview: spending });
    expect(finishedNote("v1.1", groups, passing, next)).toEqual({
      text: "v1.1 is built and all 11 scenarios pass. It runs in development.",
      actions: [
        { kind: "open-build", label: "Open v1.1", version: "v1.1" },
        { kind: "interview", label: "Interview Spending reports", featureId: "F4" },
      ],
    });
  });
});

describe("the Build card's next steps", () => {
  it("sends a failing version to its validation, where Fix is", () => {
    const next = nextSteps({ version: "v1", outcome: failing, fixedBy: null, latest: "v1", nextInterview: null });
    expect(buildCardSteps(next, "v1")).toEqual({
      note: null,
      steps: [{ kind: "validation", label: "See what failed in v1", version: "v1" }],
    });
  });

  it("keeps every other next step as it is", () => {
    const passed = nextSteps({ version: "v1", outcome: passing, fixedBy: null, latest: "v1", nextInterview: spending });
    expect(buildCardSteps(passed, "v1")).toBe(passed);
    const fixing = nextSteps({ version: "v1", outcome: failing, fixedBy: { version: "v1.1", building: true }, latest: "v1.1", nextInterview: null });
    expect(buildCardSteps(fixing, "v1")).toBe(fixing);
  });
});
