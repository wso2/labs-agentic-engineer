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

import { MAX_FEEDBACK_REQUESTS, type FeedbackRequest } from "@wso2/prototype-kit/feedback";
import type { PrototypeFeedback } from "../../agent-chat/turnScope";

// A review's Annotate queue, and the batch Send all makes of it. A request,
// its limits and the pins are the kit's (`@wso2/prototype-kit/feedback`, as the
// CLI's preview uses them): a request is made on what the reviewer is looking
// at (screen, flow, role, display state) for the elements selected, in
// selection order; none selected means the whole screen.

/** The queued requests, and the revision the first one was made on. */
export interface ReviewQueue {
  /** The hash of the prototype showing when the first request was queued. */
  hash: string;
  requests: FeedbackRequest[];
}

/** Queue a request: the queue's revision is the one showing when its first request is made. */
export function enqueue(queue: ReviewQueue | null, hash: string, request: FeedbackRequest): ReviewQueue {
  if (!queue || queue.requests.length === 0) return { hash, requests: [request] };
  if (queue.requests.length >= MAX_FEEDBACK_REQUESTS) return queue;
  return { hash: queue.hash, requests: [...queue.requests, request] };
}

/** The queue without its `index`th request; null once it is empty. */
export function dequeue(queue: ReviewQueue, index: number): ReviewQueue | null {
  const requests = queue.requests.filter((_, i) => i !== index);
  return requests.length === 0 ? null : { ...queue, requests };
}

/** What Send all sends: every queued request, on the revision they were made on. */
export function feedbackBatch(component: string, queue: ReviewQueue): PrototypeFeedback {
  return { prototypeHash: queue.hash, component, requests: queue.requests };
}
