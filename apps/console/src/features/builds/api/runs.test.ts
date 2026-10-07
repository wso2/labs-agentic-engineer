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
import { deliveryRun, deliveryRuns } from "./runs";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];

const run = (id: string, kind: MilestoneRunView["kind"], cycleKinds: string[]): MilestoneRunView =>
  ({ id, kind, state: "succeeded", cycles: cycleKinds.map((k, i) => ({ id: `${id}-${i}`, kind: k })) }) as unknown as MilestoneRunView;

describe("the runs that delivered a version", () => {
  it("keeps every run that built something, newest first, and leaves out one that only re-judged it", () => {
    const revalidation = run("r3", "validation", ["validation"]);
    const repairing = run("r2", "validation", ["validation", "coding"]);
    const dev = run("r1", "dev", ["coding", "validation"]);
    expect(deliveryRuns([revalidation, repairing, dev]).map((r) => r.id)).toEqual(["r2", "r1"]);
    expect(deliveryRun([revalidation, repairing, dev])?.id).toBe("r2");
  });

  it("keeps a dev run that stopped before it started any session", () => {
    expect(deliveryRuns([run("r1", "dev", [])]).map((r) => r.id)).toEqual(["r1"]);
  });
});
