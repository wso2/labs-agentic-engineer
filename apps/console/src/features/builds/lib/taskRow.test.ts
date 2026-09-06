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
import {
  anyTaskRunning,
  runClaims,
  latestExecution,
  taskElapsedFrom,
  taskRowChip,
  taskRowNote,
  taskRowState,
  taskSettledAt,
  taskTally,
} from "./taskRow";

type TaskView = components["schemas"]["TaskView"];
type ExecutionView = components["schemas"]["ExecutionView"];

const exec = (over: Partial<ExecutionView> = {}): ExecutionView => ({
  id: "e1",
  kind: "coding",
  status: "running",
  createdAt: "2026-07-12T04:46:00Z",
  ...over,
});

const task = (over: Partial<TaskView> = {}): TaskView => ({
  issueNumber: 121,
  title: "Route claims over 5,000 to finance",
  issueUrl: "https://github.com/acme-dev/demo-shop/issues/121",
  executorClass: "coding",
  dependsOn: [],
  lineage: {},
  derivedStatus: "pending",
  hold: false,
  attention: [],
  executions: {},
  ...over,
});

describe("taskRowState", () => {
  it("closes on merged, whatever else is going on", () => {
    expect(
      taskRowState(
        task({ derivedStatus: "merged", hold: true, executions: { a: exec() } }),
      ),
    ).toBe("merged");
  });

  it("puts a hold ahead of a running execution", () => {
    // Both are true at once when the agent hits a dependency mid-flight, and
    // "blocked" is the one the reader has to act on.
    expect(
      taskRowState(task({ hold: true, executions: { a: exec() } })),
    ).toBe("blocked");
  });

  it("treats a named blocker as a hold even without the flag", () => {
    expect(taskRowState(task({ blockedBy: ["Payroll SFTP"] }))).toBe("blocked");
  });

  it("is in progress while an execution is running and unfinished", () => {
    expect(taskRowState(task({ executions: { a: exec() } }))).toBe("in_progress");
  });

  it("accepts the platform's other running spellings", () => {
    for (const status of ["running", "in_progress", "Started", "ACTIVE"]) {
      expect(taskRowState(task({ executions: { a: exec({ status }) } }))).toBe(
        "in_progress",
      );
    }
  });

  it("does not invent review from a finished execution", () => {
    // An earlier version read "the execution ended and the issue is still open"
    // as awaiting review. That heuristic existed because agent progress was
    // being read off executions at all — which it cannot be, since agent work
    // has none. Review is now the run's own signal: a claimed task whose build
    // session has opened its pull request.
    expect(
      taskRowState(
        task({ executions: { a: exec({ endedAt: "2026-07-12T05:02:00Z" }) } }),
      ),
    ).toBe("pending");
  });

  it("is pending when nothing has run", () => {
    expect(taskRowState(task())).toBe("pending");
  });

  it("does not call a task in progress when its execution is not running", () => {
    expect(
      taskRowState(task({ executions: { a: exec({ status: "queued" }) } })),
    ).toBe("pending");
  });
});

