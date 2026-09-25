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
import { renderReport, renderRunFailureReport } from "../src/report.js";
import type { Verdict } from "../src/verdict.js";

const V: Verdict = {
  passed: false,
  overall: 0.6,
  scenarios: [
    {
      id: "SC-001",
      score: 0.6,
      passed: false,
      failed: [{ id: "MC-1", reason: "did not ask for dates" }],
      ungraded: [],
      toolOverReach: [],
    },
    { id: "SC-002", score: 1, passed: true, failed: [], ungraded: [], toolOverReach: [] },
  ],
};

// A denied tool call is a security signal, not a rubric miss — it must read
// as neither a `failed` line nor an `ungraded` one, and it must reach a
// human reading `report.md`, not stop at a field only a machine consumes.
const V_OVERREACH: Verdict = {
  passed: false,
  overall: 1,
  scenarios: [
    {
      id: "SC-004",
      score: 1,
      passed: true,
      failed: [],
      ungraded: [],
      toolOverReach: ["LUNCH_API_URL: createRound"],
    },
  ],
};

// A grader that never returned a verdict for a line is a grader problem, not
// an agent problem — Ruling R7.3 requires the report to keep it visibly apart
// from genuine rubric misses.
const V_UNGRADED: Verdict = {
  passed: false,
  overall: 0.5,
  scenarios: [
    {
      id: "SC-003",
      score: 0.5,
      passed: false,
      failed: [{ id: "MC-2", reason: "invented a price" }],
      ungraded: [{ id: "MC-3", reason: "grader timeout" }],
      toolOverReach: [],
    },
  ],
};

describe("renderReport", () => {
  it("leads with the score and says plainly that it did not gate the build", () => {
    const md = renderReport(V, { component: "trip-agent", promptChanged: true });
    expect(md).toMatch(/0\.6/);
    expect(md).toMatch(/does not fail the build|reported, not enforced/i);
  });

  it("names every failed rubric line with its reason", () => {
    const md = renderReport(V, { component: "trip-agent", promptChanged: false });
    expect(md).toContain("MC-1");
    expect(md).toContain("did not ask for dates");
  });

  // A reader must be able to tell whether the shipped prompt is the authored one.
  it("says whether the prompt was revised", () => {
    expect(renderReport(V, { component: "a", promptChanged: true })).toMatch(/prompt was revised/i);
    expect(renderReport(V, { component: "a", promptChanged: false })).toMatch(/prompt was not changed/i);
  });

  it("renders ungraded lines separately from failed lines, and says why", () => {
    const md = renderReport(V_UNGRADED, { component: "trip-agent", promptChanged: false });
    expect(md).toContain("MC-2");
    expect(md).toContain("invented a price");
    expect(md).toContain("MC-3");
    expect(md).toContain("grader timeout");

    const failedSection = md.indexOf("## What fell short");
    const ungradedSection = md.indexOf("## Ungraded");
    expect(failedSection).toBeGreaterThanOrEqual(0);
    expect(ungradedSection).toBeGreaterThan(failedSection);
    // MC-3 (ungraded) must not appear before the "## Ungraded" heading, i.e.
    // it must not have been folded into the failed section above it.
    expect(md.indexOf("MC-3")).toBeGreaterThan(ungradedSection);
    expect(md).toMatch(/grading gap|never returned a verdict/i);
  });

  it("omits the ungraded section entirely when nothing was ungraded", () => {
    const md = renderReport(V, { component: "trip-agent", promptChanged: false });
    expect(md).not.toContain("## Ungraded");
  });

  it("names the operation and says the agent is not permitted to call it", () => {
    const md = renderReport(V_OVERREACH, { component: "lunch-buddy", promptChanged: false });
    expect(md).toContain("## Tool over-reach");
    expect(md).toContain("LUNCH_API_URL: createRound");
    expect(md).toMatch(/not permitted/i);
  });

  it("keeps a tool over-reach out of both the failed and ungraded sections", () => {
    const md = renderReport(V_OVERREACH, { component: "lunch-buddy", promptChanged: false });
    expect(md).not.toContain("## What fell short");
    expect(md).not.toContain("## Ungraded");
  });

  it("omits the tool over-reach section entirely when nothing over-reached", () => {
    const md = renderReport(V, { component: "trip-agent", promptChanged: false });
    expect(md).not.toContain("## Tool over-reach");
  });

  // A 0.00 with an empty table reads as "every scenario failed"; an empty
  // result set is a materially different situation and must say so plainly.
  it("says plainly that nothing was graded when the result set is empty", () => {
    const empty: Verdict = { passed: false, overall: 0, scenarios: [] };
    const md = renderReport(empty, { component: "trip-agent", promptChanged: false });
    expect(md).toMatch(/no scenario was graded/i);
    expect(md).not.toContain("## Scenarios");
  });
});

describe("renderRunFailureReport", () => {
  it("says the run itself failed, cites the error, and still does not gate the build", () => {
    const md = renderRunFailureReport({ component: "trip-agent", error: "promptfoo exited 1: boom" });
    expect(md).toMatch(/run itself failed/i);
    expect(md).toContain("promptfoo exited 1: boom");
    expect(md).toMatch(/does not fail the build|reported, not enforced/i);
  });
});
