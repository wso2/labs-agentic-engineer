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
import { buildRefusal, BuildRefusedError, projectBuilds } from "./builds";

// How a failed build start reads, as the old console's useBuildProject reads it:
// the envelope's message, and the gate's 422 detail rows kept as a checklist.

describe("buildRefusal", () => {
  it("keeps the gate's detail rows, so the refusal renders as a checklist", () => {
    const err = buildRefusal({
      code: "validation_failed",
      message: "Not ready to build yet.",
      details: [
        { field: "body.selection.F3", message: "Payroll export: waiting on Xero" },
        { message: "Something without a field" },
      ],
    });
    expect(err).toBeInstanceOf(BuildRefusedError);
    expect(err.message).toBe("Not ready to build yet.");
    expect(err.problems).toEqual([
      { field: "body.selection.F3", message: "Payroll export: waiting on Xero" },
      { message: "Something without a field" },
    ]);
  });

  it("is the plain message when there are no details (a build already running)", () => {
    const err = buildRefusal({ code: "build_in_progress", message: "A build is already running for this project." });
    expect(err.message).toBe("A build is already running for this project.");
    expect(err.problems).toEqual([]);
  });

  it("falls back when the failure carried no envelope", () => {
    expect(buildRefusal(undefined).message).toBe("Failed to start the build");
  });
});

describe("projectBuilds", () => {
  const version = (name: string, fixes?: string) => ({
    name,
    features: [{ id: "F1", name: "Claims", lines: [{ id: "F1.1", words: "F1.1 Submit a claim." }, { words: "A receipt is required." }] }],
    productWide: ["P1"],
    heldBack: [],
    ...(fixes ? { fixes } : {}),
  });
  const row = (tag: string, status: "in_progress" | "completed" | "failed" | "cancelled") => ({ tag, status, milestoneNumber: 1, startedAt: "2026-10-01T00:00:00Z" });

  it("joins what each version built with how its run went, oldest first", () => {
    const builds = projectBuilds(
      [version("v1"), version("v1.1", "v1"), version("v2")],
      [row("v2", "in_progress"), row("v1.1", "completed"), row("v1", "failed")],
    );
    expect(builds.map((b) => [b.version, b.status, b.fixes])).toEqual([
      ["v1", "failed", undefined],
      ["v1.1", "built", "v1"],
      ["v2", "building", undefined],
    ]);
    expect(builds[0]!.features[0]!.lines).toEqual([
      { id: "F1.1", words: "F1.1 Submit a claim." },
      { id: null, words: "A receipt is required." },
    ]);
  });

  it("reads a version with no ledger row yet as building, and a cancelled one as failed", () => {
    expect(projectBuilds([version("v1"), version("v2")], [row("v1", "cancelled")]).map((b) => b.status)).toEqual([
      "failed",
      "building",
    ]);
  });
});
