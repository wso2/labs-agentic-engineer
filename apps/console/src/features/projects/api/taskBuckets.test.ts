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
import { bucketTasks } from "./taskBuckets";

type TaskView = components["schemas"]["TaskView"];

function task(issueNumber: number, derivedStatus: string): TaskView {
  return {
    issueNumber,
    derivedStatus,
    title: `task ${issueNumber}`,
    issueUrl: `https://github.com/acme/demo/issues/${issueNumber}`,
    attention: null,
    dependsOn: null,
    executions: {},
    hold: false,
    lineage: {},
  };
}

describe("bucketTasks", () => {
  it("buckets derivedStatus into board-style counts", () => {
    const result = bucketTasks([
      task(1, "pending"),
      task(2, "on_hold"),
      task(3, "in_progress"),
      task(4, "ready_for_review"),
      task(5, "building"),
      task(6, "merged"),
      task(7, "deployed"),
      task(8, "failed"),
      task(9, "rejected"),
    ]);
    expect(result.counts).toEqual({
      todo: 2,
      inProgress: 3,
      failed: 2,
      done: 2,
    });
    expect(result.total).toBe(9);
    expect(result.firstFailed?.issueNumber).toBe(8);
    expect(result.firstInProgress?.issueNumber).toBe(3);
  });

  it("excludes abandoned and unknown statuses", () => {
    const result = bucketTasks([task(1, "abandoned"), task(2, "???")]);
    expect(result.total).toBe(0);
    expect(result.firstFailed).toBeUndefined();
  });
});
