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

import type { components } from "../../../generated/aep-api";

type TaskView = components["schemas"]["TaskView"];

// Board-style buckets over TaskView.derivedStatus (#113 rework: /board is
// gone from the contract; the Build card derives its counts from list-tasks).
// `abandoned` is excluded from every bucket — it's not part of the journey.
const BUCKET_BY_STATUS: Record<string, keyof TaskBuckets["counts"]> = {
  pending: "todo",
  on_hold: "todo",
  in_progress: "inProgress",
  ready_for_review: "inProgress",
  building: "inProgress",
  merged: "done",
  deployed: "done",
  failed: "failed",
  rejected: "failed",
};

export interface TaskBuckets {
  counts: { todo: number; inProgress: number; failed: number; done: number };
  total: number;
  firstFailed?: TaskView;
  firstInProgress?: TaskView;
}

export function bucketTasks(tasks: TaskView[]): TaskBuckets {
  const buckets: TaskBuckets = {
    counts: { todo: 0, inProgress: 0, failed: 0, done: 0 },
    total: 0,
  };
  for (const task of tasks) {
    const bucket = BUCKET_BY_STATUS[task.derivedStatus];
    if (!bucket) continue;
    buckets.counts[bucket] += 1;
    buckets.total += 1;
    if (bucket === "failed") buckets.firstFailed ??= task;
    if (bucket === "inProgress") buckets.firstInProgress ??= task;
  }
  return buckets;
}
