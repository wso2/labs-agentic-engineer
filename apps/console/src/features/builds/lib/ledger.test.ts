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
  isAwaitingValues,
  isDeployable,
  isDurationOpen,
  isLedgerLive,
  ledgerDuration,
  ledgerStatus,
  milestoneLabel,
  taskBreakdown,
} from "./ledger";

type BuildSummary = components["schemas"]["BuildSummary"];
type TaskView = components["schemas"]["TaskView"];
type DeployStage = components["schemas"]["DeployStage"];
type RunCycleView = components["schemas"]["RunCycleView"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];

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

  it("says a parked version is waiting on the reader, not that an agent is running", () => {
    // ADR-0023's deploy gate: the run stopped and only a person can end it.
    // The wording is the build page's own pill, so the two surfaces agree.
    expect(
      ledgerStatus(build({ status: "in_progress", waitingReason: "external-values" })),
    ).toEqual({ label: "Waiting for configuration", tone: "warning", live: false });
  });

  it("treats `started` as running too", () => {
    expect(ledgerStatus(build({ status: "started" })).live).toBe(true);
  });

  // A cancel is not a failure, and the row has to say so. It rendered as
  // "Failed" until the API gained its own `cancelled` status — with no reason
  // beside it, because a cancel writes none — while the same page's run row two
  // lines below already read "Cancelled" off the same database row.
  it("reads a cancelled version as cancelled, not failed", () => {
    const status = ledgerStatus(build({ status: "cancelled" }));
    expect(status.label).toBe("Cancelled");
    expect(status.tone).toBe("neutral");
    expect(status.live).toBe(false);
  });

  // The status is a contract enum value, so a missing case would have the
  // console answer "Unknown" to a state the API had just named — which is what
  // the `default` branch is for and exactly what it must not catch.
  it("does not fall through to Unknown for a cancelled version", () => {
    expect(ledgerStatus(build({ status: "cancelled" })).label).not.toBe("Unknown");
  });

  // The BFF and this bundle deploy as separate units, so a status the console
  // has never heard of means the API is ahead. Shrugging "Unknown" at it is how
  // `cancelled` looked before this bundle knew the word — worse than the
  // "Failed" it replaced — so an unrecognised value shows what the API said.
  it("labels an unknown status with what the API actually said", () => {
    // @ts-expect-error — deliberately a value outside the contract enum, which
    // is the whole point: this is the shape of a BFF newer than the bundle.
    const status = ledgerStatus(build({ status: "quarantined_pending_review" }));
    expect(status.label).toBe("Quarantined pending review");
    expect(status.tone).toBe("neutral");
    expect(status.live).toBe(false);
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

describe("isAwaitingValues", () => {
  it("separates a parked version from a running one, which share a status", () => {
    expect(
      isAwaitingValues(build({ status: "in_progress", waitingReason: "external-values" })),
    ).toBe(true);
    expect(isAwaitingValues(build({ status: "in_progress" }))).toBe(false);
  });
});

describe("isLedgerLive", () => {
  it("keeps a parked run in flight — it is still polled and still cancellable", () => {
    // Deliberately not `LedgerStatus.live`: the row goes quiet because nothing
    // is moving, but the RUN is unfinished, resumes on its own when the last
    // value is saved, and Cancel still acts on it.
    const parked = build({ status: "in_progress", waitingReason: "external-values" });
    expect(isLedgerLive(parked)).toBe(true);
    expect(ledgerStatus(parked).live).toBe(false);
  });

  it("is true only for the two running states", () => {
    expect(isLedgerLive(build({ status: "in_progress" }))).toBe(true);
    expect(isLedgerLive(build({ status: "started" }))).toBe(true);
    expect(isLedgerLive(build({ status: "completed" }))).toBe(false);
    expect(isLedgerLive(build({ status: "failed" }))).toBe(false);
    expect(isLedgerLive(build({ status: "cancelled" }))).toBe(false);
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

describe("isDurationOpen", () => {
  it("is open exactly while the build has no end — that is when the clock is now", () => {
    expect(isDurationOpen(build({ completedAt: null }))).toBe(true);
    expect(isDurationOpen(build({ completedAt: "2026-08-14T16:38:04Z" }))).toBe(false);
  });

  it("stays open for a build the ledger no longer calls live", () => {
    // The screenshot that reported this: `status` had already left
    // started/in_progress, but `completedAt` was absent, so the Duration cell
    // was still being measured against now — and had to keep counting.
    // Keying the ticker on `isLedgerLive` would have frozen exactly this case.
    const stray = build({ status: "completed", completedAt: null });
    expect(isLedgerLive(stray)).toBe(false);
    expect(isDurationOpen(stray)).toBe(true);
  });

  it("is closed when there is no span to measure at all", () => {
    expect(isDurationOpen(build({ startedAt: "" }))).toBe(false);
  });
});

describe("isDeployable", () => {
  const cycle = (over: Partial<RunCycleView> = {}): RunCycleView => ({
    id: "cycle-1",
    kind: "coding",
    attempts: 1,
    createdAt: "2026-08-28T09:36:00Z",
    ...over,
  });

  const run = (cycles: RunCycleView[], over: Partial<MilestoneRunView> = {}): MilestoneRunView => ({
    id: "run-1",
    milestoneNumber: 1,
    milestoneTitle: "Milestone 1",
    kind: "dev",
    state: "succeeded",
    origin: "spec-build",
    createdAt: "2026-08-28T09:35:04Z",
    cycles,
    budgets: {
      cyclesTotal: 1,
      cycleCeiling: 8,
      fixCycles: 0,
      conflictCycles: 0,
      buildRetriggers: 0,
      validationCycles: 0,
    },
    validation: {},
    ...over,
  });

  it("withholds the Deployments link until a pull request has actually merged", () => {
    // The reported bug: the card offered a board that had nothing to show for
    // this version, one line above a note saying it deploys as its tasks merge.
    expect(isDeployable(build(), [run([cycle({ mergeSha: "" })])], undefined)).toBe(false);
    expect(isDeployable(build(), [run([cycle({ mergeSha: "abc1234" })])], undefined)).toBe(true);
  });

  it("is not fooled by closed issues that no pull request ever produced", () => {
    // Found by deploying: a cancelled run had BOTH its tasks reading
    // `derivedStatus: "merged"` — that field only says the issue is closed —
    // while its single cycle carried `prNumber` 0 and no merge SHA. Counting
    // "merged" tasks would have called this version deployable.
    expect(
      isDeployable(build(), [run([cycle({ prNumber: 0, mergeSha: "" })], { state: "cancelled" })], undefined),
    ).toBe(false);
  });

  it("asks EVERY run of the version, not just the newest", () => {
    // Also found by deploying: a version whose coding cycle merged pull request
    // #15 was later reworked by a `task` run that opened no cycle at all.
    // Asking only the newest run made merged code look unmerged — and a later
    // run cannot un-merge what is already in the repository.
    const merged = run([cycle({ prNumber: 15, mergeSha: "c185b23" })], { id: "older" });
    const rework = run([], { id: "newer", kind: "task", state: "cancelled" });
    expect(isDeployable(build(), [rework, merged], undefined)).toBe(true);
  });

  it("stays away when there is no run to have merged anything", () => {
    expect(isDeployable(build(), undefined, undefined)).toBe(false);
    expect(isDeployable(build(), [], undefined)).toBe(false);
    expect(isDeployable(build(), [run([])], undefined)).toBe(false);
  });

  it("ignores a validation cycle's SHA — it names the commit it judged", () => {
    expect(
      isDeployable(build(), [run([cycle({ kind: "validation", mergeSha: "abc1234" })])], undefined),
    ).toBe(false);
  });

  it("offers the link regardless once the deploy names this version", () => {
    // Whatever the runs say, a version the platform has deployed is on the
    // Deployments board by definition.
    expect(isDeployable(build({ tag: "v1" }), [], deploy({ version: "v1" }))).toBe(true);
    expect(isDeployable(build({ tag: "v2" }), [], deploy({ version: "v1" }))).toBe(false);
  });
});

describe("milestoneLabel", () => {
  it("names the number, which is all the platform records", () => {
    expect(milestoneLabel(build({ milestoneNumber: 3 }))).toBe("Milestone #3");
  });
});
