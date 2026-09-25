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
import { buildPromptfooConfig } from "../src/config.js";

// The function returns `unknown` on purpose — promptfoo owns the config
// shape, not us. Tests narrow only the fields they assert on.
interface PromptfooConfigShape {
  defaultTest: { options: { provider: string } };
  tests: { assert: { metric: string; type: string; weight?: number; threshold?: number }[] }[];
}

const FILE = {
  version: 1 as const,
  component: "trip-agent",
  scenarios: [
    {
      id: "SC-001",
      criteria: [],
      brief: { goal: "g", facts: {}, withholds: [] },
      rubric: {
        mustCover: [{ id: "MC-1", must: "asks for dates", weight: 2 }],
        mustNot: [{ id: "MN-1", mustNot: "invents a price" }],
      },
    },
  ],
};

describe("buildPromptfooConfig", () => {
  it("pins the grader so a score means the same thing between runs", () => {
    const c = buildPromptfooConfig(FILE, {
      providerPath: "./p.js",
      graderModel: "anthropic:messages:claude-sonnet-5",
    }) as PromptfooConfigShape;
    expect(c.defaultTest.options.provider).toBe("anthropic:messages:claude-sonnet-5");
  });

  it("carries each mustCover through with its weight", () => {
    const c = buildPromptfooConfig(FILE, { providerPath: "./p.js", graderModel: "m" }) as PromptfooConfigShape;
    const cover = c.tests[0]!.assert.find((a) => a.metric === "MC-1")!;
    expect(cover.type).toBe("llm-rubric");
    expect(cover.weight).toBe(2);
  });

  // A mustNot is not a low score — it is a zero. Encoding it as a weighted
  // rubric line would let a good scenario average away a harm.
  it("makes a mustNot a hard failure, not a weighted line", () => {
    const c = buildPromptfooConfig(FILE, { providerPath: "./p.js", graderModel: "m" }) as PromptfooConfigShape;
    const not = c.tests[0]!.assert.find((a) => a.metric === "MN-1")!;
    expect(not.type).toBe("llm-rubric");
    expect(not.threshold).toBe(1);
  });

  // promptfoo's own weighted mean would otherwise fold mustNot into the
  // mustCover average (as the default weight of 1), diluting the score the
  // verdict reports as "0.8 of mustCover weight". weight: 0 keeps mustNot
  // out of that mean entirely — it still fails outright via `threshold: 1`,
  // which is read straight from componentResults, not from the mean.
  it("gives a mustNot zero weight so it cannot dilute the mustCover mean", () => {
    const c = buildPromptfooConfig(FILE, { providerPath: "./p.js", graderModel: "m" }) as PromptfooConfigShape;
    const not = c.tests[0]!.assert.find((a) => a.metric === "MN-1")!;
    expect(not.weight).toBe(0);
  });

  // An unpinned or missing grader makes a score meaningless between runs —
  // reject it at config build time rather than let it surface as a mystery
  // later.
  it("rejects a blank grader model", () => {
    expect(() => buildPromptfooConfig(FILE, { providerPath: "./p.js", graderModel: "" })).toThrow();
    expect(() => buildPromptfooConfig(FILE, { providerPath: "./p.js", graderModel: "   " })).toThrow();
  });
});
