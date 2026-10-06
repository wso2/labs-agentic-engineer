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
import { resetStamp } from "../../../lib/resetStamp";
import { startupFailureCause, startupWaitNotice } from "./agentStart";

type RunCycleView = components["schemas"]["RunCycleView"];

const now = new Date("2026-10-06T14:00:00Z");
const failsAt = "2026-10-06T14:08:00Z";

const waiting = (reason: string, over: Partial<RunCycleView> = {}): RunCycleView => ({
  id: "c1",
  kind: "coding",
  attempts: 1,
  createdAt: "2026-10-06T13:58:00Z",
  startupWait: { reason, since: "2026-10-06T13:58:30Z", failsAt },
  ...over,
});

describe("startupWaitNotice — an agent the cluster has not started yet", () => {
  it("names the cluster's lack of room, in plain words, and when the run fails", () => {
    const n = startupWaitNotice(waiting("Unschedulable"), now);
    expect(n?.title).toBe("Waiting for room in the cluster to start the agent");
    expect(n?.body).toContain("The cluster has no room for the agent right now (CPU, memory or a scheduling rule).");
    expect(n?.body).toContain(`If it has not started by ${resetStamp(failsAt, now)}, this run fails.`);
  });

  // Only Unschedulable is a matter of room; the other causes name themselves.
  it.each([
    ["ImagePullBackOff", "The cluster cannot pull the agent's container image."],
    ["ErrImagePull", "The cluster cannot pull the agent's container image."],
    ["CreateContainerConfigError", "A secret or setting the agent needs is not ready yet."],
  ])("names %s in plain words", (reason, sentence) => {
    const n = startupWaitNotice(waiting(reason), now);
    expect(n?.title).toBe("Waiting to start the agent");
    expect(n?.body).toContain(sentence);
    expect(n?.body).not.toContain(reason);
  });

  it("shows a reason it has no words for as the cluster gave it", () => {
    const n = startupWaitNotice(waiting("RunContainerError"), now);
    expect(n?.title).toBe("Waiting to start the agent");
    expect(n?.body).toContain("The cluster reports the agent as waiting: RunContainerError.");
  });

  it("says nothing for a cycle with no wait, or one that has ended", () => {
    expect(startupWaitNotice(undefined, now)).toBeUndefined();
    expect(
      startupWaitNotice({ id: "c1", kind: "coding", attempts: 1, createdAt: "2026-10-06T13:58:00Z" }, now),
    ).toBeUndefined();
    expect(startupWaitNotice(waiting("Unschedulable", { endedAt: "2026-10-06T14:08:00Z" }), now)).toBeUndefined();
  });
});

describe("startupFailureCause — why an agent never started", () => {
  it("reads the cause off the cycle's startup_failed reason", () => {
    expect(startupFailureCause("startup_failed:Unschedulable: 0/1 nodes are available: 1 Insufficient cpu.")).toBe(
      "The cluster had no room for it (CPU, memory or a scheduling rule).",
    );
    expect(startupFailureCause("startup_failed:ImagePullBackOff")).toBe("The cluster could not pull its container image.");
    expect(startupFailureCause("startup_failed:CreateContainerConfigError: secret not found")).toBe(
      "A secret or setting it needed was not ready.",
    );
    expect(startupFailureCause("startup_failed:RunContainerError")).toBe("The cluster reported RunContainerError.");
  });

  it("has nothing to say about another kind of stop", () => {
    expect(startupFailureCause("agent_failed:OOMKilled")).toBeUndefined();
    expect(startupFailureCause(undefined)).toBeUndefined();
  });
});
