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
import { fixedBy, versionRows, versionState } from "./ledger";

const summaries = [
  { tag: "v1.1", status: "in_progress" as const, startedAt: "2026-10-02T09:00:00Z" },
  { tag: "v1", status: "completed" as const, startedAt: "2026-10-01T09:00:00Z", completedAt: "2026-10-01T09:40:00Z" },
];
const builds = [
  { version: "v1", features: [{ id: "F1", name: "Submit expenses", lines: [] }, { id: "F2", name: "Approvals", lines: [] }] },
  { version: "v1.1", fixes: "v1", features: [{ id: "F1", name: "Submit expenses", lines: [] }, { id: "F2", name: "Approvals", lines: [] }] },
];

describe("the version ledger", () => {
  const rows = versionRows(summaries, builds);

  it("lists the versions newest first, with what each built, what a repair fixes, and when it ran", () => {
    expect(rows).toEqual([
      {
        version: "v1.1",
        status: "in_progress",
        fixes: "v1",
        features: ["Submit expenses", "Approvals"],
        featureIds: ["F1", "F2"],
        regressions: 0,
        startedAt: "2026-10-02T09:00:00Z",
        completedAt: null,
      },
      {
        version: "v1",
        status: "completed",
        fixes: null,
        features: ["Submit expenses", "Approvals"],
        featureIds: ["F1", "F2"],
        regressions: 0,
        startedAt: "2026-10-01T09:00:00Z",
        completedAt: "2026-10-01T09:40:00Z",
      },
    ]);
    expect(fixedBy(rows, "v1")?.version).toBe("v1.1");
    expect(fixedBy(rows, "v1.1")).toBeNull();
  });

  it("says each version's state in words: building, passing, how many fail, or failed", () => {
    const failing = [{ featureId: "F2", featureName: "Approvals", story: "F2.4", name: "A deputy approves", standing: null }];
    expect(versionState("in_progress", null)).toEqual({ label: "building", tone: "primary" });
    expect(versionState("completed", { passed: 10, total: 11, failing, regressions: 0 })).toEqual({ label: "1 failing", tone: "warning" });
    expect(versionState("completed", { passed: 11, total: 11, failing: [], regressions: 0 })).toEqual({ label: "passing", tone: "success" });
    expect(versionState("completed", null)).toEqual({ label: "built", tone: null });
    expect(versionState("failed", null)).toEqual({ label: "failed", tone: "error" });
  });
});

describe("regressions on the ledger row", () => {
  it("adds the regression count to a failing version's label", () => {
    const failing = [
      { featureId: "F2", featureName: "Approvals", story: "F2.1", name: "The queue", standing: "regression" as const },
      { featureId: "F3", featureName: "Payroll", story: null, name: "Export", standing: null },
    ];
    expect(versionState("completed", { passed: 9, total: 11, failing, regressions: 1 }, 1)).toEqual({
      label: "2 failing · 1 regression",
      tone: "warning",
    });
  });
});
