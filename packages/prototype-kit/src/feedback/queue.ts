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
 * The Annotate queue a review keeps, headless, so every host (the console,
 * the kit CLI's preview) keeps it the same way and only draws its own UI:
 *
 *  - queued comments, numbered 1.. as their pins show them, each on the
 *    revision it was written on; one can be edited or removed in place. The
 *    queue is on one revision, which the batch names: the one its first
 *    comment was written on, moved on to each revision that lands while it
 *    waits (`onRevision`), since the reviewer keeps writing against it;
 *  - drafts: a comment the reviewer started on some elements and closed
 *    without adding, kept per screen and set of elements (in whatever order
 *    they were selected) so selecting them again restores it. A draft is
 *    neither counted against the limit nor sent;
 *  - comments a revision orphaned: their elements are no longer drawn;
 *    one can be kept on its whole screen instead;
 *  - where a whole-screen comment was made, when the reviewer clicked a spot
 *    on the screen to make it (`at`): its pin is drawn there. The spot is the
 *    review's own; a submission never carries it.
 *
 * How the open comment's text becomes a draft is the host layer's
 * (`host/comment-draft.ts`).
 *
 * Every operation returns a new queue (the same one when it refuses).
 */

import type { FramePoint, FrameScreenPin } from "../host/bridge.js";
import type { PrototypeViewState } from "../host/view-state.js";
import { MAX_FEEDBACK_REQUESTS, type FeedbackRequest, type FeedbackSubmission } from "./request.js";

/**
 * A request as the review holds it: for a whole-screen comment made by
 * clicking a spot on the screen, that spot too (in the frame document's CSS
 * pixels, where its pin is drawn). Never sent.
 */
export interface PlacedRequest extends FeedbackRequest {
  at?: FramePoint | undefined;
}

/** A queued comment: the request, and the revision it was written on. */
export interface QueuedComment extends PlacedRequest {
  revision: string;
}

export interface FeedbackQueue {
  /** The revision the batch is on (see above); null while none is queued. */
  hash: string | null;
  requests: readonly QueuedComment[];
  /** Started comments, each on its own screen and set of elements; never sent. */
  drafts: readonly PlacedRequest[];
}

export const EMPTY_FEEDBACK_QUEUE: FeedbackQueue = Object.freeze({ hash: null, requests: [], drafts: [] });

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
 * What a comment is on, as the reviewer reads it, the same in every host's
 * bubble and list: its elements by their labels (`labels`, by element key, as
 * the frame reported its screen; the id where none is known), or the whole
 * screen.
 */
export function targetLabel(request: Pick<FeedbackRequest, "elementIds">, labels: Readonly<Record<string, string>>): string {
  if (request.elementIds.length === 0) return "Whole screen";
  return request.elementIds.map((id) => (Object.hasOwn(labels, id) ? labels[id] : id)).join(", ");
}

/** Whether `a` and `b` name the same elements, in any order. */
function sameElements(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((id) => b.includes(id));
}

const samePlace = (draft: PlacedRequest, screenId: string, elementIds: readonly string[]) =>
  draft.screenId === screenId && sameElements(draft.elementIds, elementIds);

const withoutDraftAt = (drafts: readonly PlacedRequest[], screenId: string, elementIds: readonly string[]) =>
  drafts.filter((d) => !samePlace(d, screenId, elementIds));

/** The request as the review keeps it: a spot only for a whole-screen comment (one on elements is pinned on them). */
function placed({ at, ...request }: PlacedRequest): PlacedRequest {
  return at !== undefined && request.elementIds.length === 0 ? { ...request, at } : request;
}

/** Queue a comment, refused once the queue is full; it uses up the draft on its elements. */
export function enqueue(queue: FeedbackQueue, hash: string, request: PlacedRequest): FeedbackQueue {
  if (queue.requests.length >= MAX_FEEDBACK_REQUESTS) return queue;
  return {
    hash: queue.hash ?? hash,
    requests: [...queue.requests, { ...placed(request), revision: hash }],
    drafts: withoutDraftAt(queue.drafts, request.screenId, request.elementIds),
  };
}

/** The `index`th (0-based) comment with new text; refused for empty text or a comment it does not hold. */
export function editRequest(queue: FeedbackQueue, index: number, text: string): FeedbackQueue {
  const request = queue.requests[index];
  if (!request || text.trim() === "") return queue;
  return { ...queue, requests: queue.requests.map((r, i) => (i === index ? { ...r, text } : r)) };
}

/** The queue without its `index`th (0-based) comment; the rest renumber. Emptied, it forgets its revision. */
export function dequeue(queue: FeedbackQueue, index: number): FeedbackQueue {
  const requests = queue.requests.filter((_, i) => i !== index);
  return { hash: requests.length === 0 ? null : queue.hash, requests, drafts: queue.drafts };
}

/**
 * The queue once revision `hash` landed: the batch names it, as the reviewer
 * now writes against it; each comment keeps the revision it was written on
 * (`earlierComments`). The same queue when it is empty or already there.
 */
export function onRevision(queue: FeedbackQueue, hash: string): FeedbackQueue {
  return queue.hash === null || queue.hash === hash ? queue : { ...queue, hash };
}

/** The 0-based numbers of the queued comments written on an earlier revision than `hash`, the one showing. */
export function earlierComments(queue: FeedbackQueue, hash: string): number[] {
  return queue.requests.flatMap((r, i) => (r.revision !== hash ? [i] : []));
}

/** The queue with its `index`th (0-based) comment kept on its whole screen, at its number: the elements it named are dropped (it has no spot, so no pin). */
export function keepOnScreen(queue: FeedbackQueue, index: number): FeedbackQueue {
  if (!queue.requests[index]) return queue;
  return { ...queue, requests: queue.requests.map((r, i) => (i === index ? { ...r, elementIds: [] } : r)) };
}

/**
 * The 0-based numbers of the queued comments a revision orphaned: written on
 * an earlier revision than `hash` (the one showing), on this screen, role and
 * state, and naming an element the screen no longer draws (`rendered`, as the
 * frame reports it for `hash`). Only those: as another role or in another
 * state the element may well be drawn.
 */
export function orphansOnScreen(queue: FeedbackQueue, hash: string, view: PrototypeViewState, rendered: readonly string[]): number[] {
  const drawn = new Set(rendered);
  return queue.requests.flatMap((r, i) =>
    r.revision !== hash && r.screenId === view.screenId && r.roleId === view.roleId && r.stateId === view.stateId && r.elementIds.some((id) => !drawn.has(id))
      ? [i]
      : [],
  );
}

/** Keep `draft` as the draft on its screen and elements (replacing any there); empty text drops it. */
export function keepDraft(queue: FeedbackQueue, draft: PlacedRequest): FeedbackQueue {
  const others = withoutDraftAt(queue.drafts, draft.screenId, draft.elementIds);
  return { ...queue, drafts: draft.text.trim() === "" ? others : [...others, placed(draft)] };
}

/** The draft on these elements of this screen (none: its whole-screen draft), if any. */
export function draftAt(queue: FeedbackQueue, screenId: string, elementIds: readonly string[]): PlacedRequest | undefined {
  return queue.drafts.find((d) => samePlace(d, screenId, elementIds));
}

/** The elements of this screen that show a draft pin: each draft's first element, once. A whole-screen draft has none. */
export function draftPinsOnScreen(queue: FeedbackQueue, screenId: string): string[] {
  return [...new Set(queue.drafts.filter((d) => d.screenId === screenId && d.elementIds.length > 0).map((d) => d.elementIds[0] as string))];
}

/** The draft the draft pin on `key` opens: the latest one drawn there. */
export function draftOfPin(queue: FeedbackQueue, screenId: string, key: string): PlacedRequest | undefined {
  return [...queue.drafts].reverse().find((d) => d.screenId === screenId && d.elementIds[0] === key);
}

/**
 * The whole-screen comments' pins on this screen, at their spots: each queued
 * one made at a spot, numbered as the queue numbers it, then one hollow pin:
 * at the spot of the whole-screen comment being written here (`writing`; none
 * when it has no spot), else at the screen's draft's spot, if it has one.
 * Comments without a spot (written from the list) have no pin.
 */
export function screenPinsOnScreen(queue: FeedbackQueue, screenId: string, writing: { at?: FramePoint | undefined } | null): FrameScreenPin[] {
  const pins: FrameScreenPin[] = queue.requests.flatMap((r, i) => (r.screenId === screenId && r.elementIds.length === 0 && r.at ? [{ at: r.at, number: i + 1 }] : []));
  const hollow = writing ? writing.at : draftAt(queue, screenId, [])?.at;
  return hollow ? [...pins, { at: hollow }] : pins;
}

/** A queued comment as the batch carries it: the request alone. */
function asRequest(c: QueuedComment): FeedbackRequest {
  return {
    screenId: c.screenId,
    ...(c.flowId !== undefined ? { flowId: c.flowId } : {}),
    roleId: c.roleId,
    stateId: c.stateId,
    elementIds: [...c.elementIds],
    text: c.text,
  };
}

/** What a host sends: the queued comments on their revision (never the drafts); null while none is queued. */
export function submissionOf(queue: FeedbackQueue): FeedbackSubmission | null {
  if (queue.hash === null || queue.requests.length === 0) return null;
  return { prototypeHash: queue.hash, requests: queue.requests.map(asRequest) };
}
