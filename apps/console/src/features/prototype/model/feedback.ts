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

import { EMPTY_FEEDBACK_QUEUE, MAX_FEEDBACK_REQUESTS, submissionOf, type FeedbackQueue } from "@wso2/prototype-kit/feedback";
import type { PrototypeFeedback } from "../../agent-chat/turnScope";

// What Send makes of a review's comment queue. The queue itself is the kit's
// (`@wso2/prototype-kit/feedback`, as the CLI's preview keeps it): queued
// comments made on what the reviewer is looking at, edited and removed in
// place, and drafts, which are kept but never sent.

/** What Send sends: the queued comments on the revision they were made on, as the component's feedback; null while none is queued. */
export function feedbackBatch(component: string, queue: FeedbackQueue): PrototypeFeedback | null {
  const submission = submissionOf(queue);
  return submission && { prototypeHash: submission.prototypeHash, component, requests: submission.requests };
}

/**
 * The queue with a sent batch the agent did not apply put back in front of
 * what was written since, on the revision the batch was made on (the one
 * showing, unless the turn wrote files before it failed: the session then
 * moves the queue on to them, `revision.ts`).
 */
export function restored(queue: FeedbackQueue, batch: PrototypeFeedback): FeedbackQueue {
  const given = batch.requests.map((r) => ({ ...r, revision: batch.prototypeHash }));
  return { hash: batch.prototypeHash, requests: [...given, ...queue.requests], drafts: queue.drafts };
}

/**
 * Whether no other comment can be queued: `queued` comments are, and `held`
 * are out with the agent (0 when no batch is). A batch out keeps its places,
 * so given back by a failed turn (`restored`) it still fits one batch with
 * the comments written meanwhile.
 */
export function queueFull(queued: number, held: number): boolean {
  return queued + held >= MAX_FEEDBACK_REQUESTS;
}

/** The queue once its comments are sent: empty, keeping the drafts. */
export function sent(queue: FeedbackQueue): FeedbackQueue {
  return { ...EMPTY_FEEDBACK_QUEUE, drafts: queue.drafts };
}

/** "1 comment", "2 comments": how the review counts comments. */
export function commentCount(n: number): string {
  return `${n} ${n === 1 ? "comment" : "comments"}`;
}
