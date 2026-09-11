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
        cycles: [{ id: "c1", kind: "coding", attempts: 2, createdAt: "2026-09-11T07:40:00Z", agentReason: "timed_out", recording: "none" }] as MilestoneRunView["cycles"],
      }),
    );
    expect(copy?.title).toBe("The coding agent stopped without opening a pull request");
    expect(copy?.body).toContain("The runner reported: timed_out");
  });

  it("points a deploy failure at Deployments and a validation failure at Validation", () => {
    expect(failureCopy(run({ terminalReason: "deploy-budget" }))?.next?.to).toBe("/projects/$projectName/deployments");
    expect(failureCopy(run({ terminalReason: "validation-failed" }))?.next?.to).toBe("/projects/$projectName/validation");
  });

  it("renders an unknown reason rather than swallowing it", () => {
    const copy = failureCopy(run({ terminalReason: "something-new" }));
    expect(copy?.title).toBe("The build failed — something-new");
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
