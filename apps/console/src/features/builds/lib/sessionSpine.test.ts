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
import { SESSION_STAGE_COUNT, sessionIssues, sessionStages } from "./sessionSpine";
import type { StageState } from "./stage";

type RunCycleView = components["schemas"]["RunCycleView"];
type TaskView = components["schemas"]["TaskView"];
type CycleBuild = components["schemas"]["CycleBuild"];

function cycle(over: Partial<RunCycleView> = {}): RunCycleView {
  return {
    id: "c1",
    kind: "coding",
    attempts: 1,
    createdAt: "2026-07-10T09:00:00Z",
    ...over,
  } as RunCycleView;
}

function task(issueNumber: number, over: Partial<TaskView> = {}): TaskView {
  return {
    issueNumber,
    title: `issue ${issueNumber}`,
    issueUrl: `https://github.com/acme/repo/issues/${issueNumber}`,
    executorClass: "coding",
    dependsOn: [],
    lineage: {},
    derivedStatus: "pending",
    hold: false,
    attention: [],
    executions: {},
    ...over,
  } as TaskView;
}

function build(over: Partial<CycleBuild> = {}): CycleBuild {
  return {
    component: "workout-api",
    buildName: "proj-workout-api-4a91c2f8ab31-1",
    status: "Succeeded",
    completed: true,
    attempt: 1,
    ...over,
  };
}

/** The state of one stage by id — the shape every assertion below reads. */
function stateOf(
  facts: Parameters<typeof sessionStages>[0],
): Record<string, StageState> {
  const out: Record<string, StageState> = {};
  for (const stage of sessionStages(facts)) out[stage.id] = stage.state;
  return out;
}

function noteOf(facts: Parameters<typeof sessionStages>[0], id: string): string {
  return sessionStages(facts).find((stage) => stage.id === id)?.note ?? "";
}