describe("runClaims — agent progress comes from the RUN, not the task", () => {
  const cycle = (over: Partial<components["schemas"]["RunCycleView"]> = {}) => ({
    id: "c1",
    kind: "coding",
    attempts: 1,
    createdAt: "2026-07-12T04:46:00Z",
    ...over,
  }) as components["schemas"]["RunCycleView"];

  const run = (
    cycles: components["schemas"]["RunCycleView"][],
    over: Partial<components["schemas"]["MilestoneRunView"]> = {},
  ) => ({
    id: "run-1",
    milestoneNumber: 1,
    milestoneTitle: "Milestone 1",
    kind: "dev",
    state: "running",
    origin: "spec-build",
    createdAt: "2026-07-12T04:45:00Z",
    cycles,
    ...over,
  }) as components["schemas"]["MilestoneRunView"];

  it("records what the open session claims", () => {
    const c = runClaims([run([cycle({ resolves: [121, 122] })])]);
    expect([...c.byIssue.keys()]).toEqual([121, 122]);
    expect(c.presumeOpenWork).toBe(false);
  });

  it("KEEPS a closed session's claims — its answer is final, not gone", () => {
    // This is the regression the states were lost to: a cycle ends the moment
    // its pull request settles, so dropping closed cycles threw the answer away
    // at exactly the moment it became true. The row fell back to `Pending`.
    const c = runClaims([
      run([cycle({ resolves: [121], prNumber: 7, endedAt: "2026-07-12T05:02:00Z" })]),
    ]);
    expect(c.byIssue.get(121)?.prNumber).toBe(7);
    expect(c.byIssue.get(121)?.open).toBe(false);
  });

  it("presumes open work only before the session records anything", () => {
    expect(runClaims([run([cycle()])]).presumeOpenWork).toBe(true);
    expect(runClaims([run([cycle({ resolves: [121] })])]).presumeOpenWork).toBe(false);
    expect(runClaims([run([])]).presumeOpenWork).toBe(false);
    expect(runClaims([]).presumeOpenWork).toBe(false);
    expect(runClaims(undefined).presumeOpenWork).toBe(false);
  });

  it("lets a later run's claim outrank an earlier one on the same issue", () => {
    // Runs arrive newest-first; a `task` run reworking an issue a `dev` run
    // already merged is the newer word on it.
    const claims = runClaims([
      run([cycle({ id: "new", resolves: [121] })], { id: "newer", kind: "task" }),
      run([cycle({ id: "old", resolves: [121], prNumber: 7, mergeSha: "abc", endedAt: "x" })], {
        id: "older",
      }),
    ]);
    expect(claims.byIssue.get(121)?.merged).toBe(false);
    expect(claims.byIssue.get(121)?.open).toBe(true);
  });

  it("ignores a validation cycle — it claims no agent work", () => {
    const claims = runClaims([run([cycle({ kind: "validation", resolves: [121] })])]);
    expect(claims.byIssue.size).toBe(0);
  });
});

describe("taskRowState — the three things one pull request says about its issues", () => {
  const cycle = (over: Partial<components["schemas"]["RunCycleView"]> = {}) => ({
    id: "c1",
    kind: "coding",
    attempts: 1,
    createdAt: "2026-07-12T04:46:00Z",
    ...over,
  }) as components["schemas"]["RunCycleView"];

  const claimsFor = (over: Partial<components["schemas"]["RunCycleView"]>) =>
    runClaims([
      {
        id: "run-1",
        milestoneNumber: 1,
        milestoneTitle: "Milestone 1",
        kind: "dev",
        state: "running",
        origin: "spec-build",
        createdAt: "2026-07-12T04:45:00Z",
        cycles: [cycle(over)],
      } as components["schemas"]["MilestoneRunView"],
    ]);

  const at = (issueNumber: number, claims: ReturnType<typeof runClaims>) =>
    taskRowState(task({ issueNumber }), claims);

  it("in progress — the session is open and no pull request exists yet", () => {
    expect(at(121, claimsFor({ resolves: [121] }))).toBe("in_progress");
    // Without the run there is nothing to know, and the row says so.
    expect(taskRowState(task({ issueNumber: 121 }))).toBe("pending");
  });

  it("PR sent — the pull request exists and nothing has merged it", () => {
    expect(at(121, claimsFor({ resolves: [121], prNumber: 7 }))).toBe("pr_sent");
  });

  it("PR sent survives the session ending — an unmerged pull request is the news", () => {
    expect(
      at(121, claimsFor({ resolves: [121], prNumber: 7, endedAt: "2026-07-12T05:02:00Z" })),
    ).toBe("pr_sent");
  });

  it("merged — the SHA is recorded, even before GitHub closes the issue", () => {
    // The two can disagree for a moment: the merge lands, the close event has
    // not. Reading only `derivedStatus` showed `PR sent` on merged work.
    const claims = claimsFor({
      resolves: [121],
      prNumber: 7,
      mergeSha: "abc1234",
      endedAt: "2026-07-12T05:02:00Z",
    });
    expect(at(121, claims)).toBe("merged");
  });

  it("says the same thing about EVERY issue the one pull request claims", () => {
    // One cycle dispatches one pull request that resolves a set of issues, so
    // its state lands on all of them — that is why a row can say "PR sent"
    // without the task itself carrying a pull request.
    const claims = claimsFor({ resolves: [121, 122, 123], prNumber: 7 });
    expect([121, 122, 123].map((n) => at(n, claims))).toEqual([
      "pr_sent",
      "pr_sent",
      "pr_sent",
    ]);
  });

  it("presumes the open issues are being worked before any claim", () => {
    expect(at(999, claimsFor({}))).toBe("in_progress");
  });

  it("never overrides closed or blocked", () => {
    const claims = claimsFor({ resolves: [121] });
    expect(taskRowState(task({ issueNumber: 121, derivedStatus: "merged" }), claims)).toBe(
      "merged",
    );
    expect(taskRowState(task({ issueNumber: 121, hold: true }), claims)).toBe("blocked");
  });

  it("counts elapsed time from the session that claimed it", () => {
    expect(
      taskElapsedFrom(task({ issueNumber: 121 }), claimsFor({ resolves: [121] })),
    ).toBe("2026-07-12T04:46:00Z");
  });

  it("still prefers a GATE's own execution row, which agent work lacks", () => {
    const gate = task({ issueNumber: 8, executorClass: "provision", executions: { p: exec() } });
    expect(taskRowState(gate)).toBe("in_progress");
    expect(taskElapsedFrom(gate)).toBe("2026-07-12T04:46:00Z");
  });
});

