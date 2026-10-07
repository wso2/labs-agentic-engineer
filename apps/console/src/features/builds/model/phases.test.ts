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
import type { RunProgressCycle, StampedRunEvent } from "../hooks/useRunProgress";
import { phaseStripLabel, runStatus, runSteps, type RunPhase } from "./phases";

let seq = 0;
const at = (cycleId: string, second: number, rest: Partial<StampedRunEvent>): StampedRunEvent =>
  ({
    v: 2,
    seq: seq++,
    ts: new Date(Date.UTC(2026, 8, 30, 10, 0, second)).toISOString(),
    agentId: "lead",
    cycleId,
    attempt: 1,
    kind: "tool_use",
    ...rest,
  }) as StampedRunEvent;

const read = (s: number, path: string) => at("c1", s, { tool: "Read", summary: path });
const item = (s: number, itemId: string, title: string, itemStatus: string, ownerAgentId?: string) =>
  at("c1", s, { kind: "work_item", source: "plan", itemId, title, itemStatus, ...(ownerAgentId ? { ownerAgentId } : {}) } as Partial<StampedRunEvent>);
const criterion = (s: number, itemId: string, itemStatus: string) =>
  at("c2", s, { kind: "work_item", source: "criterion", itemId, itemStatus } as Partial<StampedRunEvent>);

const cycle = (id: string, kind: "coding" | "validation", events: StampedRunEvent[], ended = false): RunProgressCycle => ({
  cycle: { id, kind, attempts: 1, createdAt: "2026-09-30T10:00:00Z", ...(ended ? { endedAt: "2026-09-30T10:05:00Z" } : {}) },
  events,
});

const planned = [
  read(1, "specs/requirements/prd.md"),
  item(2, "foundation", "Foundation · API", "pending"),
  item(2, "F1-api", "F1 Submit expenses · API", "pending"),
];

describe("a run's steps, from its progress stream", () => {
  it("starts with Plan and Validate alone: the agent's own plan is not written yet", () => {
    const steps = runSteps({ state: "planning" }, []);
    expect(steps.phases.map((p) => [p.label, p.state])).toEqual([
      ["Plan", "live"],
      ["Validate", "queued"],
    ]);
    expect(runStatus("planning", steps, null)).toEqual({ text: "Running · 1 of 2", tone: "primary" });
  });

  it("lists the agent's plan entries between them, in the order it named them, while it still plans", () => {
    const steps = runSteps({ state: "running" }, [cycle("c1", "coding", planned)]);
    expect(steps.phases.map((p) => [p.label, p.state])).toEqual([
      ["Plan", "live"],
      ["Foundation · API", "queued"],
      ["F1 Submit expenses · API", "queued"],
      ["Validate", "queued"],
    ]);
    expect(steps.phases[0]?.log).toEqual(["$ Read specs/requirements/prd.md"]);
  });

  it("runs an entry from in progress to completed, its log its owner's events in that window", () => {
    const events = [
      ...planned,
      item(3, "foundation", "Foundation · API", "in_progress"),
      at("c1", 4, { tool: "Write", summary: "api/src/app.ts" }),
      at("c1", 5, { agentId: "helper", tool: "Write", summary: "web/src/other.tsx" }),
      item(9, "foundation", "Foundation · API", "completed"),
      item(9, "F1-api", "F1 Submit expenses · API", "in_progress", "helper"),
      at("c1", 10, { agentId: "helper", tool: "Write", summary: "api/src/claims/routes.ts" }),
    ];
    const steps = runSteps({ state: "running" }, [cycle("c1", "coding", events)]);
    const [plan, foundation, f1] = steps.phases;
    expect(plan).toMatchObject({ state: "done", log: ["$ Read specs/requirements/prd.md"] });
    expect(foundation).toMatchObject({ state: "done", log: ["$ Write api/src/app.ts"], durationMs: 6_000 });
    expect(f1).toMatchObject({ state: "live", log: ["$ Write api/src/claims/routes.ts"], durationMs: null });
    expect(steps.current).toBe(2);
    expect(steps.fraction).toBeCloseTo(2 / 4);
    expect(runStatus("running", steps, null).text).toBe("Running · 3 of 4");
  });

  it("counts validation by the criteria judged so far, and is done when its cycle ends", () => {
    const coding = cycle("c1", "coding", [...planned.slice(0, 2), item(3, "foundation", "Foundation · API", "completed")], true);
    const judging = [criterion(20, "AC-1", "planned"), criterion(20, "AC-2", "planned"), criterion(21, "AC-1", "pass")];
    const live = runSteps({ state: "running" }, [coding, cycle("c2", "validation", judging)]);
    expect(live.phases.at(-1)).toMatchObject({ key: "validate", state: "live", count: { done: 1, total: 2 } });
    expect(runStatus("running", live, null).text).toBe("Validating · 1 of 2");

    const over = runSteps({ state: "succeeded" }, [coding, cycle("c2", "validation", [...judging, criterion(22, "AC-2", "fail")], true)]);
    expect(over.phases.every((p) => p.state === "done")).toBe(true);
    expect(over.current).toBeNull();
    expect(over.fraction).toBe(1);
  });

  it("marks the step it was on as failed when the run fails", () => {
    const steps = runSteps({ state: "failed" }, [cycle("c1", "coding", [...planned, item(3, "foundation", "Foundation · API", "in_progress")])]);
    expect(steps.phases[1]).toMatchObject({ label: "Foundation · API", state: "failed" });
    expect(runStatus("failed", steps, null)).toEqual({ text: "Failed", tone: "error" });
  });
});

describe("the run's result in one line", () => {
  const done = runSteps({ state: "succeeded" }, []);

  it("says how many passed, and which story fails", () => {
    const failing = [{ story: "F2.4", name: "A deputy approves while the manager is on leave" }];
    expect(runStatus("succeeded", done, { passed: 10, total: 11, failing })).toEqual({
      text: "Built · 10/11 · F2.4 failing",
      tone: "warning",
    });
    expect(runStatus("succeeded", done, { passed: 11, total: 11, failing: [] })).toEqual({
      text: "Built · 11/11 passing",
      tone: "success",
    });
    expect(runStatus("succeeded", done, null)).toEqual({ text: "Built", tone: null });
  });
});

describe("the phase strip", () => {
  const phase = (rest: Partial<RunPhase>): RunPhase => ({ key: "p", label: "Plan", state: "queued", count: null, log: [], durationMs: null, ...rest });

  it("says where each phase is in words, never by its colour alone", () => {
    expect(phaseStripLabel(phase({ state: "done", durationMs: 125_000 }))).toBe("Plan: done in 2m5s");
    expect(phaseStripLabel(phase({ state: "done" }))).toBe("Plan: done");
    expect(phaseStripLabel(phase({ label: "Approvals · Web app", state: "live" }))).toBe("Approvals · Web app: running");
    expect(phaseStripLabel(phase({ label: "Validate", state: "live", count: { done: 3, total: 9 } }))).toBe("Validate: 3 of 9");
    expect(phaseStripLabel(phase({ state: "failed" }))).toBe("Plan: failed");
    expect(phaseStripLabel(phase({}))).toBe("Plan: queued");
  });
});
