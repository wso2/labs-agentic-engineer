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
import type { components } from "../../../generated/aep-api";
import { attemptResult, attemptsOf, noAttemptsReason, revalidateRefusal, validationIsLive, verdictView } from "./attempts";

type RunCycleView = components["schemas"]["RunCycleView"];

const cycle = (id: string, rest: Partial<RunCycleView> = {}): RunCycleView => ({
  id,
  kind: "validation",
  attempts: 1,
  createdAt: "2026-10-03T18:00:00Z",
  ...rest,
});

describe("a version's attempts", () => {
  it("lists every attempt newest first, numbered from the oldest across runs", () => {
    // Newest run first; each run's cycles oldest first.
    const runs = [
      { id: "revalidation", cycles: [cycle("c4")] },
      { id: "dev", cycles: [cycle("c1"), cycle("c2"), cycle("c3")] },
    ];
    expect(attemptsOf(runs).map((a) => [a.number, a.cycle.id, a.runId])).toEqual([
      [4, "c4", "revalidation"],
      [3, "c3", "dev"],
      [2, "c2", "dev"],
      [1, "c1", "dev"],
    ]);
    expect(attemptsOf([])).toEqual([]);
  });

  it("says each attempt's result: in flight, its verdict, or that it never landed", () => {
    expect(attemptResult(cycle("c"))).toEqual({ label: "Validating", tone: "primary" });
    const ended = "2026-10-03T18:30:00Z";
    expect(attemptResult(cycle("c", { endedAt: ended, mergeSha: "a", validationVerdict: "failed" }))).toEqual({ label: "Failed", tone: "error" });
    expect(attemptResult(cycle("c", { endedAt: ended, mergeSha: "a", validationVerdict: "passed" }))).toEqual({ label: "Passed", tone: "success" });
    expect(attemptResult(cycle("c", { endedAt: ended }))).toEqual({ label: "Did not land", tone: "error" });
  });
});

describe("a version's validation state", () => {
  it("names every state but none, which a surface says in words", () => {
    expect(verdictView("awaiting-fix")).toEqual({ label: "Awaiting fix", tone: "warning" });
    expect(verdictView("partial")?.tone).toBe("success");
    expect(verdictView("none")).toBeNull();
  });

  it("is live while a judging or its repair is in flight", () => {
    expect(validationIsLive("running")).toBe(true);
    expect(validationIsLive("awaiting-fix")).toBe(true);
    expect(validationIsLive("none")).toBe(false);
    expect(validationIsLive("failed")).toBe(false);
  });
});

describe("validating a version again", () => {
  it("is refused while a run works the version, or for a version not deployed", () => {
    expect(revalidateRefusal({ live: true, deployed: true })).toBe("A run is already working this version.");
    expect(revalidateRefusal({ live: false, deployed: false })).toMatch(/Only the deployed version/);
    expect(revalidateRefusal({ live: false, deployed: true })).toBeNull();
  });

  it("says why there is no attempt yet", () => {
    expect(noAttemptsReason("skipped", false)).toMatch(/no acceptance scenarios/);
    expect(noAttemptsReason("none", true)).toMatch(/Once it deploys/);
    expect(noAttemptsReason("none", false)).toMatch(/Revalidate/);
  });
});
