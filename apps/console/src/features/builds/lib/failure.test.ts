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
import { detailsText, failureCopy, failureLabel } from "./failure";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunFailure = components["schemas"]["RunFailure"];

const run = (over: Partial<MilestoneRunView> = {}): MilestoneRunView =>
  ({
    id: "96cdfd2d",
    milestoneNumber: 1,
    milestoneTitle: "v1",
    kind: "dev",
    origin: "spec-build",
    state: "failed",
    budgets: { cyclesTotal: 0, cycleCeiling: 8, fixCycles: 0, conflictCycles: 0, buildRetriggers: 0, validationCycles: 0 },
    validation: {},
    cycles: [],
    createdAt: "2026-09-11T07:35:31Z",
    ...over,
  }) as MilestoneRunView;

const planFault = (): RunFailure => ({
  code: "plan-turn-failed",
  phase: "planning",
  permanent: false,
  attempts: 4,
  maxAttempts: 0,
  firstAt: "2026-09-11T07:35:54Z",
  lastAt: "2026-09-11T07:36:54Z",
});

const sendgrid = (over: Partial<RunFailure> = {}): RunFailure => ({
  code: "dependency-unprovisionable",
  phase: "planning",
  component: "allocation-api",
  dependency: "sendgrid",
  permanent: true,
  attempts: 1,
  maxAttempts: 3,
  firstAt: "2026-09-11T07:35:54Z",
  lastAt: "2026-09-11T07:35:54Z",
  detail: 'external resourcetype "sendgrid": at least one config key required',
  workflowId: "dev-default-testdsxc1-1",
  ...over,
});

describe("failureLabel — the qualifier after the middot", () => {
  it("puts a failure code into words", () => {
    expect(failureLabel("dependency-unprovisionable")).toBe("Dependency could not be provisioned");
    expect(failureLabel("plan-failed")).toBe("Planning failed");
  });

  it("passes an unknown code through rather than hiding it", () => {
    expect(failureLabel("Merge conflict")).toBe("Merge conflict");
  });

  it("is nothing for nothing", () => {
    expect(failureLabel(undefined)).toBeUndefined();
    expect(failureLabel("")).toBeUndefined();
  });
});

describe("failureCopy — the sendgrid incident", () => {
  it("names the dependency, the component, what did not happen, and that retrying cannot help", () => {
    const copy = failureCopy(run({ failure: sendgrid() }));
    expect(copy?.tone).toBe("error");
    expect(copy?.title).toBe("The platform could not provision `sendgrid`");
    expect(copy?.body).toContain("allocation-api depends on it");
    expect(copy?.body).toContain("Nothing was coded or deployed");
    expect(copy?.body).toContain("Retrying cannot fix this");
  });

  it("sends the reader to the dependency's definition in the design", () => {
    const copy = failureCopy(run({ failure: sendgrid() }));
    expect(copy?.next).toEqual({
      label: "Open sendgrid in the design",
      to: "/projects/$projectName/spec",
      search: { file: "specs/design/dependencies/sendgrid/dependency.json" },
    });
  });

  it("carries the platform's facts for a bug report", () => {
    const copy = failureCopy(run({ failure: sendgrid() }));
    expect(copy?.details.code).toBe("dependency-unprovisionable");
    expect(copy?.details.permanent).toBe(true);
    expect(copy?.details.attempts).toBe("1 of 3");
    expect(copy?.details.detail).toContain("at least one config key required");
    expect(copy?.details.runId).toBe("96cdfd2d");
    expect(copy?.details.workflowId).toBe("dev-default-testdsxc1-1");
    expect(detailsText(copy!.details)).toBe(
      [
        "code: dependency-unprovisionable · permanent",
        "attempts: 1 of 3",
        `window: ${copy!.details.window}`,
        'recorded: external resourcetype "sendgrid": at least one config key required',
        "run: 96cdfd2d",
        "workflow: dev-default-testdsxc1-1",
      ].join("\n"),
    );
  });
});

