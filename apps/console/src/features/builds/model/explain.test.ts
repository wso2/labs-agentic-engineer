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