describe("latestExecution", () => {
  it("picks the newest by creation, not by map order", () => {
    const t = task({
      executions: {
        older: exec({ id: "older", createdAt: "2026-07-12T04:00:00Z" }),
        newer: exec({ id: "newer", createdAt: "2026-07-12T06:00:00Z" }),
      },
    });
    expect(latestExecution(t)?.id).toBe("newer");
  });

  it("is undefined when there are none", () => {
    expect(latestExecution(task())).toBeUndefined();
  });
});

describe("taskRowChip", () => {
  it("marks only in-progress as live", () => {
    expect(taskRowChip("in_progress").live).toBe(true);
    expect(taskRowChip("blocked").live).toBe(false);
    expect(taskRowChip("merged").label).toBe("Merged");
    expect(taskRowChip("merged").tone).toBe("success");
    expect(taskRowChip("pr_sent").label).toBe("PR sent");
    expect(taskRowChip("pr_sent").tone).toBe("warning");
    expect(taskRowChip("pending").tone).toBe("neutral");
  });
});

const comment = (body: string, id = "c1") => ({
  id,
  author: "aep-bot",
  body,
  createdAt: "2026-07-12T04:50:00Z",
  url: "https://github.com/acme-dev/demo-shop/issues/121#issuecomment-1",
});

describe("taskRowNote", () => {
  it("prefers the agent's own words", () => {
    expect(
      taskRowNote(task({ comments: [comment("Writing tests for the routing rule")] })),
    ).toBe("Writing tests for the routing rule");
  });

  it("takes the NEWEST comment — the list arrives oldest first", () => {
    expect(
      taskRowNote(
        task({ comments: [comment("Starting", "c1"), comment("Nearly done", "c2")] }),
      ),
    ).toBe("Nearly done");
  });

  it("flattens a markdown body to its first real line", () => {
    // A comment body is markdown over an unbounded textarea; the row is one
    // dense line. Leading blank lines must not render as an empty note.
    expect(
      taskRowNote(task({ comments: [comment("\n\n  Rebased onto main\n\nthen re-ran the suite")] })),
    ).toBe("Rebased onto main");
  });

  it("falls through when a comment body is entirely whitespace", () => {
    expect(
      taskRowNote(task({ comments: [comment("   \n  ")], rationale: "Minted from the spec" })),
    ).toBe("Minted from the spec");
  });

  it("names the blocker when the agent has said nothing", () => {
    expect(taskRowNote(task({ blockedBy: ["Payroll SFTP"] }))).toBe(
      "Waiting on Payroll SFTP",
    );
  });

  it("falls back to the platform's rationale", () => {
    expect(taskRowNote(task({ rationale: "Minted from the spec" }))).toBe(
      "Minted from the spec",
    );
  });

  it("is null rather than a placeholder when there is nothing to say", () => {
    // Eleven rows each reading "No updates yet" is noise, not information.
    expect(taskRowNote(task())).toBeNull();
  });
});