describe("failureCopy — a fault being retried", () => {
  it("is amber, counts the attempts against the bound, and asks for nothing", () => {
    const copy = failureCopy(
      run({
        state: "planning",
        failure: sendgrid({ code: "dependency-provision-failed", permanent: false, attempts: 2, dependency: "orders-db", component: "api" }),
      }),
    );
    expect(copy?.tone).toBe("warning");
    expect(copy?.title).toBe("Provisioning `orders-db` failed — retrying (attempt 2 of 3)");
    expect(copy?.body).toContain("No action is needed yet");
    expect(copy?.next).toBeUndefined();
  });

  it("counts an unbounded retry without a denominator", () => {
    const copy = failureCopy(
      run({
        state: "planning",
        failure: planFault(),
      }),
    );
    expect(copy?.title).toBe("Planning the version's tasks failed — retrying (attempt 4)");
  });

  it("settles to red once the run has failed on it", () => {
    const copy = failureCopy(
      run({ failure: sendgrid({ code: "dependency-provision-failed", permanent: false, attempts: 3 }) }),
    );
    expect(copy?.tone).toBe("error");
    expect(copy?.body).toContain("It tried 3 times");
    expect(copy?.body).toContain("build again");
  });
});

describe("failureCopy — runs with no record", () => {
  it("says so honestly for a run failed before the record existed", () => {
    const copy = failureCopy(run({ terminalReason: "plan-failed" }));
    expect(copy?.tone).toBe("error");
    expect(copy?.title).toBe("The build failed while preparing the version");
    expect(copy?.body).toContain("recorded no further details");
    expect(copy?.details.code).toBe("plan-failed");
    expect(copy?.details.attempts).toBeUndefined();
  });

  it("reads the runner's own reason off the newest cycle", () => {
    const copy = failureCopy(
      run({
        terminalReason: "redispatch-budget",
        cycles: [{ id: "c1", kind: "coding", attempts: 2, createdAt: "2026-09-11T07:40:00Z", agentReason: "timed_out", recording: "kept" }] as MilestoneRunView["cycles"],
      }),
    );
    expect(copy?.title).toBe("The coding agent stopped without opening a pull request");
    expect(copy?.body).toContain("The runner reported: timed_out");
  });

  it("counts the dispatches that actually happened, not the budget", () => {
    const once = failureCopy(
      run({
        terminalReason: "redispatch-budget",
        cycles: [{ id: "c1", kind: "coding", attempts: 1, createdAt: "2026-09-11T07:40:00Z", agentReason: "agent_failed:OOMKilled", recording: "kept" }] as MilestoneRunView["cycles"],
      }),
    );
    // A cycle the pod-truth watcher closed cannot be re-dispatched, so this run
    // settled on one launch. Saying "twice" would describe an attempt nobody made.
    expect(once?.body).toContain("dispatched it once");
    expect(once?.body).not.toContain("twice");

    const twice = failureCopy(
      run({
        terminalReason: "redispatch-budget",
        cycles: [{ id: "c1", kind: "coding", attempts: 2, createdAt: "2026-09-11T07:40:00Z", recording: "kept" }] as MilestoneRunView["cycles"],
      }),
    );
    expect(twice?.body).toContain("dispatched it twice");
  });

  it("points a deploy failure at Deployments and a validation failure at Validation", () => {
    expect(failureCopy(run({ terminalReason: "deploy-budget" }))?.next?.to).toBe("/projects/$projectName/deployments");
    expect(failureCopy(run({ terminalReason: "validation-failed" }))?.next?.to).toBe("/projects/$projectName/validations");
  });

  it("renders an unknown reason rather than swallowing it", () => {
    const copy = failureCopy(run({ terminalReason: "something-new" }));
    expect(copy?.title).toBe("The build failed — something-new");
  });
});

