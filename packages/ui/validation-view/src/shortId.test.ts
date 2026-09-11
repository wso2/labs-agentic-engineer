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

import { describe, it, expect } from "vitest";
import { shortCriterionId, shortRequirementId } from "./shortId.js";

describe("shortRequirementId", () => {
  it("keeps only the number, past the padding", () => {
    expect(shortRequirementId("REQ-001")).toBe("1");
    expect(shortRequirementId("REQ-013")).toBe("13");
    expect(shortRequirementId("REQ-100")).toBe("100");
  });

  it("does not renumber — a gap stays a gap", () => {
    // Ids are stable by contract (a spec file is named after its criterion), so
    // deleting REQ-002 leaves 1, 3, 4. These are names, not list positions.
    expect(["REQ-001", "REQ-003", "REQ-004"].map(shortRequirementId)).toEqual([
      "1",
      "3",
      "4",
    ]);
  });

  it("shows an id that breaks the convention in full", () => {
    expect(shortRequirementId("R-1")).toBe("R-1");
    expect(shortRequirementId("REQ-1a")).toBe("REQ-1a");
    expect(shortRequirementId("requirement one")).toBe("requirement one");
    expect(shortRequirementId("")).toBe("");
  });
});

describe("shortCriterionId", () => {
  it("drops the prefix its own card already carries", () => {
    expect(shortCriterionId("AC-001-a", "REQ-001")).toBe("a");
    expect(shortCriterionId("AC-013-b", "REQ-013")).toBe("b");
  });

  it("matches on the number, not the padding", () => {
    expect(shortCriterionId("AC-1-a", "REQ-001")).toBe("a");
    expect(shortCriterionId("AC-001-a", "REQ-1")).toBe("a");
  });

  it("keeps the full id when the criterion names another requirement", () => {
    // The number is the only part of the id that says it is filed in the wrong
    // card, so shortening would render the mistake as a tidy "a".
    expect(shortCriterionId("AC-001-a", "REQ-002")).toBe("AC-001-a");
  });

  it("keeps the full id when either side breaks the convention", () => {
    expect(shortCriterionId("AC-001-a", "R-1")).toBe("AC-001-a");
    expect(shortCriterionId("AC-001", "REQ-001")).toBe("AC-001");
    expect(shortCriterionId("AC-001-", "REQ-001")).toBe("AC-001-");
    expect(shortCriterionId("criterion one", "REQ-001")).toBe("criterion one");
    expect(shortCriterionId("", "REQ-001")).toBe("");
  });

  it("keeps a multi-part suffix whole", () => {
    expect(shortCriterionId("AC-001-a-2", "REQ-001")).toBe("a-2");
  });
});
