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

import { afterEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import {
  buildDuration,
  countTasks,
  isLedgerLive,
  ledgerDuration,
  ledgerStatus,
  milestoneLabel,
  taskBreakdown,
} from "./ledger";

type BuildSummary = components["schemas"]["BuildSummary"];
type TaskView = components["schemas"]["TaskView"];
type DeployStage = components["schemas"]["DeployStage"];

const build = (over: Partial<BuildSummary> = {}): BuildSummary => ({
  tag: "v1",
  milestoneNumber: 1,
  status: "completed",
  startedAt: "2026-08-14T16:20:00Z",
  ...over,
});

const task = (tag: string | undefined, over: Partial<TaskView> = {}): TaskView => ({
  issueNumber: 1,
  title: "A task",
  issueUrl: "https://github.com/acme-dev/demo-shop/issues/1",
  executorClass: "coding",
  dependsOn: [],
  lineage: tag ? { specTag: tag } : {},
  derivedStatus: "pending",
  hold: false,
  attention: [],
  executions: {},
  ...over,
});

const deploy = (over: Partial<DeployStage> = {}): DeployStage => ({
  version: "v1",
  status: "deployed",
  components: { total: 3, ready: 3 },
  validation: "passed",
  ...over,
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ledgerStatus", () => {
  it("names the actor while a version is running", () => {
    expect(ledgerStatus(build({ status: "in_progress" }))).toEqual({
      label: "Running · Coding agent",
      tone: "info",
      live: true,
    });
  });

  it("treats `started` as running too", () => {
    expect(ledgerStatus(build({ status: "started" })).live).toBe(true);
  });

  it("carries the terminal reason onto a failed row", () => {
    expect(ledgerStatus(build({ status: "failed", reason: "Merge conflict" })).label).toBe(
      "Failed · Merge conflict",
    );
  });

  it("falls back to a bare Failed when the platform left no reason", () => {
    expect(ledgerStatus(build({ status: "failed" })).label).toBe("Failed");
  });

  it("describes the DEPLOYED version by where it reached", () => {
    expect(ledgerStatus(build({ tag: "v1" }), deploy()).label).toBe(
      "Deployed to development",
    );
    expect(ledgerStatus(build({ tag: "v1" }), deploy({ status: "deploying" })).live).toBe(
      true,
    );
    expect(ledgerStatus(build({ tag: "v1" }), deploy({ status: "failed" })).tone).toBe(
      "error",
    );
  });

  it("says Built for every version the deploy aggregate does not name", () => {
    // The platform records ONE deployed version per project. Describing any
    // other completed version by where it reached would be a guess.
    expect(ledgerStatus(build({ tag: "v2" }), deploy({ version: "v1" })).label).toBe(
      "Built",
    );
    expect(ledgerStatus(build()).label).toBe("Built");
  });

  it("says Built when the named version has not reached an environment", () => {
    expect(ledgerStatus(build({ tag: "v1" }), deploy({ status: "none" })).label).toBe(
      "Built",
    );
  });
});

describe("isLedgerLive", () => {
  it("is true only for the two running states", () => {
    expect(isLedgerLive(build({ status: "in_progress" }))).toBe(true);
    expect(isLedgerLive(build({ status: "started" }))).toBe(true);
    expect(isLedgerLive(build({ status: "completed" }))).toBe(false);
    expect(isLedgerLive(build({ status: "failed" }))).toBe(false);
  });
});

describe("countTasks", () => {
  it("counts a version-scoped list by row state", () => {
    expect(
      countTasks([
        task("v1", { derivedStatus: "merged" }),
        task("v1", { derivedStatus: "merged" }),
        task("v1", { hold: true }),
        task("v1"),
      ]),
    ).toEqual({ total: 4, done: 2, inProgress: 0, inReview: 0, blocked: 1, pending: 1 });
  });

  it("is all zeroes for an empty build", () => {
    expect(countTasks([])).toEqual({
      total: 0, done: 0, inProgress: 0, inReview: 0, blocked: 0, pending: 0,
    });
  });

  it("does not look at lineage at all", () => {
    // The caller scopes the list. Grouping BY lineage is impossible on an
    // untagged read — the server leaves specTag empty when the query spans
    // versions — which is why the ledger has no Tasks column.
    expect(countTasks([task(undefined, { derivedStatus: "merged" })]).done).toBe(1);
  });
});

describe("taskBreakdown", () => {
  it("lists only the buckets that have anything in them", () => {
    expect(
      taskBreakdown({
        total: 11,
        done: 5,
        inProgress: 1,
        inReview: 1,
        blocked: 2,
        pending: 2,
      }),
    ).toBe("5 done · 1 in progress · 1 in review · 2 need config · 2 pending");
  });

  it("drops empty buckets", () => {
    expect(
      taskBreakdown({ total: 6, done: 6, inProgress: 0, inReview: 0, blocked: 0, pending: 0 }),
    ).toBe("6 done");
  });

  it("is empty when there are no counts at all", () => {
    expect(taskBreakdown(undefined)).toBe("");
  });
});

describe("buildDuration", () => {
  it("zero-pads seconds so the column stays aligned", () => {
    expect(buildDuration("2026-08-14T16:20:00Z", "2026-08-14T16:20:04Z")).toBe("0m 04s");
    expect(buildDuration("2026-08-14T16:20:00Z", "2026-08-14T16:38:04Z")).toBe("18m 04s");
  });

  it("switches to hours past sixty minutes", () => {
    expect(buildDuration("2026-08-14T16:20:00Z", "2026-08-14T17:41:12Z")).toBe("1h 21m");
  });

  it("counts up to now when the span has no end", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-14T16:23:41Z"));
    expect(buildDuration("2026-08-14T16:20:00Z")).toBe("3m 41s");
    expect(ledgerDuration(build({ startedAt: "2026-08-14T16:20:00Z" }))).toBe("3m 41s");
  });

  it("is empty rather than negative when the clocks disagree", () => {
    expect(buildDuration("2026-08-14T16:20:00Z", "2026-08-14T16:19:00Z")).toBe("");
    expect(buildDuration(undefined)).toBe("");
    expect(buildDuration("not-a-date", "also-not")).toBe("");
  });

  it("shows an em dash rather than an empty cell", () => {
    expect(ledgerDuration(build({ startedAt: "not-a-date" }))).toBe("—");
  });
});

describe("milestoneLabel", () => {
  it("names the number, which is all the platform records", () => {
    expect(milestoneLabel(build({ milestoneNumber: 3 }))).toBe("Milestone #3");
  });
});
