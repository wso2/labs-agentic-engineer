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
import type { StampedRunEvent } from "../hooks/useRunProgress";
import { agentLogLines, componentBuildState, externalValuesPark, isAgentStreaming, mergedCycle } from "./run";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];

const cycle = (id: string, rest: Partial<RunCycleView> = {}): RunCycleView => ({
  id,
  kind: "coding",
  attempts: 1,
  createdAt: "2026-10-04T10:00:00Z",
  ...rest,
});
const run = (state: MilestoneRunView["state"], cycles: RunCycleView[], rest: Partial<MilestoneRunView> = {}): MilestoneRunView =>
  ({ id: "r", state, cycles, ...rest }) as unknown as MilestoneRunView;

describe("a version's runs, for its Build card", () => {
  it("finds the newest merged build session across runs, never a validation cycle's", () => {
    const older = run("succeeded", [cycle("c1", { mergeSha: "a" }), cycle("c2", { mergeSha: "b" })]);
    const newer = run("running", [cycle("c3"), cycle("v1", { kind: "validation", mergeSha: "c" })]);
    expect(mergedCycle([newer, older])?.id).toBe("c2");
    expect(mergedCycle([run("running", [cycle("c4")])])).toBeUndefined();
  });

  it("says an agent writes only while a build session of the newest run is open", () => {
    expect(isAgentStreaming([run("running", [cycle("c1")])])).toBe(true);
    expect(isAgentStreaming([run("running", [cycle("c1", { endedAt: "2026-10-04T10:30:00Z" })])])).toBe(false);
    expect(isAgentStreaming([run("succeeded", [cycle("c1")])])).toBe(false);
  });

  it("reads a park at the deploy gate, naming the dependencies when the run does", () => {
    expect(externalValuesPark(run("waiting", [], { waitingReason: "external-values", blockingDependencies: ["xero"] }))).toEqual(["xero"]);
    expect(externalValuesPark(run("waiting", [], { waitingReason: "external-values" }))).toEqual([]);
    expect(externalValuesPark(run("running", []))).toBeNull();
  });

  it("reads a component build's outcome from the cluster's reason, and shows one it does not know", () => {
    expect(componentBuildState({ completed: false, status: "Running" })).toBe("running");
    expect(componentBuildState({ completed: true, status: "WorkflowSucceeded" })).toBe("succeeded");
    expect(componentBuildState({ completed: true, status: "Failed" })).toBe("failed");
    expect(componentBuildState({ completed: true, status: "Evicted" })).toBe("other");
  });

  it("prints the coding agent's log newest first", () => {
    const event = (seq: number, summary: string): StampedRunEvent =>
      ({ v: 2, seq, ts: `2026-10-04T10:0${seq}:00Z`, agentId: "lead", cycleId: "c1", attempt: 1, kind: "tool_use", tool: "Read", summary }) as StampedRunEvent;
    const lines = agentLogLines([event(1, "specs/prd.md"), event(2, "src/App.tsx")]);
    expect(lines).toHaveLength(2);
    expect(lines[0]).toMatch(/Read src\/App\.tsx$/);
    expect(lines[1]).toMatch(/Read specs\/prd\.md$/);
  });
});
