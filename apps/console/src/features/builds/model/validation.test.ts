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
import { groupByFeature, validationOutcome } from "./validation";

const approvals = {
  path: "specs/validation/acceptance/F2-approvals.feature",
  content: `Feature: F2 Approvals
  Managers approve or reject their team's claims.

  @story-F2.1
  Rule: A manager sees the team's pending claims, oldest first

    Scenario: The oldest claim is first
      Given Priya submitted claim 42 on Monday
      Then claim 42 is listed first

  @story-F2.4
  Rule: A deputy approves while the manager is on leave

    Scenario: A deputy approves while the manager is on leave
      Given Sam is on leave and named Lee as his deputy
      When Lee opens Pending approvals
      Then claim 43 is in Lee's queue
      And Lee can approve it for Sam
`,
};

// No story tags at all: the feature's own F-number title groups it.
const payroll = {
  path: "specs/validation/acceptance/F3-payroll.feature",
  content: `Feature: F3 Payroll export

  Scenario: Approved claims reach Xero overnight
    Given claim 42 is approved
    Then claim 42 is a bill in Xero
`,
};

const report = JSON.stringify({
  schemaVersion: 2,
  scenarios: [
    {
      feature: "F2 Approvals",
      rule: "A manager sees the team's pending claims, oldest first",
      scenario: "The oldest claim is first",
      outcome: "passed",
      steps: [{ keyword: "Given", text: "Priya submitted claim 42 on Monday", exit: 0 }],
    },
    {
      feature: "F2 Approvals",
      rule: "A deputy approves while the manager is on leave",
      scenario: "A deputy approves while the manager is on leave",
      outcome: "failed",
      steps: [
        { keyword: "Given", text: "Sam is on leave and named Lee as his deputy", command: "PUT /managers/sam/deputy", exit: 0 },
        { keyword: "When", text: "Lee opens Pending approvals", exit: 0 },
        { keyword: "Then", text: "claim 43 is in Lee's queue", command: "agent-browser wait --text 'Claim 43'", exit: 1, observed: "an empty queue" },
      ],
    },
    { feature: "F3 Payroll export", rule: "", scenario: "Approved claims reach Xero overnight", outcome: "passed", steps: [] },
  ],
});

describe("validation grouped by feature", () => {
  it("files each scenario under its feature by ID, from the Feature line or else its story tag", () => {
    const groups = groupByFeature({ criteria: [payroll, approvals], report: report });
    expect(groups.map((g) => [g.id, g.name, g.scenarios.map((s) => s.story)])).toEqual([
      ["F2", "Approvals", ["F2.1", "F2.4"]],
      ["F3", "Payroll export", [null]],
    ]);
  });

  it("says what a failing scenario expected and got, with the steps that led there", () => {
    const [f2] = groupByFeature({ criteria: [approvals], report: report });
    expect(f2).toMatchObject({ passed: 1, judged: 2 });
    expect(f2?.scenarios[1]).toEqual({
      name: "A deputy approves while the manager is on leave",
      story: "F2.4",
      outcome: "failed",
      expected: "claim 43 is in Lee's queue",
      got: "an empty queue",
      excerpt: [
        "Given Sam is on leave and named Lee as his deputy",
        "  $ PUT /managers/sam/deputy  (exit 0)",
        "When Lee opens Pending approvals",
        "Then claim 43 is in Lee's queue",
        "  $ agent-browser wait --text 'Claim 43'  (exit 1)",
        "  observed: an empty queue",
      ],
      standing: null,
    });
  });

  it("lists every scenario as pending before the attempt has committed a report", () => {
    const [f2] = groupByFeature({ criteria: [approvals], report: null });
    expect(f2?.scenarios.map((s) => s.outcome)).toEqual(["pending", "pending"]);
    expect(f2).toMatchObject({ passed: 0, judged: 0 });
  });

  it("adds up to the version's result: how many passed, and which failed", () => {
    expect(validationOutcome(groupByFeature({ criteria: [approvals, payroll], report: report }))).toEqual({
      passed: 2,
      total: 3,
      failing: [
        {
          featureId: "F2",
          featureName: "Approvals",
          story: "F2.4",
          name: "A deputy approves while the manager is on leave",
          standing: null,
        },
      ],
      regressions: 0,
    });
  });

  // B4: the version's reading of the attempt.
  const deputyKey = "F2 / A deputy approves while the manager is on leave / A deputy approves while the manager is on leave";

  it("marks a failure that passed in the previous validated version as a regression", () => {
    const groups = groupByFeature({ criteria: [approvals], report, regressions: [deputyKey] });
    expect(groups[0]?.scenarios[1]?.standing).toBe("regression");
    expect(validationOutcome(groups).regressions).toBe(1);
    const still = groupByFeature({ criteria: [approvals], report, stillFailing: [deputyKey] });
    expect(still[0]?.scenarios[1]?.standing).toBe("still-failing");
  });

  it("does not run a feature not built yet, nor a rule whose stories are held back", () => {
    const groups = groupByFeature({
      criteria: [approvals, payroll],
      report,
      scope: { features: ["F2"], heldBack: ["F2.4"] },
    });
    expect(groups.map((g) => [g.id, g.notBuilt, g.scenarios.map((s) => s.outcome)])).toEqual([
      ["F2", false, ["passed", "not-run"]],
      ["F3", true, ["not-run"]],
    ]);
    expect(validationOutcome(groups)).toMatchObject({ passed: 1, total: 1, failing: [] });
  });

  it("keeps a renamed feature's results: the report is matched by the feature's ID", () => {
    const renamed = report.replaceAll('"F2 Approvals"', '"F2 Claim review"');
    expect(groupByFeature({ criteria: [approvals], report: renamed })[0]).toMatchObject({ passed: 1, judged: 2 });
  });
});
