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
import {
  answeredRun,
  isRepairing,
  lastMergedValidationCycle,
  validatingRun,
} from "./runs";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];

// Origin is still on the wire beside kind, so a fixture that set one without the
// other would not be a row the platform can produce.
const ORIGIN_FOR_KIND: Record<
  MilestoneRunView["kind"],
  MilestoneRunView["origin"]
> = {
  dev: "spec-build",
  task: "incident-adoption",
  validation: "revalidate",
};

function run(
  id: string,
  kind: MilestoneRunView["kind"],
  cycles: MilestoneRunView["cycles"] = [],
): MilestoneRunView {
  return {
    id,
    milestoneNumber: 1,
    milestoneTitle: "v1",
    kind,
    origin: ORIGIN_FOR_KIND[kind],
    state: "succeeded",
    budgets: {
      cyclesTotal: cycles.length,
      cycleCeiling: 8,
      fixCycles: 0,
      conflictCycles: 0,
      buildRetriggers: 0,
      validationCycles: cycles.filter((c) => c.kind === "validation").length,
    },
    validation: {},
    cycles,
    createdAt: "2026-07-10T09:00:00Z",
  };
}

type Cycle = NonNullable<MilestoneRunView["cycles"]>[number];

function cycle(id: string, kind: Cycle["kind"], mergeSha?: string): Cycle {
  return {
    id,
    kind,
    attempts: 1,
    ...(mergeSha ? { mergeSha } : {}),
    createdAt: "2026-07-10T10:00:00Z",
  };
}

describe("validatingRun", () => {
  // #423: an incident adoption never validates, and `settle` stamps `skipped` on any
  // succeeded run that never did — so the newest run is routinely one whose verdict
  // means "I was never asked". Reading it reported a genuinely passed version as
  // unvalidated. The page was fixed; the deployments hook still read runs[0].
  it("skips a newer NON-validating run to find the one that asked", () => {
    const runs = [
      run("run-incident", "task"),
      run("run-spec", "dev", [cycle("c2", "validation", "abc")]),
    ];
    expect(validatingRun(runs)?.id).toBe("run-spec");
  });

  // A revalidation exists to ask again, so when one is newer it owns the answer.
  it("prefers the newest run that DOES validate", () => {
    const runs = [run("run-reval", "validation"), run("run-spec", "dev")];
    expect(validatingRun(runs)?.id).toBe("run-reval");
  });

  it("is undefined when no run on the version ever asked", () => {
    expect(validatingRun([run("run-incident", "task")])).toBeUndefined();
  });
});

describe("lastMergedValidationCycle", () => {
  // A repeat attempt in flight has no report yet and carries no mergeSha; pinning to
  // it passes an empty ref, which degrades to a branch-tip read — the one thing the
  // pin exists to prevent.
  it("skips an attempt still in flight and takes the last MERGED one", () => {
    const runs = [
      run("run-1", "dev", [
        cycle("c1", "coding", "aaa"),
        cycle("c2", "validation", "bbb"),
        cycle("c3", "coding", "ccc"),
        cycle("c4", "validation"), // in flight: no mergeSha
      ]),
    ];
    expect(lastMergedValidationCycle(runs)?.id).toBe("c2");
  });

  // A version can be judged more than once, and a revalidation is a LATER run on the
  // same milestone — so the newest attempt is not always inside the newest run.
  it("looks across runs, newest attempt wins", () => {
    const runs = [
      run("run-reval", "validation", [cycle("c9", "validation", "zzz")]),
      run("run-spec", "dev", [cycle("c2", "validation", "bbb")]),
    ];
    expect(lastMergedValidationCycle(runs)?.id).toBe("c9");
  });

  // The counterpart of validatingRun's case: the attempt lives on an OLDER run, and
  // a newer non-validating one must not hide it.
  it("finds an attempt under an older run when a newer one never validated", () => {
    const runs = [
      run("run-incident", "task", [cycle("c5", "coding", "ddd")]),
      run("run-spec", "dev", [cycle("c2", "validation", "bbb")]),
    ];
    expect(lastMergedValidationCycle(runs)?.id).toBe("c2");
  });

  it("is undefined when nothing has merged an attempt yet", () => {
    const runs = [run("run-1", "dev", [cycle("c2", "validation")])];
    expect(lastMergedValidationCycle(runs)).toBeUndefined();
  });
});

// A revalidation is a fresh run row on the same milestone: it enters the loop at
// validation with an empty verdict while the run that delivered the version still
// holds `passed`. Reading the ASKING run's verdict there reported a validated version
// as having nothing to show.
describe("answeredRun / isRepairing", () => {
  const revalidating = () => {
    const reval = run("run-reval", "validation");
    const spec = run("run-spec", "dev", [cycle("c2", "validation", "bbb")]);
    spec.validation = { verdict: "passed" };
    return [reval, spec];
  };

  it("finds the verdict on an older run while a revalidation is in flight", () => {
    const runs = revalidating();
    expect(validatingRun(runs)?.id).toBe("run-reval"); // who is being asked
    expect(answeredRun(runs)?.id).toBe("run-spec"); // who last answered
  });

  // The two are the SAME run only in the self-heal loop, which repeats within one
  // row — which is what lets the copy claim a fix has been deployed.
  it("calls a revalidation a re-ask, not a repair", () => {
    expect(isRepairing(revalidating())).toBe(false);
  });

  it("calls a repeat on the answering run a repair", () => {
    const healing = run("run-spec", "dev", [cycle("c2", "validation", "bbb")]);
    healing.validation = { verdict: "failed" };
    expect(isRepairing([healing])).toBe(true);
  });

  it("is neither when nothing has answered yet", () => {
    const first = run("run-spec", "dev");
    expect(answeredRun([first])).toBeUndefined();
    expect(isRepairing([first])).toBe(false);
  });

  // `skipped` IS an answer — the version was reached and passed over, which is a
  // result a revalidation exists to replace.
  it("counts skipped as an answer", () => {
    const skipped = run("run-spec", "dev");
    skipped.validation = { verdict: "skipped" };
    expect(answeredRun([skipped])?.id).toBe("run-spec");
  });
});
