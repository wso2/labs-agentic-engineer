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
import {
  NO_RESULT_LABEL,
  outcomeLabel,
  outcomeTone,
  tallyOutcomes,
  tallySentence,
} from "./outcomes.js";
import type { ReportScenario } from "./report.js";

function scenarios(...outcomes: string[]): ReportScenario[] {
  return outcomes.map((outcome, i) => ({
    feature: "F",
    rule: "R",
    scenario: `S${i}`,
    outcome,
    steps: [],
  }));
}

describe("outcomeLabel", () => {
  it("title-cases the report's own word, so there is no table to keep in step", () => {
    expect(outcomeLabel("passed")).toBe("Passed");
    expect(outcomeLabel("failed")).toBe("Failed");
    expect(outcomeLabel("blocked")).toBe("Blocked");
    expect(outcomeLabel("unjudgeable")).toBe("Unjudgeable");
  });

  it("renders a word it has never seen verbatim rather than mislabelling it", () => {
    expect(outcomeLabel("abandoned")).toBe("Abandoned");
    expect(outcomeTone("abandoned")).toBe("default");
  });

  it("says No result for a scenario the report does not mention", () => {
    expect(outcomeLabel("")).toBe(NO_RESULT_LABEL);
  });
});

describe("outcomeTone", () => {
  // Amber means a person has to look, and both of these are that: no repair
  // issue is filed for either (FailedScenarios returns `failed` alone), the Go
  // ladder pairs them in one arm, and the partial sentence counts them
  // together. They differ by glyph, not by hue.
  it("gives both unsettled outcomes the same call to action", () => {
    expect(outcomeTone("blocked")).toBe("warning");
    expect(outcomeTone("unjudgeable")).toBe("warning");
  });

  // The one that genuinely needs no action: a scenario written since the run is
  // the ordinary authoring loop, and colouring the expected state teaches a
  // reader to discount the colour.
  it("leaves a scenario the run never covered neutral", () => {
    expect(outcomeTone("")).toBe("default");
  });

  it("keeps passed and failed on the two colours the console already uses", () => {
    expect(outcomeTone("passed")).toBe("success");
    expect(outcomeTone("failed")).toBe("error");
  });
});

describe("tallyOutcomes", () => {
  it("counts everything that is not passed as unresolved, unknown words included", () => {
    const t = tallyOutcomes(scenarios("passed", "passed", "blocked", "abandoned"));
    expect(t).toMatchObject({ total: 4, passed: 2, unresolved: 2 });
  });

  it("orders the known outcomes as the vocabulary is written, unknown ones last", () => {
    const t = tallyOutcomes(scenarios("abandoned", "blocked", "passed", "failed"));
    expect(t.counts.map((c) => c.outcome)).toEqual(["passed", "failed", "blocked", "abandoned"]);
  });
});

describe("tallySentence", () => {
  it("drops the denominator when everything passed", () => {
    expect(tallySentence(tallyOutcomes(scenarios("passed", "passed")))).toBe("2 passed");
  });

  it("names what else happened", () => {
    expect(tallySentence(tallyOutcomes(scenarios("passed", "blocked", "blocked")))).toBe(
      "1 of 3 passed · 2 blocked",
    );
  });

  it("says nothing at all about no scenarios", () => {
    expect(tallySentence(tallyOutcomes([]))).toBe("");
  });
});
