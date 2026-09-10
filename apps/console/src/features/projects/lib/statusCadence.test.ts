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
import { statusIsMoving } from "./statusCadence";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type DeployStage = components["schemas"]["DeployStage"];

/** A settled project: repo ready, spec written, nothing in flight. Every case
 *  below changes one thing, so what it asserts is that one thing. */
function settled(deploy: Partial<DeployStage> = {}, build = "succeeded"): ProjectStatus {
  return {
    phase: "tasks",
    repoStatus: "ready",
    repoUrl: "https://github.com/acme/widgets",
    hasSpec: true,
    hasDesign: true,
    hasTasks: true,
    specStatus: "approved",
    designStatus: "approved",
    spec: { exists: true, version: "v3", dirty: false, design: true, agent: "" },
    build: { status: build, version: "v3" },
    deploy: {
      version: "v3",
      status: "deployed",
      components: { total: 1, ready: 1 },
      validation: "passed",
      ...deploy,
    },
  } as ProjectStatus;
}

describe("statusIsMoving", () => {
  it("holds it through the validation cycle and the repair that follows one", () => {
    expect(statusIsMoving(settled({ validation: "running" }))).toBe(true);
    expect(statusIsMoving(settled({ validation: "awaiting-fix" }))).toBe(true);
  });

  it("never holds it on `none`, whatever the build says", () => {
    // The regression this pins is a poll that never stops. `none` means both "a
    // verdict is expected" and "none is coming": a validation run that settles
    // without recording one closes the version's task on the way out, so nothing
    // restarts it and the state is `none` for good. `build.status` cannot screen
    // that off — it comes from the newest DEV run, which succeeded — so a clause
    // keyed on `none` would fast-poll such a project on every route, forever.
    for (const build of ["succeeded", "failed", "cancelled", "idle"]) {
      expect(statusIsMoving(settled({ validation: "none" }, build))).toBe(false);
    }
  });

  it("answers for every validation state a deployed version can be in", () => {
    // Exhaustive by TYPE, so a new member of the enum fails to compile here
    // rather than silently picking up whichever cadence the fall-through gives
    // it. Each entry is whether that state should hold the FAST poll.
    const moving: Record<NonNullable<DeployStage["validation"]>, boolean> = {
      none: false, // two states wearing one name — see the test above
      running: true, // a validation cycle is in flight
      "awaiting-fix": true, // the coding cycle repairing a failed one
      passed: false,
      partial: false,
      failed: false,
      inconclusive: false,
      unreported: false,
      skipped: false,
      cancelled: false, // a person stopped the judging; nothing is coming
    };
    for (const [validation, want] of Object.entries(moving)) {
      expect(statusIsMoving(settled({ validation: validation as DeployStage["validation"] })))
        .toBe(want);
    }
  });

  it("still holds it for the states it always did", () => {
    expect(statusIsMoving(settled({}, "running"))).toBe(true);
    expect(statusIsMoving(settled({ status: "deploying" }))).toBe(true);
    // The repo rungs, which have nothing to do with validation and are the
    // clauses a careless move of this predicate would silently drop.
    expect(statusIsMoving({ ...settled(), repoStatus: "pending" })).toBe(true);
    expect(statusIsMoving({ ...settled(), repoStatus: "cloning" })).toBe(true);
    // And the spec-interview clauses.
    expect(
      statusIsMoving({ ...settled(), spec: { ...settled().spec, agent: "working" } }),
    ).toBe(true);
  });
});
