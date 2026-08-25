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
  commitLine,
  isLedgerLive,
  ledgerDuration,
  ledgerStarted,
  ledgerStatus,
  secondsDuration,
  taskBreakdown,
  taskProgress,
} from "./ledger";

type BuildSummary = components["schemas"]["BuildSummary"];

const build = (over: Partial<BuildSummary> = {}): BuildSummary => ({
  tag: "v1",
  milestoneNumber: 1,
  status: "completed",
  startedAt: "2026-08-14T16:20:00Z",
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

  it("describes a completed version by where it reached", () => {
    expect(ledgerStatus(build({ deployedTo: ["development"] })).label).toBe(
      "Deployed to development",
    );
  });

  it("counts environments rather than listing them", () => {
    expect(
      ledgerStatus(build({ deployedTo: ["development", "production"] })).label,
    ).toBe("Deployed to 2 environments");
  });

  it("says Built for a version that never reached one", () => {
    expect(ledgerStatus(build({ deployedTo: [] })).label).toBe("Built");
    expect(ledgerStatus(build()).label).toBe("Built");
  });

  it("marks a queued version as waiting, not moving", () => {
    expect(ledgerStatus(build({ status: "queued" }))).toEqual({
      label: "Queued · next",
      tone: "neutral",
      live: false,
    });
  });
});

describe("isLedgerLive", () => {
  it("is true only for the two running states", () => {
    expect(isLedgerLive(build({ status: "in_progress" }))).toBe(true);
    expect(isLedgerLive(build({ status: "started" }))).toBe(true);
    expect(isLedgerLive(build({ status: "queued" }))).toBe(false);
    expect(isLedgerLive(build({ status: "completed" }))).toBe(false);
    expect(isLedgerLive(build({ status: "failed" }))).toBe(false);
  });
});

describe("taskProgress", () => {
  it("renders the bar and the count", () => {
    const p = taskProgress(build({ taskCounts: { total: 7, done: 3 } }));
    expect(p.percent).toBe(43);
    expect(p.label).toBe("3 of 7 done");
  });

  it("makes no claim when counts are absent", () => {
    // The distinction that matters: "the console does not know" must not render
    // as "this version has no work".
    const p = taskProgress(build());
    expect(p.label).toBe("—");
    expect(p.percent).toBe(0);
    expect(p.tone).toBe("neutral");
  });

  it("says No tasks when the backend genuinely reports none", () => {
    expect(taskProgress(build({ taskCounts: { total: 0, done: 0 } })).label).toBe(
      "No tasks",
    );
  });

  it("clamps a bar that double-counted", () => {
    expect(
      taskProgress(build({ taskCounts: { total: 2, done: 5 } })).percent,
    ).toBe(100);
  });

  it("tones the bar by the version's own state", () => {
    expect(
      taskProgress(build({ status: "failed", taskCounts: { total: 5, done: 4 } })).tone,
    ).toBe("error");
    expect(
      taskProgress(build({ status: "in_progress", taskCounts: { total: 7, done: 3 } }))
        .tone,
    ).toBe("info");
    expect(taskProgress(build({ taskCounts: { total: 6, done: 6 } })).tone).toBe(
      "success",
    );
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
    expect(taskBreakdown({ total: 6, done: 6 })).toBe("6 done");
  });

  it("falls back to the total when every bucket is empty", () => {
    expect(taskBreakdown({ total: 4, done: 0 })).toBe("4 total");
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
  });

  it("is empty rather than negative when the clocks disagree", () => {
    expect(buildDuration("2026-08-14T16:20:00Z", "2026-08-14T16:19:00Z")).toBe("");
    expect(buildDuration(undefined)).toBe("");
    expect(buildDuration("not-a-date", "also-not")).toBe("");
  });
});

describe("secondsDuration", () => {
  it("renders a server-measured span in the same shape as buildDuration", () => {
    expect(secondsDuration(221)).toBe("3m 41s");
    expect(secondsDuration(4)).toBe("0m 04s");
    expect(secondsDuration(2472)).toBe("41m 12s");
    expect(secondsDuration(4872)).toBe("1h 21m");
  });

  it("is empty rather than wrong for an absent or nonsense span", () => {
    expect(secondsDuration(undefined)).toBe("");
    expect(secondsDuration(null)).toBe("");
    expect(secondsDuration(-5)).toBe("");
    expect(secondsDuration(Number.NaN)).toBe("");
  });

  it("says 0m 00s for a genuinely instant span, not nothing", () => {
    expect(secondsDuration(0)).toBe("0m 00s");
  });
});

describe("ledgerDuration / ledgerStarted", () => {
  it("shows no span or start for a queued version", () => {
    // startedAt is the ENQUEUE time on a queued row; rendering it would claim a
    // start that has not happened.
    const queued = build({ status: "queued" });
    expect(ledgerDuration(queued)).toBe("—");
    expect(ledgerStarted(queued)).toBeNull();
  });

  it("passes the start through for every other state", () => {
    expect(ledgerStarted(build())).toBe("2026-08-14T16:20:00Z");
  });
});

describe("commitLine", () => {
  it("abbreviates the sha and names the branch", () => {
    expect(
      commitLine(build({ commit: { sha: "a1c9f2e0d4b8", branch: "feat/approval-routing" } })),
    ).toBe("a1c9f2e · feat/approval-routing");
  });

  it("degrades to whichever half it has", () => {
    expect(commitLine(build({ commit: { sha: "a1c9f2e0d4b8" } }))).toBe("a1c9f2e");
    expect(commitLine(build({ commit: { sha: "", branch: "main" } }))).toBe("main");
    expect(commitLine(build())).toBe("");
  });
});
