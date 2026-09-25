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
import { simAnswer } from "../src/sim-user.js";

const BRIEF = {
  goal: "Book a hotel in London",
  facts: { city: "London", nights: 3, dates: "25th-28th December" },
  withholds: ["dates"],
};

describe("simAnswer", () => {
  it("opens with the goal and nothing else", () => {
    const said = simAnswer(BRIEF, "", 0);
    expect(said).toContain("London");
    // The withheld fact must NOT be volunteered — that is the whole test.
    expect(said).not.toContain("December");
  });

  it("gives up a withheld fact only when the agent asks for it", () => {
    expect(simAnswer(BRIEF, "Which dates are you travelling?", 1)).toContain("December");
  });

  it("does not volunteer a withheld fact for an unrelated question", () => {
    expect(simAnswer(BRIEF, "How many guests?", 1)).not.toContain("December");
  });

  it("answers a non-withheld fact freely", () => {
    expect(simAnswer(BRIEF, "How many nights?", 1)).toContain("3");
  });

  // A conversation that never ends burns the org's key.
  it("closes the conversation once the agent stops asking", () => {
    expect(simAnswer(BRIEF, "Here are two options.", 2).toLowerCase()).toMatch(/thanks|that's all/);
  });

  // Turn 0 performs no substitution on `goal` at all — a goal that states a
  // withheld fact is rejected upstream by `parseScenarios`, so `simAnswer`
  // never has to touch the text. Proven here with a substring that an
  // earlier redaction step over-matched: "3 people" against a withheld
  // `nights: 3` must survive completely intact.
  it("performs no substitution on the goal — an unrelated substring that would have over-matched survives verbatim", () => {
    const brief = {
      goal: "Book a hotel for 3 people under $300",
      facts: { city: "London", nights: 3 },
      withholds: ["nights"],
    };
    expect(simAnswer(brief, "", 0)).toBe("Book a hotel for 3 people under $300 (city: London)");
  });
});