describe("failureCopy — an agent that could not start", () => {
  const startFailed = (kind: string, agentReason?: string) =>
    run({
      terminalReason: "agent-start-failed",
      cycles: [
        {
          id: "c1",
          kind,
          attempts: 1,
          createdAt: "2026-09-11T07:40:00Z",
          endedAt: "2026-09-11T07:50:00Z",
          ...(agentReason ? { agentReason } : {}),
          recording: "kept",
        },
      ] as MilestoneRunView["cycles"],
    });

  it("says the coding agent never started, why, and that nothing ran", () => {
    const copy = failureCopy(
      startFailed("coding", "startup_failed:Unschedulable: 0/1 nodes are available: 1 Insufficient memory."),
    );
    expect(copy?.tone).toBe("error");
    expect(copy?.title).toBe("The coding agent could not start");
    expect(copy?.body).toContain("The cluster had no room for it (CPU, memory or a scheduling rule).");
    expect(copy?.body).toContain("Nothing ran; no pull request was opened.");
    expect(copy?.body).toContain("Retry this build once the cluster has room.");
    expect(copy?.body).toContain("The cluster reported: Unschedulable: 0/1 nodes are available: 1 Insufficient memory.");
    // It was never a stop: no dispatch count, no "stopped" verb.
    expect(copy?.body).not.toMatch(/stopped|dispatched/);
    expect(copy?.details.code).toBe("agent-start-failed");
  });

  it("does not deny the pull request an earlier session of the same build opened", () => {
    // A fix or conflict session dispatched after the first one merged #7: only
    // that later session could not start.
    const copy = failureCopy(
      run({
        terminalReason: "agent-start-failed",
        cycles: [
          {
            id: "c1",
            kind: "coding",
            attempts: 1,
            createdAt: "2026-09-11T07:00:00Z",
            endedAt: "2026-09-11T07:30:00Z",
            prNumber: 7,
            prUrl: "https://github.com/acme/orders/pull/7",
            mergeSha: "abc123",
            recording: "kept",
          },
          {
            id: "c2",
            kind: "fix",
            attempts: 1,
            createdAt: "2026-09-11T07:40:00Z",
            endedAt: "2026-09-11T07:50:00Z",
            agentReason: "startup_failed:Unschedulable: no room",
            recording: "kept",
          },
        ] as MilestoneRunView["cycles"],
      }),
    );
    expect(copy?.title).toBe("The coding agent could not start");
    expect(copy?.body).not.toContain("no pull request was opened");
    expect(copy?.body).toContain("Nothing ran this time, so no new pull request was opened; #7, opened earlier in this build, is unchanged.");
  });

  it("names the validation agent on a validation cycle, and its own way to try again", () => {
    const copy = failureCopy(startFailed("validation", "startup_failed:Unschedulable: no room"));
    expect(copy?.title).toBe("The validation agent could not start");
    expect(copy?.body).toContain("Nothing ran; the version was not validated.");
    expect(copy?.body).toContain("Run validation again once the cluster has room.");
    expect(copy?.body).not.toContain("pull request");
  });

  it("asks for the cause to be fixed when it is not a matter of room", () => {
    const copy = failureCopy(startFailed("coding", "startup_failed:ImagePullBackOff"));
    expect(copy?.body).toContain("The cluster could not pull its container image.");
    expect(copy?.body).toContain("Retry this build once that is fixed.");
  });

  it("labels the outcome after the middot", () => {
    expect(failureLabel("agent-start-failed")).toBe("Agent could not start");
  });
});

describe("failureCopy — a project with no environment to deploy into", () => {
  const noTarget = (): RunFailure => ({
    code: "no-write-target",
    phase: "deploying",
    permanent: true,
    attempts: 1,
    maxAttempts: 0,
    firstAt: "2026-09-11T07:35:54Z",
    lastAt: "2026-09-11T07:35:54Z",
    detail: "no write target for acme/shop: project names no deployment pipeline",
  });

  it("names the pipeline, says no fix task was filed, and that retrying cannot help", () => {
    const copy = failureCopy(run({ terminalReason: "no-write-target", failure: noTarget() }));
    expect(copy?.tone).toBe("error");
    expect(copy?.title).toBe("The project has no environment to deploy into");
    expect(copy?.body).toContain("deployment pipeline");
    expect(copy?.body).toContain("no fix task was filed");
    expect(copy?.body).toContain("Retrying cannot fix this");
    expect(copy?.details.detail).toContain("project names no deployment pipeline");
  });

  it("says the code merged and built when the deploy stage met the fault", () => {
    const copy = failureCopy(run({ terminalReason: "no-write-target", failure: noTarget() }));
    expect(copy?.body).toContain("The code merged and built; nothing was deployed");
  });

  it("says nothing was coded when the coding agent's dispatch met the fault", () => {
    const copy = failureCopy(run({ terminalReason: "no-write-target", failure: { ...noTarget(), phase: "coding" } }));
    expect(copy?.title).toBe("The project has no environment to deploy into");
    expect(copy?.body).toContain("Nothing was coded, built or deployed");
    expect(copy?.body).not.toContain("merged and built");
    expect(copy?.body).toContain("no fix task was filed");
  });

  it("explains itself from the reason alone when the record was never written", () => {
    const copy = failureCopy(run({ terminalReason: "no-write-target" }));
    expect(copy?.title).toBe("The project has no environment to deploy into");
    expect(copy?.body).not.toContain("merged and built");
    expect(copy?.body).not.toContain("Nothing was coded");
    expect(copy?.body).toContain("Retrying cannot fix this");
  });

  it("has a short label for the ledger", () => {
    expect(failureLabel("no-write-target")).toBe("No environment to deploy into");
  });
});

