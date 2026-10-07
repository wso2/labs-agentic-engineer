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
 *  - the Annotate queue's helpers: a request on what the reviewer is looking
 *    at, and the numbered pins on a screen;
 *  - the revision hash a submission names.
 *
 * Lengths are counted the way JavaScript counts them, in UTF-16 code units
 * (`string.length`); the Go server counts the same way.
 */

import type { PrototypeViewState } from "../host/view-state.js";
import { sha256Hex } from "./sha256.js";

/** The most requests one submission takes. */
export const MAX_FEEDBACK_REQUESTS = 50;

/** The longest request text, in UTF-16 code units. */
export const MAX_FEEDBACK_TEXT = 4000;

/** The longest screen, flow, role, state, element or component id, in UTF-16 code units. */
export const MAX_FEEDBACK_ID = 200;

/** A revision hash: 64 lowercase hex characters. */
export const PROTOTYPE_HASH_PATTERN = /^[0-9a-f]{64}$/;

export interface FeedbackRequest {
  screenId: string;
  flowId?: string;
  roleId: string;
  stateId: string;
  /** The selected elements' ids, in selection order; empty for the whole screen. */
  elementIds: string[];
  text: string;
}

/** A batch of requests on one revision. */
export interface FeedbackSubmission {
  /** The revision the requests were made on (`prototypeHash`). */
  prototypeHash: string;
  requests: FeedbackRequest[];
}

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

/** A request on the current screen, flow, role and state, for the selection in the order it was made (none: the whole screen). */
export function requestFor(view: PrototypeViewState, text: string): FeedbackRequest {
  return {
    screenId: view.screenId,
    ...(view.flowId !== null ? { flowId: view.flowId } : {}),
    roleId: view.roleId,
    stateId: view.stateId,
    elementIds: [...view.selectedKeys],
    text,
  };
}

/** For each element a queued request on this screen names, the requests' 1-based queue numbers. */
export function pinsOnScreen(queue: readonly FeedbackRequest[], screenId: string): Record<string, number[]> {
  const pins: Record<string, number[]> = {};
  queue.forEach((request, i) => {
    if (request.screenId !== screenId) return;
    for (const id of request.elementIds) pins[id] = [...(pins[id] ?? []), i + 1];
  });
  return pins;
}

/**
 * A prototype revision's identity: the SHA-256 of the manifest's text, a NUL
 * and the source's, UTF-8 encoded, in lowercase hex. Feedback and persisted
 * data are keyed by it.
 */
export function prototypeHash(manifest: string, source: string): string {
  return sha256Hex(new TextEncoder().encode(`${manifest}\u0000${source}`));
}
