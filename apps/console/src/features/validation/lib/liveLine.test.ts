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
import type { ValidationCriteria } from "@aep/ui-validation-view";
import { validationLiveLine } from "./liveLine";

const oracle = (
  criteria: { id: string; method: string }[],
): ValidationCriteria => ({
  requirements: [
    {
      id: "REQ-001",
      statement: "A user can sign in",
      criteria: criteria.map((c) => ({ id: c.id, must: `${c.id} holds`, method: c.method })),
    },
  ],
});

const AUTO_TWO = oracle([
  { id: "AC-001-a", method: "e2e" },
  { id: "AC-002-a", method: "e2e" },
]);

describe("validationLiveLine", () => {
  it("narrates the setup window, where every row is still Pending", () => {
    // SKILL.md steps 1-5: read the issue, cut the branch, scaffold tests/e2e.
    // Several minutes in which nothing on the page moves.
    expect(validationLiveLine(AUTO_TWO, {}, false)).toBe("Setting up the test harness…");
    expect(validationLiveLine(AUTO_TWO, undefined, false)).toBe("Setting up the test harness…");
  });

  it("says nothing once any criterion has been picked up", () => {
    // The rows are the better narrator from here on, and a run-wide sentence
    // over rows in three different states could only be a coarser version of
    // what the reader is already looking at.
    expect(validationLiveLine(AUTO_TWO, { "AC-001-a": "exploring" }, false)).toBe("");
    expect(
      validationLiveLine(AUTO_TWO, { "AC-001-a": "pass", "AC-002-a": "running" }, false),
    ).toBe("");
  });

  it("narrates the reporting tail, where every row is settled and frozen", () => {
    expect(
      validationLiveLine(AUTO_TWO, { "AC-001-a": "pass", "AC-002-a": "fail" }, false),
    ).toBe("Writing the validation report…");
  });

  it("stops narrating once the report has landed", () => {
    // The report is the authority and the rows now read from it; a line still
    // claiming the report is being written would outlive its own subject.
    expect(
      validationLiveLine(AUTO_TWO, { "AC-001-a": "pass", "AC-002-a": "pass" }, true),
    ).toBe("");
  });

  it("does not wait on manual criteria to call the run settled", () => {
    // A manual criterion is answered by a person and never moves. Counting it
    // would mean the reporting line never appears on any project that has one.
    const mixed = oracle([
      { id: "AC-001-a", method: "e2e" },
      { id: "AC-900", method: "manual" },
    ]);
    expect(validationLiveLine(mixed, { "AC-001-a": "pass" }, false)).toBe(
      "Writing the validation report…",
    );
  });

  it("says nothing when there is no oracle, or nothing an agent can check", () => {
    expect(validationLiveLine(undefined, {}, false)).toBe("");
    expect(validationLiveLine(oracle([{ id: "AC-900", method: "manual" }]), {}, false)).toBe("");
  });
});
