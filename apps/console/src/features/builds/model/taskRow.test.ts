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
import { orderedTasks, runClaims, statusLine, taskNote, taskState, taskTally } from "./taskRow";

type TaskView = components["schemas"]["TaskView"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];

const task = (issueNumber: number, rest: Partial<TaskView> = {}): TaskView =>
  ({
    issueNumber,
    title: `Task ${issueNumber}`,
    issueUrl: `https://github.com/acme/expenses/issues/${issueNumber}`,
    derivedStatus: "pending",
    executorClass: "coding",
    hold: false,
    executions: {},
    attention: null,
    dependsOn: null,
    ...rest,
  }) as TaskView;

const comment = (body: string) => ({ id: body, author: "aep-bot", body, createdAt: "2026-10-04T10:00:00Z", url: "" });

const cycle = (rest: Partial<RunCycleView>): RunCycleView => ({ id: "c", kind: "coding", attempts: 1, createdAt: "2026-10-04T10:00:00Z", ...rest });
const run = (cycles: RunCycleView[]): MilestoneRunView => ({ id: "r", state: "running", cycles }) as unknown as MilestoneRunView;

describe("a task's status line", () => {
  it("is the newest comment's first line that says something", () => {
    const t = task(14, { comments: [comment("Starting."), comment("\n  Waiting for Xero sandbox credentials (attempt 3 of 5).\nMore detail")] });
    expect(statusLine(t)).toBe("Waiting for Xero sandbox credentials (attempt 3 of 5).");
  });

  it("is nothing when the issue has no comment", () => {
    expect(statusLine(task(14))).toBeNull();
    expect(statusLine(task(14, { comments: [comment("  \n ")] }))).toBeNull();
  });

  it("falls back to what a hold waits on, then the platform's reason, under the row", () => {
    expect(taskNote(task(14, { blockedBy: ["xero"] }))).toBe("Waiting on xero");
    expect(taskNote(task(14, { rationale: "Planned for F3." }))).toBe("Planned for F3.");
    expect(taskNote(task(14, { comments: [comment("Merged in PR #21.")], blockedBy: ["xero"] }))).toBe("Merged in PR #21.");
  });
});

describe("a task's state", () => {
  it("reads a merge from the run's cycle before GitHub closes the issue", () => {
    const claims = runClaims([run([cycle({ resolves: [11], prNumber: 21, mergeSha: "abc", endedAt: "2026-10-04T10:30:00Z" })])]);
    expect(taskState(task(11), claims)).toBe("merged");
    expect(taskState(task(12, { derivedStatus: "merged" }))).toBe("merged");
  });

  it("puts a hold before the work it interrupted", () => {
    const claims = runClaims([run([cycle({ resolves: [14] })])]);
    expect(taskState(task(14, { hold: true }), claims)).toBe("blocked");
  });

  it("says a pull request waits, and an open session works its claimed or unclaimed issues", () => {
    expect(taskState(task(15), runClaims([run([cycle({ resolves: [15], prNumber: 25 })])]))).toBe("pr_sent");
    expect(taskState(task(15), runClaims([run([cycle({ resolves: [15] })])]))).toBe("in_progress");
    expect(taskState(task(16), runClaims([run([cycle({})])]))).toBe("in_progress");
    expect(taskState(task(16))).toBe("pending");
  });

  it("lets a later run's claim on an issue outrank an earlier one", () => {
    const older = run([cycle({ resolves: [15], prNumber: 25, endedAt: "2026-10-04T10:30:00Z" })]);
    const newer = run([cycle({ resolves: [15], prNumber: 26, mergeSha: "def" })]);
    expect(taskState(task(15), runClaims([newer, older]))).toBe("merged");
  });

  it("counts what is done and what waits on a person", () => {
    const claims = runClaims([run([cycle({ resolves: [15], prNumber: 25 })])]);
    expect(taskTally([task(11, { derivedStatus: "merged" }), task(14, { hold: true }), task(15), task(16)], claims)).toEqual({
      total: 4,
      done: 1,
      attention: 2,
    });
  });

  it("lists tasks in the order the milestone planned them", () => {
    expect(orderedTasks([task(15), task(11), task(14)]).map((t) => t.issueNumber)).toEqual([11, 14, 15]);
  });
});
