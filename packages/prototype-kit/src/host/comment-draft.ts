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
 * The open new comment's text, never lost, headless, so every host keeps it
 * the same way: whenever its bubble closes, or a plain click moves it to
 * other elements, or the screen it was on goes, the text is kept as a draft
 * where it was written (on the elements, or on the whole screen), and a
 * bubble opening where a draft is kept starts from it. Shift-click carries
 * the text along to the grown or shrunk selection.
 */

import { draftAt, keepDraft, requestFor, type FeedbackQueue, type PlacedRequest } from "../feedback/queue.js";
import type { FramePoint } from "./bridge.js";
import type { ReviewState } from "./review-state.js";
import type { PrototypeViewState } from "./view-state.js";

/** Where a new comment is written: the view, its selection the elements (none: the whole screen), and the whole screen's spot, if any. */
type Place = PrototypeViewState & { at?: FramePoint | undefined };

/** Where the open new comment is: the selected elements, or (none) the whole screen, at its spot; null when no new comment is open. */
function writingOn({ view, bubble }: ReviewState): Place | null {
  if (bubble?.on === "selection") return view;
  if (bubble?.on === "screen") return { ...view, selectedKeys: [], at: bubble.at };
  return null;
}

/** The draft `text` makes where it was written. */
function draftOf(place: Place, text: string): PlacedRequest {
  return place.at ? { ...requestFor(place, text), at: place.at } : requestFor(place, text);
}

/** Whether two places a comment is written on are the same: the screen and its elements, in any order (a whole screen's spot aside). */
function samePlace(a: Place | null, b: Place | null): boolean {
  if (a === null || b === null) return a === b;
  return a.screenId === b.screenId && a.selectedKeys.length === b.selectedKeys.length && a.selectedKeys.every((k) => b.selectedKeys.includes(k));
}

/**
 * The queue and the open comment's text once the review moved from `before`
 * (with `text` typed there) to `after`. `carry`: the move was a Shift-click,
 * which takes the text along to the new selection.
 */
export function followComment(queue: FeedbackQueue, before: ReviewState, text: string, after: ReviewState, carry: boolean): { queue: FeedbackQueue; text: string } {
  const from = writingOn(before);
  const to = writingOn(after);
  if (samePlace(from, to)) return { queue, text };
  if (carry && from !== null && to !== null && from.selectedKeys.length > 0 && to.selectedKeys.length > 0) return { queue, text };
  const kept = from === null ? queue : keepDraft(queue, draftOf(from, text));
  return { queue: kept, text: to === null ? "" : (draftAt(kept, to.screenId, to.selectedKeys)?.text ?? "") };
}

/** The comment the open new comment's `text` adds: on the selected elements, or on the whole screen at its spot; null when no new comment is open. */
export function newComment(review: ReviewState, text: string): PlacedRequest | null {
  const on = writingOn(review);
  return on && draftOf(on, text);
}

/** The queue once the review went away with `text` typed in its open comment: the text kept as a draft there. */
export function keepOpenComment(queue: FeedbackQueue, review: ReviewState, text: string): FeedbackQueue {
  const on = writingOn(review);
  return on === null || text.trim() === "" ? queue : keepDraft(queue, draftOf(on, text));
}
