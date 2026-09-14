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
import type { RunProgressCycle } from "../../builds/hooks/useRunProgress";
import { foldValidationProgress } from "./useValidationLive";

let seq = 0;

const item = (itemId: string, itemStatus: string, cycleId = "c1") =>
  ({
    v: 2,
    kind: "work_item",
    source: "criterion",
    itemId,
    itemStatus,
    agentId: "lead",
    cycleId,
    attempt: 1,
    seq: (seq += 1),
    ts: "2026-08-31T00:00:00Z",
  }) as RunProgressCycle["events"][number];

const other = (kind: string, cycleId = "c1") =>
  ({
    v: 2,
    kind,
    agentId: "lead",
    cycleId,
    attempt: 1,
    seq: (seq += 1),
    ts: "2026-08-31T00:00:00Z",
    summary: "npm test",
  }) as RunProgressCycle["events"][number];

/** An entry of the AGENT's own plan — the other half of the work_item kind. */
const planItem = (itemId: string, itemStatus: string, cycleId = "c1") =>
  ({
    v: 2,
    kind: "work_item",
    source: "plan",
    itemId,
    itemStatus,
    title: "Wire the fixture",
    agentId: "lead",
    cycleId,
    attempt: 1,
    seq: (seq += 1),
    ts: "2026-08-31T00:00:00Z",
  }) as RunProgressCycle["events"][number];

const cycle = (
  id: string,
  kind: string,
  events: RunProgressCycle["events"],
  endedAt?: string,
): RunProgressCycle =>
  ({
    cycle: { id, kind, attempts: 1, createdAt: "2026-08-31T00:00:00Z", ...(endedAt ? { endedAt } : {}) },
    events,
  }) as RunProgressCycle;

describe("foldValidationProgress", () => {
  it("folds many events about one criterion into one status", () => {
    // The whole reason work_item exists: a reader wants one row repainted,
    // not seven rows printed.
    const out = foldValidationProgress([
      cycle("c1", "validation", [
        item("AC-001-a", "planned"),
        item("AC-001-a", "exploring"),
        item("AC-001-a", "authoring"),
        item("AC-002-a", "planned"),
      ]),
    ]);
    expect(out).toEqual({ statuses: { "AC-001-a": "authoring", "AC-002-a": "planned" }, active: true });
  });

  it("ignores every other kind of line on the same feed", () => {
    const out = foldValidationProgress([
      cycle("c1", "validation", [other("tool_use"), item("AC-001-a", "running"), other("log")]),
    ]);
    expect(out).toEqual({ statuses: { "AC-001-a": "running" }, active: true });
  });

  it("reads only the NEWEST validation cycle", () => {
    // A run can validate more than once — an `unreported` re-dispatch, or the
    // loop's bounded re-check. The earlier attempt describes work that is over,
    // and its report was rejected; carrying its statuses forward would show a
    // criterion as passed on the strength of an attempt the platform refused.
    const out = foldValidationProgress([
      cycle("c1", "validation", [item("AC-001-a", "pass", "c1")], "2026-08-31T01:00:00Z"),
      cycle("c2", "validation", [item("AC-001-a", "authoring", "c2")]),
    ]);
    expect(out).toEqual({ statuses: { "AC-001-a": "authoring" }, active: true });
  });

  it("says nothing while the newest validation cycle is closed", () => {
    // Mid-repair the newest validation cycle is the PREVIOUS attempt's, already
    // ended. Folding it would override that attempt's own report with the
    // statuses that produced it — a page insisting a criterion is running while
    // a coding cycle is what is actually in flight.
    const out = foldValidationProgress([
      cycle("c1", "validation", [item("AC-001-a", "fail")], "2026-08-31T01:00:00Z"),
      cycle("c2", "coding", []),
    ]);
    expect(out).toEqual({ statuses: {}, active: false });
  });

  it("says nothing for a run that has not validated", () => {
    expect(foldValidationProgress([cycle("c1", "coding", [])])).toEqual({
      statuses: {},
      active: false,
    });
    expect(foldValidationProgress([])).toEqual({ statuses: {}, active: false });
  });

  it("reports an OPEN cycle that has touched nothing as active", () => {
    // The distinction the `active` flag exists for: no statuses because the run
    // is still setting up, versus no statuses because nothing is validating.
    // Both are empty; only the first has anything to narrate.
    expect(foldValidationProgress([cycle("c1", "validation", [])])).toEqual({
      statuses: {},
      active: true,
    });
  });

  it("skips an event missing either half of its identity", () => {
    // A producer this build does not understand: the row it would produce is
    // blank in one direction or the other, and a blank row is worse than no row.
    const out = foldValidationProgress([
      cycle("c1", "validation", [
        { ...item("AC-001-a", "running"), itemId: undefined },
        { ...item("AC-002-a", "running"), itemStatus: undefined },
        item("AC-003-a", "running"),
      ] as RunProgressCycle["events"]),
    ]);
    expect(out).toEqual({ statuses: { "AC-003-a": "running" }, active: true });
  });

  // `work_item` serves two populations and `source` is what splits them. A plan
  // entry's `completed` says an agent ticked an item off its own to-do list;
  // a criterion's `pass` says something was asserted and held. Folding the
  // former in would paint the agent's plan onto the acceptance criteria.
  it("ignores an agent's own plan items, which share the kind", () => {
    const out = foldValidationProgress([
      cycle("c1", "validation", [
        planItem("plan-1", "completed"),
        item("AC-001-a", "pass"),
      ]),
    ]);
    expect(out).toEqual({ statuses: { "AC-001-a": "pass" }, active: true });
  });
});
