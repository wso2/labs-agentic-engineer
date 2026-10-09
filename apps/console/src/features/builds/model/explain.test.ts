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
import { buildExplanation, detailsText, failureExplanation } from "./explain";
import { runClaims } from "./taskRow";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type TaskView = components["schemas"]["TaskView"];

const run = (rest: Partial<MilestoneRunView>): MilestoneRunView =>
  ({ id: "run-1", state: "running", cycles: [], ...rest }) as unknown as MilestoneRunView;

const development = { name: "development", label: "Development" };
const noClaims = runClaims([]);

describe("why a build failed", () => {
  it("says nothing for a run that is moving cleanly or that a person cancelled", () => {
    expect(failureExplanation(run({}))).toBeUndefined();
    expect(failureExplanation(run({ state: "cancelled", terminalReason: "plan-failed" }))).toBeUndefined();
  });

  it("puts a recorded fault into words, amber while the platform retries it", () => {
    const failure = {
      code: "dependency-provision-failed" as const,
      phase: "planning",
      dependency: "xero",
      permanent: false,
      attempts: 2,
      maxAttempts: 5,
      firstAt: "2026-10-04T10:00:00Z",
      lastAt: "2026-10-04T10:05:00Z",
    };
    expect(failureExplanation(run({ failure }))).toMatchObject({
      tone: "warning",
      title: "Provisioning `xero` failed, retrying (attempt 2 of 5)",
    });
    expect(failureExplanation(run({ state: "failed", failure }))).toMatchObject({
      tone: "error",
      title: "The platform could not provision `xero`",
    });
  });

  it("explains a run that ended on a reason alone, with the way out where one exists", () => {
    expect(failureExplanation(run({ state: "failed", terminalReason: "deploy-budget" }))).toMatchObject({
      tone: "error",
      next: { label: "Go to Deploy", to: "/projects/$projectName/deploy" },
    });
  });

  it("names whose usage limit stopped the agent", () => {
    const failure = {
      code: "model-provider-limit" as const,
      phase: "coding",
      host: "Anthropic",
      permanent: false,
      attempts: 1,
      maxAttempts: 0,
      firstAt: "2026-10-04T10:00:00Z",
      lastAt: "2026-10-04T10:00:00Z",
    };
    expect(failureExplanation(run({ state: "blocked", failure }))).toMatchObject({
      tone: "warning",
      title: "Anthropic's usage limit was reached",
    });
  });

  it("gives its details as text a bug report can carry", () => {
    const text = detailsText({ code: "plan-failed", permanent: undefined, attempts: undefined, detail: "boom", runId: "run-1", workflowId: undefined });
    expect(text).toBe("code: plan-failed\nrecorded: boom\nrun: run-1");
  });
});

describe("what the Build card says above its tasks", () => {
  it("sends a run parked for dependency values to the write target's Configure card", () => {
    const parked = run({ state: "waiting", waitingReason: "external-values", blockingDependencies: ["xero"] });
    expect(buildExplanation({ run: parked, tasks: [], claims: noClaims, environment: development })).toMatchObject({
      tone: "warning",
      title: "Waiting for configuration: xero",
      next: { label: "Configure Development", to: "/projects/$projectName/deploy/$env/configure", env: "development" },
    });
  });

  it("explains the first blocked task in its own words when the run is not at fault", () => {
    const blocked = {
      issueNumber: 14,
      title: "Payroll export · API",
      hold: true,
      derivedStatus: "pending",
      comments: [{ id: "1", author: "aep", body: "Waiting for Xero sandbox credentials", createdAt: "", url: "" }],
    } as unknown as TaskView;
    expect(buildExplanation({ run: run({}), tasks: [blocked], claims: noClaims, environment: development })).toMatchObject({
      title: "#14 Payroll export · API is blocked",
      body: "Waiting for Xero sandbox credentials.",
      next: { env: "development" },
    });
  });

  it("leads with a failure over a park or a blocked task", () => {
    const failed = run({ state: "failed", terminalReason: "plan-failed" });
    expect(buildExplanation({ run: failed, tasks: [], claims: noClaims, environment: development })?.tone).toBe("error");
  });

  it("says nothing for a build that is moving or done", () => {
    expect(buildExplanation({ run: run({}), tasks: [], claims: noClaims, environment: development })).toBeNull();
    expect(buildExplanation({ run: undefined, tasks: [], claims: noClaims, environment: null })).toBeNull();
  });
});