describe("failureCopy — nothing to explain", () => {
  it("draws nothing for a cancelled run, even with a record", () => {
    expect(failureCopy(run({ state: "cancelled", failure: sendgrid() }))).toBeUndefined();
  });

  it("draws nothing for a healthy run", () => {
    expect(failureCopy(run({ state: "running" }))).toBeUndefined();
    expect(failureCopy(run({ state: "succeeded" }))).toBeUndefined();
  });

  it("leaves a blocked run to its existing message", () => {
    expect(failureCopy(run({ state: "blocked", terminalReason: "agent-quota-blocked" }))).toBeUndefined();
  });
});

describe("failureCopy — a model provider's usage limit", () => {
  // Local-time stamps, so "the same day" holds in whatever zone the suite runs.
  const now = new Date(2026, 8, 26, 10, 0);
  const limit = (over: Partial<RunFailure> = {}): RunFailure => ({
    code: "model-provider-limit",
    phase: "coding",
    permanent: false,
    attempts: 1,
    maxAttempts: 1,
    firstAt: "2026-09-26T10:05:00Z",
    lastAt: "2026-09-26T10:05:00Z",
    host: "ollama.com",
    ...over,
  });
  const blocked = (failure?: RunFailure) =>
    run({ state: "blocked", terminalReason: "model-provider-limit", ...(failure ? { failure } : {}) });

  it("names the host and the reset time, in amber, on a blocked run", () => {
    const resetAt = new Date(2026, 8, 26, 14, 5).toISOString();
    const copy = failureCopy(blocked(limit({ resetAt })), now);
    expect(copy?.tone).toBe("warning");
    expect(copy?.title).toBe("ollama.com's usage limit was reached");
    expect(copy?.body).toMatch(/^The coding agent stopped when ollama\.com refused further requests, and nothing from that build session was merged\. /);
    expect(copy?.body).toMatch(/Start the run again after it resets \((14:05|0?2:05\s?PM)\)\.$/);
  });

  it("adds the date when the plan resets another day", () => {
    const resetAt = new Date(2026, 8, 29, 9, 30).toISOString();
    const copy = failureCopy(blocked(limit({ resetAt })), now);
    expect(copy?.body).toMatch(/Start the run again after it resets \(.*29.*\)\.$/);
  });

  it("promises no time when the provider stated none", () => {
    const copy = failureCopy(blocked(limit()), now);
    expect(copy?.title).toBe("ollama.com's usage limit was reached");
    expect(copy?.body).toMatch(/Start the run again once it resets — the provider did not say when\.$/);
    expect(copy?.body).not.toMatch(/\(/);
  });

  it("still explains itself when the record was never written", () => {
    const copy = failureCopy(blocked(), now);
    expect(copy?.title).toBe("The model provider's usage limit was reached");
    expect(copy?.body).toMatch(/when its model provider refused further requests/);
    expect(copy?.details.code).toBe("model-provider-limit");
  });

  it("reads the same while the run is still settling on it", () => {
    const copy = failureCopy(run({ state: "running", failure: limit() }), now);
    expect(copy?.tone).toBe("warning");
    expect(copy?.title).toBe("ollama.com's usage limit was reached");
  });

  it("has a short label for the ledger", () => {
    expect(failureLabel("model-provider-limit")).toBe("Model provider limit reached");
  });
});