describe("sessionStages", () => {
  // The property the whole redesign rests on: every stage exists from the moment
  // the session does, so there is no window in which a transition is drawn
  // nowhere.
  it("returns all five stages whatever the session has reached", () => {
    const ids = sessionStages({ cycle: cycle(), work: [], builds: undefined }).map((s) => s.id);
    expect(ids).toEqual(["agent", "pr", "merge", "builds", "deploy"]);
  });

  // The rail numbers every stage straight through BEFORE any session's
  // cluster-derived facts arrive, which it can only do if the count is fixed.
  it("contributes exactly SESSION_STAGE_COUNT stages, whatever the session did", () => {
    for (const facts of [
      { cycle: cycle(), work: [], builds: undefined },
      { cycle: cycle({ prNumber: 4, mergeSha: "0f7a478612ab" }), work: [], builds: [build()] },
      { cycle: cycle({ endedAt: "2026-07-10T09:40:00Z" }), work: [], builds: undefined },
    ]) {
      expect(sessionStages(facts)).toHaveLength(SESSION_STAGE_COUNT);
    }
  });

  it("names an actor for every stage — half of a session is not the agent's work", () => {
    const actors = sessionStages({ cycle: cycle(), work: [], builds: undefined }).map(
      (s) => s.actor,
    );
    expect(actors).toEqual(["runner", "agent", "platform", "platform", "cluster"]);
  });

  it("walks a healthy session from dispatch to deployed", () => {
    // Dispatched, nothing learned yet.
    expect(stateOf({ cycle: cycle(), work: [], builds: undefined })).toMatchObject({
      agent: "active",
      pr: "waiting",
      merge: "waiting",
      builds: "waiting",
      deploy: "waiting",
    });
    // A pull request IS the agent's exit — the Job stops the moment it opens one.
    expect(stateOf({ cycle: cycle({ prNumber: 4 }), work: [], builds: undefined })).toMatchObject({
      agent: "done",
      pr: "done",
      merge: "active",
    });
    // Merged, builds in flight.
    expect(
      stateOf({
        cycle: cycle({ prNumber: 4, mergeSha: "0f7a478612ab" }),
        work: [],
        builds: [build({ completed: false, status: "Running" })],
      }),
    ).toMatchObject({ merge: "done", builds: "active", deploy: "waiting" });
    // Green, and therefore deployed: components carry auto-deploy.
    expect(
      stateOf({
        cycle: cycle({ prNumber: 4, mergeSha: "0f7a478612ab" }),
        work: [],
        builds: [build()],
      }),
    ).toMatchObject({ builds: "done", deploy: "done" });
  });

  // The number alone is a dead end: a reader who wants the pull request has to
  // go and find it. The link is the platform's OWN recorded URL — never one this
  // file assembles from a repo URL and a number.
  it("links the pull request to the page the platform recorded", () => {
    const url = "https://github.com/acme/repo/pull/4";
    const pr = sessionStages({
      cycle: cycle({ prNumber: 4, prUrl: url }),
      work: [],
      builds: undefined,
    }).find((s) => s.id === "pr");
    expect(pr).toMatchObject({ fact: "#4", factHref: url });
  });

  // A draft is the stage a reader most wants to open — it is the one waiting on
  // a person — so the link cannot be reserved for the settled case.
  it("links a draft's pull request too", () => {
    const url = "https://github.com/acme/repo/pull/4";
    const pr = sessionStages({
      cycle: cycle({ prNumber: 4, prUrl: url, prDraft: true }),
      work: [],
      builds: undefined,
    }).find((s) => s.id === "pr");
    expect(pr).toMatchObject({ state: "attention", fact: "#4", factHref: url });
  });

  // Cycles recorded before the platform kept the URL still have their number,
  // and a fact with nowhere to go must stay a fact rather than become a link to
  // a guess.
  it("shows the number with no link when no URL was recorded", () => {
    const pr = sessionStages({
      cycle: cycle({ prNumber: 4 }),
      work: [],
      builds: undefined,
    }).find((s) => s.id === "pr");
    expect(pr?.fact).toBe("#4");
    expect(pr?.factHref).toBeUndefined();
  });

  it("shows the merge SHA short, as the platform logs it", () => {
    const merge = sessionStages({
      cycle: cycle({ prNumber: 4, mergeSha: "0f7a478612ab34" }),
      work: [],
      builds: undefined,
    }).find((s) => s.id === "merge");
    expect(merge?.fact).toBe("0f7a4786");
  });

  // ---- the silent stalls ---------------------------------------------------

  it("reads a draft as needing attention, and holds the merge behind it", () => {
    const facts = { cycle: cycle({ prNumber: 4, prDraft: true }), work: [], builds: undefined };
    expect(stateOf(facts)).toMatchObject({ pr: "attention", merge: "waiting" });
    expect(noteOf(facts, "pr")).toMatch(/draft/);
    // Not "deciding": the policy never runs on a draft, so saying it was would
    // describe work nobody is doing.
    expect(noteOf(facts, "merge")).toMatch(/as soon as a pull request is ready/);
  });

  it("carries the policy's own reason for a declined merge", () => {
    const facts = {
      cycle: cycle({ prNumber: 4, mergeVerdict: "declined", mergeReason: "pull request resolves no issue" }),
      work: [],
      builds: undefined,
    };
    expect(stateOf(facts).merge).toBe("attention");
    expect(noteOf(facts, "merge")).toMatch(/pull request resolves no issue/);
  });

  it("reads a refused merge as a failure with a way forward", () => {
    const facts = {
      cycle: cycle({ prNumber: 4, mergeVerdict: "refused", mergeReason: "Pull Request is not mergeable" }),
      work: [],
      builds: undefined,
    };
    expect(stateOf(facts).merge).toBe("failed");
    expect(noteOf(facts, "merge")).toMatch(/next build session rebases/);
  });

  // A merge SHA is the merge. A stale verdict from an earlier decline on the
  // same pull request must not outrank the fact that it later landed.
  it("prefers the merge SHA over a stale verdict", () => {
    expect(
      stateOf({
        cycle: cycle({ prNumber: 4, mergeSha: "0f7a478612ab", mergeVerdict: "declined" }),
        work: [],
        builds: undefined,
      }).merge,
    ).toBe("done");
  });

  it("blames the agent, not the pull request, when it died without opening one", () => {
    const facts = {
      cycle: cycle({ attempts: 2, endedAt: "2026-07-10T09:40:00Z" }),
      work: [],
      builds: undefined,
    };
    expect(stateOf(facts)).toMatchObject({ agent: "failed", pr: "waiting" });
    expect(noteOf(facts, "agent")).toMatch(/re-dispatch budget is spent/);
    expect(noteOf(facts, "pr")).toMatch(/landed nothing/);
  });

  // F1: the stage must not claim the agent is writing code while the cluster
  // has not started it, nor that it "ended" when it never began.
  it("says the agent is waiting to start while its pod is stuck", () => {
    const facts = {
      cycle: cycle({
        startupWait: { reason: "Unschedulable", since: "2026-07-10T09:05:30Z", failsAt: "2026-07-10T09:15:00Z" },
      }),
      work: [],
      builds: undefined,
    };
    expect(stateOf(facts)).toMatchObject({ agent: "waiting" });
    expect(noteOf(facts, "agent")).toBe("Waiting for room in the cluster to start the agent.");
  });

  it("says the agent could not start once the watcher closed it", () => {
    const facts = {
      cycle: cycle({
        endedAt: "2026-07-10T09:15:00Z",
        agentReason: "startup_failed:Unschedulable: 0/1 nodes are available",
      }),
      work: [],
      builds: undefined,
    };
    expect(stateOf(facts)).toMatchObject({ agent: "failed" });
    expect(noteOf(facts, "agent")).toBe("Could not start. The cluster had no free CPU or memory for it.");
  });

  // The cluster writes WorkflowSucceeded / WorkflowFailed, not the bare words.
  // Recognising only the bare ones made a real red build read as green here.
  it("reads the cluster's own reason strings, not just the bare ones", () => {
    const facts = {
      cycle: cycle({ prNumber: 4, mergeSha: "0f7a478612ab" }),
      work: [],
      builds: [
        build({ status: "WorkflowSucceeded" }),
        build({ component: "webapp", buildName: "b2", status: "WorkflowSucceeded" }),
      ],
    };
    expect(stateOf(facts)).toMatchObject({ builds: "done", deploy: "done" });

    const red = { ...facts, builds: [build({ status: "WorkflowFailed" })] };
    expect(stateOf(red)).toMatchObject({ builds: "failed", deploy: "failed" });
    expect(noteOf(red, "builds")).toMatch(/workout-api did not go green/);
  });

  // "Not red" is not "green": the Reason set is open, and a terminal Reason the
  // console cannot classify must not be counted as a success — the deployment
  // stage below would otherwise promise a rollout on the strength of it.
  it("refuses to call an unrecognised terminal reason green", () => {
    const facts = {
      cycle: cycle({ prNumber: 4, mergeSha: "0f7a478612ab" }),
      work: [],
      builds: [build({ status: "WorkflowTimedOut" })],
    };
    expect(stateOf(facts)).toMatchObject({ builds: "attention", deploy: "attention" });
    expect(noteOf(facts, "builds")).toMatch(/finished as WorkflowTimedOut/);
    expect(noteOf(facts, "deploy")).toMatch(/could not read/);
  });

  it("names the red components and refuses to claim a deployment", () => {
    const facts = {
      cycle: cycle({ prNumber: 4, mergeSha: "0f7a478612ab" }),
      work: [],
      builds: [build({ status: "Failed" }), build({ component: "webapp", buildName: "b2" })],
    };
    expect(stateOf(facts)).toMatchObject({ builds: "failed", deploy: "failed" });
    expect(noteOf(facts, "builds")).toMatch(/workout-api did not go green/);
    expect(noteOf(facts, "builds")).toMatch(/fix issue/);
  });

  // Every session's Builds stage is on the rail now, so every merged session is
  // read: an unanswered read and an empty fan-out are both "still coming".
  it("reads a merge with no builds in hand as a fan-out still landing", () => {
    const merged = cycle({ prNumber: 4, mergeSha: "0f7a478612ab" });
    expect(stateOf({ cycle: merged, work: [], builds: undefined }).builds).toBe("active");
    expect(stateOf({ cycle: merged, work: [], builds: [] }).builds).toBe("active");
  });

  // A second attempt means the FIRST agent died. It rides the coding stage
  // because that is the stage it is a fact about.
  it("carries a re-dispatch onto the coding stage, and nothing onto a first attempt", () => {
    const factOf = (attempts: number) =>
      sessionStages({ cycle: cycle({ attempts }), work: [], builds: undefined }).find(
        (stage) => stage.id === "agent",
      )?.fact;
    expect(factOf(2)).toBe("attempt 2/2");
    expect(factOf(1)).toBeUndefined();
  });
});

