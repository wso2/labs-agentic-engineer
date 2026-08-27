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
import { buildRunsForTag, projectBuilds } from "./project";

/**
 * A mock is only worth having while it cannot contradict itself. This one
 * served `/builds/v3/runs` as an envelope tagged `v3` carrying a run that said
 * it belonged to v1, and then — once identity was stamped — served a run still
 * WAITING for versions the ledger showed as finished. Both are the same failure:
 * the run page and the ledger row disagreeing about one version.
 */
describe("buildRunsForTag", () => {
  const ledger = projectBuilds.building.builds ?? [];

  it("covers every tag the building scenario's ledger shows", () => {
    expect(ledger.map((b) => b.tag)).toEqual(["v3", "v2", "v1"]);
  });

  it("answers each ledger tag with its OWN identity", () => {
    for (const build of ledger) {
      const list = buildRunsForTag("building", build.tag);
      expect(list.tag).toBe(build.tag);
      expect(list.milestoneNumber).toBe(build.milestoneNumber);
      for (const run of list.runs ?? []) {
        expect(run.milestoneNumber).toBe(build.milestoneNumber);
        expect(run.milestoneTitle).toBe(build.tag);
      }
    }
  });

  it("tells a story that agrees with the version's own status", () => {
    // v3 is in progress, v2 failed, v1 completed — a waiting run under all
    // three is what this test exists to stop.
    expect(buildRunsForTag("building", "v3").runs?.[0]?.state).toBe("waiting");
    expect(buildRunsForTag("building", "v2").runs?.[0]?.state).toBe("failed");
    expect(buildRunsForTag("building", "v1").runs?.[0]?.state).toBe("succeeded");
  });

  it("keeps run ids unique when a story carries several runs", () => {
    // `run-${tag}-1` for every record collapsed distinct history rows onto one
    // identity, which the console upserts by id.
    for (const build of ledger) {
      const ids = (buildRunsForTag("building", build.tag).runs ?? []).map((r) => r.id);
      expect(new Set(ids).size).toBe(ids.length);
    }
  });

  it("answers an empty run list for a version the scenario never built", () => {
    // What the real server answers for a version it has no runs for.
    const list = buildRunsForTag("building", "v99");
    expect(list.runs).toEqual([]);
    expect(list.tag).toBe("v99");
  });

  it("still answers the single-version scenarios", () => {
    const list = buildRunsForTag("deployed", "v1");
    expect(list.tag).toBe("v1");
    expect((list.runs ?? []).length).toBeGreaterThan(0);
  });
});
