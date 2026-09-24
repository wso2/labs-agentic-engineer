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
import { parseFeatureFile, type AcceptanceFeature } from "./parseFeature.js";
import { parseAcceptanceReport, isReportParseError, type AcceptanceReport } from "./report.js";
import {
  ANY_STATUS,
  deriveStatuses,
  deriveTags,
  haystack,
  isFiltering,
  matchesFilter,
  NO_FILTER,
  NO_RESULT,
} from "./filter.js";

const FEATURE = parseFeatureFile(
  "specs/validation/acceptance/bought-items.feature",
  [
    "@capability",
    "Feature: Bought items",
    "",
    "  @story-10",
    "  Rule: A bought item is locked",
    "",
    "    @negative",
    "    Scenario: Editing a bought item is refused",
    '      Then the quantity of "Eggs" is still "1"',
    "",
    "  @story-2",
    "  Rule: An item can be marked bought",
    "",
    "    Scenario: Marking an item bought",
    '      Then the list shows "Eggs" as bought',
  ].join("\n"),
) as AcceptanceFeature;

function reportOf(...outcomes: string[]): AcceptanceReport {
  const names = ["Editing a bought item is refused", "Marking an item bought"];
  const rules = ["A bought item is locked", "An item can be marked bought"];
  const parsed = parseAcceptanceReport(
    JSON.stringify({
      scenarios: outcomes.map((outcome, i) => ({
        feature: "Bought items",
        rule: rules[i],
        scenario: names[i],
        outcome,
        steps: [],
      })),
    }),
  );
  if (isReportParseError(parsed)) throw new Error(parsed.error);
  return parsed;
}

describe("matchesFilter", () => {
  const scenario = { hay: "editing a bought item is refused @negative", tags: ["@negative", "@story-10"], outcome: "blocked" };

  it("passes everything through when nothing is set", () => {
    expect(matchesFilter(scenario, NO_FILTER)).toBe(true);
    expect(isFiltering(NO_FILTER)).toBe(false);
  });

  it("narrows on a substring of the haystack, case-insensitively", () => {
    expect(matchesFilter(scenario, { ...NO_FILTER, query: "BOUGHT" })).toBe(true);
    expect(matchesFilter(scenario, { ...NO_FILTER, query: "clearing" })).toBe(false);
  });

  // Two tags narrow; they do not widen. Selecting @negative and @story-2 asks
  // for scenarios that are both, which is the useful reading.
  it("requires every selected tag, not any of them", () => {
    expect(matchesFilter(scenario, { ...NO_FILTER, tags: ["@negative"] })).toBe(true);
    expect(matchesFilter(scenario, { ...NO_FILTER, tags: ["@negative", "@story-10"] })).toBe(true);
    expect(matchesFilter(scenario, { ...NO_FILTER, tags: ["@negative", "@story-2"] })).toBe(false);
  });

  it("filters on the outcome as rendered, so an uncovered scenario is selectable", () => {
    // Filtering on report.outcome instead would drop every uncovered row out of
    // every segment, `All` included.
    const uncovered = { ...scenario, outcome: NO_RESULT };
    expect(matchesFilter(uncovered, { ...NO_FILTER, status: NO_RESULT })).toBe(true);
    expect(matchesFilter(uncovered, { ...NO_FILTER, status: ANY_STATUS })).toBe(true);
    expect(matchesFilter(uncovered, { ...NO_FILTER, status: "passed" })).toBe(false);
  });

  it("combines all three", () => {
    expect(
      matchesFilter(scenario, { query: "refused", tags: ["@negative"], status: "blocked" }),
    ).toBe(true);
    expect(
      matchesFilter(scenario, { query: "refused", tags: ["@negative"], status: "passed" }),
    ).toBe(false);
  });
});

describe("haystack", () => {
  it("covers everything the row shows, lowercased", () => {
    const hay = haystack({
      feature: "Bought items",
      rule: "A bought item is locked",
      scenario: "Editing a bought item is refused",
      tags: ["@negative"],
      steps: [{ text: 'the quantity of "Eggs" is still "1"' }],
    });
    for (const term of ["bought items", "locked", "refused", "@negative", "eggs"]) {
      expect(hay, term).toContain(term);
    }
  });
});

describe("deriveTags", () => {
  it("puts the refusal first, then the stories in numeric order", () => {
    // @story-10 must not sort between 1 and 2, which a string sort would do.
    expect(deriveTags([FEATURE])).toEqual(["@negative", "@story-2", "@story-10", "@capability"]);
  });
});

describe("deriveStatuses", () => {
  it("offers nothing at all without a run", () => {
    expect(deriveStatuses([FEATURE], undefined)).toEqual([]);
  });

  it("offers only the outcomes the run actually produced", () => {
    expect(deriveStatuses([FEATURE], reportOf("blocked", "passed"))).toEqual([
      ANY_STATUS,
      "passed",
      "blocked",
    ]);
  });

  // A control whose every option selects the whole list is furniture.
  it("offers nothing when every scenario came out the same way", () => {
    expect(deriveStatuses([FEATURE], reportOf("passed", "passed"))).toEqual([]);
  });

  it("counts a scenario the report never mentions as No result", () => {
    const partial = parseAcceptanceReport(
      JSON.stringify({
        scenarios: [
          {
            feature: "Bought items",
            rule: "A bought item is locked",
            scenario: "Editing a bought item is refused",
            outcome: "passed",
            steps: [],
          },
        ],
      }),
    );
    if (isReportParseError(partial)) throw new Error(partial.error);
    expect(deriveStatuses([FEATURE], partial)).toEqual([ANY_STATUS, "passed", NO_RESULT]);
  });

  // While an attempt is in flight an uncovered row carries no pill at all, so a
  // segment selecting those rows would select rows showing no state.
  it("withholds No result while an attempt is in flight", () => {
    const partial = parseAcceptanceReport(
      JSON.stringify({
        scenarios: [
          {
            feature: "Bought items",
            rule: "A bought item is locked",
            scenario: "Editing a bought item is refused",
            outcome: "passed",
            steps: [],
          },
        ],
      }),
    );
    if (isReportParseError(partial)) throw new Error(partial.error);
    expect(deriveStatuses([FEATURE], partial, true)).toEqual([]);
  });
});