describe("taskElapsedFrom / taskSettledAt", () => {
  it("counts from the running execution's start", () => {
    expect(
      taskElapsedFrom(
        task({ executions: { a: exec({ startedAt: "2026-07-12T04:46:00Z" }) } }),
      ),
    ).toBe("2026-07-12T04:46:00Z");
  });

  it("falls back to creation when the start was never stamped", () => {
    expect(
      taskElapsedFrom(task({ executions: { a: exec({ createdAt: "2026-07-12T04:40:00Z" }) } })),
    ).toBe("2026-07-12T04:40:00Z");
  });

  it("gives no elapsed time for a queued execution", () => {
    // A queued execution has no `endedAt` either, so testing only for that made
    // a row taskRowState calls `pending` render a counting-up elapsed time.
    const queued = task({ executions: { a: exec({ status: "queued" }) } });
    expect(taskRowState(queued)).toBe("pending");
    expect(taskElapsedFrom(queued)).toBeNull();
  });

  it("gives no elapsed time for a finished task, and a settled stamp instead", () => {
    const finished = task({
      executions: { a: exec({ endedAt: "2026-07-12T05:02:00Z" }) },
    });
    expect(taskElapsedFrom(finished)).toBeNull();
    expect(taskSettledAt(finished)).toBe("2026-07-12T05:02:00Z");
    expect(taskSettledAt(task())).toBeNull();
  });
});

describe("taskTally", () => {
  it("counts merged, and folds blocked and PR-sent into one attention number", () => {
    // PR-sent comes from the run: a claimed task whose session opened a PR.
    const claims = runClaims([
      {
        id: "run-1",
        milestoneNumber: 1,
        milestoneTitle: "Milestone 1",
        kind: "dev",
        state: "running",
        origin: "spec-build",
        createdAt: "2026-07-12T04:45:00Z",
        cycles: [
          {
            id: "c1",
            kind: "coding",
            attempts: 1,
            createdAt: "2026-07-12T04:46:00Z",
            resolves: [55],
            prNumber: 7,
          },
        ],
      } as components["schemas"]["MilestoneRunView"],
    ]);
    const tally = taskTally(
      [
        task({ issueNumber: 1, derivedStatus: "merged" }),
        task({ issueNumber: 2, derivedStatus: "merged" }),
        task({ issueNumber: 3, hold: true }),
        task({ issueNumber: 55 }), // claimed + PR open -> PR sent
        task({ issueNumber: 4, executorClass: "provision", executions: { a: exec() } }),
        task({ issueNumber: 5 }),
      ],
      claims,
    );
    expect(tally).toEqual({ total: 6, done: 2, attention: 2 });
  });

  it("is all zeroes for an empty build", () => {
    expect(taskTally([])).toEqual({ total: 0, done: 0, attention: 0 });
  });
});

describe("anyTaskRunning", () => {
  it("is true only while something is actually executing", () => {
    expect(anyTaskRunning([task(), task({ executions: { a: exec() } })])).toBe(true);
    expect(anyTaskRunning([task(), task({ derivedStatus: "merged" })])).toBe(false);
    expect(anyTaskRunning([])).toBe(false);
  });
});