// F1: an agent the cluster never started. The platform closes the cycle with
// `startup_failed:<reason>[: <message>]` and the run with agent-start-failed.
describe("an agent that could not start", () => {
  const cycle = (kind: string, agentReason: string, over: Record<string, unknown> = {}) => ({
    id: "c1",
    kind,
    attempts: 1,
    createdAt: "2026-09-11T07:40:00Z",
    endedAt: "2026-09-11T07:50:00Z",
    agentReason,
    recording: "kept",
    ...over,
  });
  const startFailed = (...cycles: Record<string, unknown>[]) =>
    failureExplanation(run({ state: "failed", terminalReason: "agent-start-failed", cycles: cycles as never }));

  it("says the coding agent never started, why, that nothing ran, and when Retry can help", () => {
    const e = startFailed(cycle("coding", "startup_failed:Unschedulable: 0/1 nodes are available: 1 Insufficient memory."));
    expect(e?.tone).toBe("error");
    expect(e?.title).toBe("The coding agent could not start");
    expect(e?.body).toBe(
      "The cluster had no room for it (CPU, memory or a scheduling rule). Nothing ran; no pull request was opened. " +
        "Retry once the cluster has room. The cluster reported: Unschedulable: 0/1 nodes are available: 1 Insufficient memory.",
    );
    expect(e?.body).not.toMatch(/stopped|started it/);
    expect(e?.details?.code).toBe("agent-start-failed");
  });

  it("does not deny the pull request an earlier session of the same build opened", () => {
    const e = startFailed(
      cycle("coding", "", { id: "c0", prNumber: 7, mergeSha: "abc" }),
      cycle("fix", "startup_failed:Unschedulable: no room"),
    );
    expect(e?.body).toContain("Nothing ran this time, so no new pull request was opened; #7, opened earlier in this build, is unchanged.");
  });

  it("names the validation agent on a validation cycle, and sends the reader to its Validation card to try again", () => {
    const e = startFailed(cycle("validation", "startup_failed:Unschedulable: no room"));
    expect(e?.title).toBe("The validation agent could not start");
    expect(e?.body).toContain("Nothing ran; the version was not validated.");
    expect(e?.body).toContain("Validate it again from its Validation card once the cluster has room.");
    expect(e?.next).toEqual({ label: "Go to Validation", to: "/projects/$projectName/validations" });
  });

  it("asks for the cause to be fixed when it is not a matter of room", () => {
    const e = startFailed(cycle("coding", "startup_failed:ImagePullBackOff"));
    expect(e?.body).toContain("The cluster could not pull its container image.");
    expect(e?.body).toContain("Retry once that is fixed.");
  });

  it("ends the cluster's report with exactly one period, whether or not it brought its own", () => {
    expect(startFailed(cycle("coding", "startup_failed:Unschedulable: preemption is not helpful. "))?.body).toMatch(
      /The cluster reported: Unschedulable: preemption is not helpful\.$/,
    );
    expect(startFailed(cycle("coding", "startup_failed:Unschedulable: no room"))?.body).toMatch(/The cluster reported: Unschedulable: no room\.$/);
    expect(startFailed(cycle("coding", "startup_failed:Unschedulable: no room!"))?.body).toMatch(/no room!$/);
  });
});

describe("the runner's own reason", () => {
  it("ends with one period, whether or not the runner brought its own", () => {
    const stopped = (agentReason: string) =>
      failureExplanation(
        run({
          state: "failed",
          terminalReason: "redispatch-budget",
          cycles: [{ id: "c1", kind: "coding", attempts: 2, createdAt: "2026-09-11T07:40:00Z", agentReason, recording: "kept" }] as never,
        }),
      )?.body;
    expect(stopped("timed_out")).toContain("The runner reported: timed_out. The coding");
    expect(stopped("the agent exited.")).toContain("The runner reported: the agent exited. The coding");
  });
});

