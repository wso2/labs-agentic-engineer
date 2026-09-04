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

// AN ISSUE'S STATUS LINE — what the agent working it says it is doing now.
//
// The platform asks whoever works an issue to keep its newest comment current
// (`skills/aep/SKILL.md`, "The status line"), and two console surfaces read that
// answer: the Builds page's task rows, and the Validation page's tile. They read
// the same thing and must read it the same way, so the reader lives here rather
// than beside either of them — the first copy of this logic was inside
// `taskRowNote`, and a second one would have been free to disagree with it about
// which comment counts and where the line ends.
//
// It is deliberately not in `@aep/progress-view`. That package is about the
// runner's progress ENVELOPE, which this is not: the status line comes from
// GitHub, on a poll, and survives a reload precisely because it is not an event.

import type { components } from "../../../generated/aep-api";

type TaskView = components["schemas"]["TaskView"];
type TaskDetail = components["schemas"]["TaskDetail"];
type IssueComment = components["schemas"]["IssueComment"];

/** Either shape that carries a comment thread — the list's row, or the detail. */
type Commented = Pick<TaskView | TaskDetail, "comments">;

/**
 * The issue's NEWEST comment.
 *
 * `comments` arrives OLDEST FIRST and is never an empty array — the contract
 * omits the field entirely for every empty case — so the newest is the last
 * element. The platform's own machine comments are already excluded server-side;
 * what is left is the agent's progress notes and whatever a human wrote.
 */
export function latestComment(task: Commented): IssueComment | undefined {
  return task.comments?.at(-1);
}

/**
 * The newest comment's first line, or null when there is nothing to say.
 *
 * A comment body is markdown over an unbounded textarea, and every consumer of
 * this renders ONE line — so it is flattened to the first non-empty line and the
 * surface clamps what is left. The full thread is on the issue, which every
 * consumer links to.
 *
 * Null rather than a placeholder: an empty line is quieter than "No updates yet"
 * repeated down a list, and it is what lets a caller fall back to something it
 * knows instead of printing an apology.
 */
export function statusLine(task: Commented): string | null {
  const body = latestComment(task)?.body;
  if (!body) return null;
  return (
    body
      .split("\n")
      .map((line) => line.trim())
      .find((line) => line.length > 0) ?? null
  );
}
