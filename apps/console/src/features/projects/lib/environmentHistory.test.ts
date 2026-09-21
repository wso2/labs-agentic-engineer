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
import type { EnvironmentRow } from "./deploymentLedger";
import type { EnvironmentInfo } from "./environments";
import { historyFor } from "./environmentHistory";

type BuildSummary = components["schemas"]["BuildSummary"];

const env = (over: Partial<EnvironmentInfo> & { name: string; position: number }): EnvironmentInfo => ({
  displayName: over.name,
  isProduction: false,
  validation: "off",
  ...over,
});

const first = env({
  name: "development",
  displayName: "Development",
  position: 0,
  validation: "on",
  promotesTo: "staging",
});
const later = env({ name: "staging", displayName: "Staging", position: 1, promotesTo: "production" });

const STAMPS: Record<string, string> = {
  v1: "2026-09-02T11:05:00Z",
  v2: "2026-09-08T16:12:00Z",
  v3: "2026-09-12T09:10:00Z",
};

const b = (tag: string, over: Partial<BuildSummary> = {}): BuildSummary => ({
  tag,
  milestoneNumber: Number(tag.slice(1)),
  status: "completed",
  startedAt: STAMPS[tag] ?? "2026-09-01T00:00:00Z",
  completedAt: STAMPS[tag] ?? "2026-09-01T00:00:00Z",
  ...over,
});

const rowRunning = (version: string, environment = "development"): EnvironmentRow => ({
  environment,
  label: environment,
  version,
  cards: [{ componentName: "api", displayName: "api", kind: "success", deployment: { componentName: "api" } }],
  status: { label: "Deployed", tone: "success", live: false },
  live: 1,
  total: 1,
  deployedAt: "2026-09-12T09:40:00Z",
});

describe("historyFor", () => {
  it("gives the entry environment the build ledger, newest first, marking the live version", () => {
    const view = historyFor(first, rowRunning("v3"), [b("v1"), b("v2"), b("v3")]);
    expect(view.unrecorded).toBe(false);
    expect(view.rows.map((r) => r.version)).toEqual(["v3", "v2", "v1"]);
    expect(view.rows[0]?.current).toBe(true);
    expect(view.rows.map((r) => r.milestoneNumber)).toEqual([3, 2, 1]);
  });

  it("leaves every superseded row undated — the platform recorded no stamp for it", () => {
    // The regression guard: a build's finish time is when the BUILD ended, not
    // when that version started or stopped serving, so it must never be
    // substituted for a deploy stamp the platform never kept.
    const view = historyFor(first, rowRunning("v3"), [b("v1"), b("v2"), b("v3")]);
    expect(view.rows[0]?.deployedAt).toBe("2026-09-12T09:40:00Z");
    expect(view.rows[1]?.deployedAt).toBeUndefined();
    expect(view.rows[2]?.deployedAt).toBeUndefined();
  });

  it("dates the live row by its binding, not by when its build finished", () => {
    const view = historyFor(first, rowRunning("v3"), [b("v3")]);
    expect(view.rows[0]?.deployedAt).toBe("2026-09-12T09:40:00Z");
  });

  it("lists no version whose build never produced one", () => {
    // A failed or cancelled build never reached the environment, so it is not
    // a deployment that happened — it is a deployment that did not.
    const view = historyFor(first, rowRunning("v3"), [
      b("v1"),
      b("v2", { status: "failed" }),
      b("v3"),
    ]);
    expect(view.rows.map((r) => r.version)).toEqual(["v3", "v1"]);
  });

  it("still lists the version running now when the build ledger lost its tag", () => {
    const view = historyFor(first, rowRunning("v9"), [b("v1")]);
    expect(view.rows[0]).toMatchObject({ version: "v9", current: true });
    expect(view.rows.map((r) => r.version)).toEqual(["v9", "v1"]);
  });

  it("reads the milestone of a running version the ledger has not completed", () => {
    // A version still building is not a PAST deployment, so it is filtered out
    // of the fold — but it is what runs here, so it gets its row back. Its
    // milestone is in the ledger either way, and section 1 reads it from the
    // same place: the two must not print different answers for one version.
    const view = historyFor(first, rowRunning("v3"), [
      b("v1"),
      b("v3", { status: "in_progress" }),
    ]);
    expect(view.rows[0]).toMatchObject({ version: "v3", current: true, milestoneNumber: 3 });
  });

  it("gives a later environment only what runs now, and says the rest is unrecorded", () => {
    const view = historyFor(later, rowRunning("v2", "staging"), [b("v1"), b("v2"), b("v3")]);
    expect(view.unrecorded).toBe(true);
    expect(view.rows).toHaveLength(1);
    expect(view.rows[0]).toMatchObject({ version: "v2", current: true });
  });

  it("never invents a later environment's past from the build ledger", () => {
    const view = historyFor(later, rowRunning("v2", "staging"), [b("v1"), b("v2"), b("v3")]);
    expect(view.rows.map((r) => r.version)).not.toContain("v1");
    expect(view.rows.map((r) => r.version)).not.toContain("v3");
    expect(view.rows[0]?.milestoneNumber).toBeUndefined();
  });

  it("gives an empty later environment no rows at all, rather than a blank one", () => {
    const view = historyFor(later, undefined, [b("v1")]);
    expect(view.rows).toEqual([]);
    expect(view.unrecorded).toBe(true);
  });

  it("names no version for a later environment whose binding names none", () => {
    const row = rowRunning("v2", "staging");
    delete row.version;
    const view = historyFor(later, row, [b("v1")]);
    expect(view.rows).toHaveLength(1);
    expect(view.rows[0]?.version).toBeUndefined();
  });

  it("holds its peace while the build ledger is still out", () => {
    // `undefined` builds is a read that has not answered. Folding it to an
    // empty ledger would tell the entry environment nothing ever ran there.
    const view = historyFor(first, rowRunning("v3"), undefined);
    expect(view.pending).toBe(true);
    expect(view.rows).toEqual([]);
  });
});
