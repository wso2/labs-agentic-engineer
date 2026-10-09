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

/**
 * `@wso2/prototype-kit/feedback`: what a reviewer's requests on a prototype
 * are, the one definition every host shares — the CLI's preview, the AEP
 * agent contract (`@aep/agent-stream`) and the console. Dependency-free and
 * synchronous, so it runs in Node and in any browser page.
 *
 *  - the request and submission shapes, their limits and the validator;
 *  - the Annotate queue both hosts keep (`queue.ts`): a request on what the
 *    reviewer is looking at, the numbered pins on a screen, editing and
 *    removing queued comments, and drafts;
 *  - the revision hash a submission names.
 *
 * Lengths are counted the way JavaScript counts them, in UTF-16 code units
 * (`string.length`); the Go server counts the same way.
 */

import { MAX_FEEDBACK_ID, MAX_FEEDBACK_REQUESTS, MAX_FEEDBACK_TEXT, PROTOTYPE_HASH_PATTERN, type FeedbackRequest, type FeedbackSubmission } from "./request.js";
import { sha256Hex } from "./sha256.js";

export { MAX_FEEDBACK_ID, MAX_FEEDBACK_REQUESTS, MAX_FEEDBACK_TEXT, PROTOTYPE_HASH_PATTERN, type FeedbackRequest, type FeedbackSubmission } from "./request.js";
export {
  EMPTY_FEEDBACK_QUEUE,
  dequeue,
  earlierComments,
  draftAt,
  draftOfPin,
  draftPinsOnScreen,
  editRequest,
  enqueue,
  keepDraft,
  keepOnScreen,
  onRevision,
  orphansOnScreen,
  pinsOnScreen,
  requestFor,
  screenPinsOnScreen,
  submissionOf,
  targetLabel,
  type FeedbackQueue,
  type PlacedRequest,
  type QueuedComment,
} from "./queue.js";

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const isId = (v: unknown): v is string => typeof v === "string" && v.trim() !== "" && v.length <= MAX_FEEDBACK_ID;

/** A request of the documented shape, or the rule it breaks. */
function parseRequest(v: unknown): { request: FeedbackRequest } | { reason: string } {
  if (!isObject(v)) return { reason: "is not an object" };
  const { screenId, flowId, roleId, stateId, elementIds, text } = v;
  for (const [name, id] of [["screenId", screenId], ["roleId", roleId], ["stateId", stateId]] as const) {
    if (!isId(id)) return { reason: `${name} must be a non-empty string of at most ${MAX_FEEDBACK_ID} characters` };
  }
  if (flowId !== undefined && !isId(flowId)) return { reason: `flowId must be a non-empty string of at most ${MAX_FEEDBACK_ID} characters` };
  if (!Array.isArray(elementIds) || !elementIds.every(isId)) return { reason: `elementIds must be a list of non-empty strings of at most ${MAX_FEEDBACK_ID} characters` };
  if (typeof text !== "string" || text.trim() === "") return { reason: "text is empty" };
  if (text.length > MAX_FEEDBACK_TEXT) return { reason: `text exceeds ${MAX_FEEDBACK_TEXT} characters` };
  return { request: { screenId: screenId as string, ...(flowId !== undefined ? { flowId } : {}), roleId: roleId as string, stateId: stateId as string, elementIds: [...elementIds], text } };
}

/**
 * A submission of the documented shape, or the first rule it breaks. Refused
 * whole, never trimmed: a batch applied only in part would read as sent and
 * done. Fields it does not know are dropped.
 */
export function parseFeedbackSubmission(value: unknown): { submission: FeedbackSubmission } | { reason: string } {
  if (!isObject(value) || typeof value["prototypeHash"] !== "string" || !PROTOTYPE_HASH_PATTERN.test(value["prototypeHash"])) {
    return { reason: "prototypeHash must be the 64-character revision hash" };
  }
  const requests = value["requests"];
  if (!Array.isArray(requests) || requests.length === 0) return { reason: "requests must be a non-empty list" };
  if (requests.length > MAX_FEEDBACK_REQUESTS) return { reason: `too many requests (max ${MAX_FEEDBACK_REQUESTS})` };
  const parsed: FeedbackRequest[] = [];
  for (const [i, r] of requests.entries()) {
    const result = parseRequest(r);
    if ("reason" in result) return { reason: `request ${i + 1} ${result.reason}` };
    parsed.push(result.request);
  }
  return { submission: { prototypeHash: value["prototypeHash"], requests: parsed } };
}

/**
 * A prototype revision's identity: the SHA-256 of the manifest's text, a NUL
 * and the source's, UTF-8 encoded, in lowercase hex. Feedback and persisted
 * data are keyed by it.
 */
export function prototypeHash(manifest: string, source: string): string {
  return sha256Hex(new TextEncoder().encode(`${manifest}\u0000${source}`));
}