describe("sessionIssues", () => {
  // The reason resolves[] is persisted at all: after the merge the issues are
  // closed, and nothing else in the system says which session closed them.
  it("is exact from the recorded matched set, closed issues included", () => {
    const result = sessionIssues(
      cycle({ prNumber: 4, mergeSha: "0f7a478612ab", resolves: [2, 3], endedAt: "2026-07-10T09:40:00Z" }),
      [
        task(2, { derivedStatus: "merged" }),
        task(3, { derivedStatus: "merged" }),
        task(9, { derivedStatus: "merged" }),
      ],
    );
    expect(result.exact).toBe(true);
    expect(result.issues.map((i) => i.issueNumber)).toEqual([2, 3]);
    expect(result.caption).toMatch(/pull request #4/);
  });

  it("falls back to the milestone's open work before a pull request exists, and says so", () => {
    const result = sessionIssues(cycle(), [
      task(2),
      task(3, { derivedStatus: "merged" }),
    ]);
    expect(result.exact).toBe(false);
    expect(result.issues.map((i) => i.issueNumber)).toEqual([2]);
    expect(result.caption).toMatch(/recorded once it opens a pull request/);
  });

  // Guessing from what is open NOW would attribute a later session's work to an
  // earlier one — the exact error the fallback exists to avoid.
  it("claims nothing for a settled session that recorded no matched set", () => {
    const result = sessionIssues(cycle({ endedAt: "2026-07-10T09:40:00Z" }), [task(2)]);
    expect(result.issues).toEqual([]);
    expect(result.caption).toBe("");
  });
});
