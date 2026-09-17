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
  decidingStep,
  isReportParseError,
  parseAcceptanceReport,
  scenarioKey,
  type AcceptanceReport,
} from "./report.js";

function ok(raw: string): AcceptanceReport {
  const r = parseAcceptanceReport(raw);
  if (isReportParseError(r)) throw new Error(`expected a report, got: ${r.error}`);
  return r;
}

const BLOCKED = {
  feature: "Bought items",
  featureFile: "specs/acceptance/bought-items.feature",
  line: 12,
  rule: "A bought item is locked from further edits",
  scenario: "Editing a bought item is refused",
  tags: ["@negative"],
  outcome: "blocked",
  steps: [
    { text: "the list has a bought item", keyword: "Given", command: "POST /items", exit: 0 },
    {
      text: "Dev tries to change the quantity",
      keyword: "When",
      command: "agent-browser wait --fn '(() => ...)'",
      exit: 0,
      observed: "the Actions cell is a literal em dash and the row holds zero buttons",
    },
    { text: "the quantity is unchanged", keyword: "Then", observed: "nothing was ever attempted" },
  ],
};

describe("parseAcceptanceReport", () => {
  it("reads the run-level facts the design shows as provenance", () => {
    const r = ok(
      JSON.stringify({
        schemaVersion: 2,
        generatedAt: "2026-09-15T05:25:05Z",
        commit: "6b2964f1074d23baf628bf1f169c8ceaba993c2d",
        baseUrl: "http://shopping-list.localhost:19080/",
        isolation: "Each scenario created the list it asserts on.",
        scenarios: [BLOCKED],
      }),
    );
    expect(r.schemaVersion).toBe(2);
    expect(r.commit).toBe("6b2964f1074d23baf628bf1f169c8ceaba993c2d");
    expect(r.isolation).toBe("Each scenario created the list it asserts on.");
    expect(r.scenarios).toHaveLength(1);
  });

  it("indexes by identity, not by line, because the two sides are read at different commits", () => {
    const r = ok(JSON.stringify({ scenarios: [BLOCKED] }));
    const key = scenarioKey(BLOCKED.feature, BLOCKED.rule, BLOCKED.scenario);
    expect(r.byKey.get(key)?.outcome).toBe("blocked");
    expect(key).not.toContain("12");
  });

  it("tells an absent exit from an exit of 0", () => {
    // The Go struct uses a pointer for this, and collapsing the two would
    // render a step that never ran as one that succeeded.
    const r = ok(JSON.stringify({ scenarios: [BLOCKED] }));
    const steps = r.scenarios[0]?.steps ?? [];
    expect(steps[0]?.exit).toBe(0);
    expect(steps[2]?.exit).toBeUndefined();
    expect("exit" in (steps[2] as object)).toBe(false);
  });

  it("keeps an outcome word it does not recognise, rather than guessing at one", () => {
    const r = ok(JSON.stringify({ scenarios: [{ ...BLOCKED, outcome: "abandoned" }] }));
    expect(r.scenarios[0]?.outcome).toBe("abandoned");
  });

  it("drops an unreadable entry and renders the rest", () => {
    // Written by an agent: throwing on one bad entry would show nothing for the
    // good ones.
    const r = ok(JSON.stringify({ scenarios: [BLOCKED, { rule: "no scenario name" }, null, 7] }));
    expect(r.scenarios).toHaveLength(1);
  });

  it("drops a step with no text but keeps its scenario", () => {
    const r = ok(
      JSON.stringify({ scenarios: [{ ...BLOCKED, steps: [{ keyword: "Then" }, { text: "it holds", keyword: "Then" }] }] }),
    );
    expect(r.scenarios[0]?.steps.map((s) => s.text)).toEqual(["it holds"]);
  });

  it("fails only when there is nothing to render", () => {
    expect(isReportParseError(parseAcceptanceReport("{"))).toBe(true);
    expect(isReportParseError(parseAcceptanceReport("[]"))).toBe(true);
    expect(isReportParseError(parseAcceptanceReport(JSON.stringify({ criteria: [] })))).toBe(true);
    expect(isReportParseError(parseAcceptanceReport(JSON.stringify({ scenarios: [] })))).toBe(false);
  });
});

describe("decidingStep", () => {
  it("prefers the first step that exited nonzero", () => {
    const r = ok(
      JSON.stringify({
        scenarios: [
          {
            ...BLOCKED,
            steps: [
              { text: "a", keyword: "Given", command: "x", exit: 0, observed: "set up" },
              { text: "b", keyword: "When", command: "y", exit: 1, observed: "no such button" },
            ],
          },
        ],
      }),
    );
    expect(decidingStep(r.scenarios[0]!)?.observed).toBe("no such button");
  });

  it("falls back to the first step that observed anything", () => {
    const r = ok(JSON.stringify({ scenarios: [BLOCKED] }));
    expect(decidingStep(r.scenarios[0]!)?.keyword).toBe("When");
  });

  it("has nothing to say about a scenario that recorded neither", () => {
    const r = ok(
      JSON.stringify({ scenarios: [{ ...BLOCKED, steps: [{ text: "a", keyword: "Then", command: "x", exit: 0 }] }] }),
    );
    expect(decidingStep(r.scenarios[0]!)).toBeUndefined();
  });
});
