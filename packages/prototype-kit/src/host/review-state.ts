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
 * A review's view (the view reducer: pickers, mode, selection) and its
 * comment bubble, as one state, headless, so every host opens and closes the
 * bubble the same way and only draws it. The bubble is what the reviewer is
 * writing or reading a comment on: the selected elements, the whole screen
 * (a click on empty space in Annotate, at that spot, or a host's "Comment on
 * this screen"), or a queued comment opened from a host's list or its pin.
 */

import type { FeedbackRequest } from "../feedback/request.js";
import type { FramePoint } from "./bridge.js";
import type { PrototypeManifest } from "../manifest/types.js";
import { initialPrototypeView, reducePrototypeView, type PrototypeViewEvent, type PrototypeViewState } from "./view-state.js";
/** What the open comment bubble is on; null when none is open. */
export type CommentBubble =
  | { on: "selection" }
  /** The whole screen: at the spot of the frame's document a click on empty space hit, or (no spot) from a host's own control. */
  | { on: "screen"; at?: FramePoint | undefined }
  /**
   * The queued comment at `index` (0-based, as the queue holds it); `pin`
   * when its pin opened it (the pin's element and numbers, where keyboard
   * focus goes back to).
   */
  | { on: "comment"; index: number; pin?: CommentPin | undefined }
  | null;

/** A pin in the frame, as `PrototypeFrame.onPin` names it: on the element `key`, or (no key) a whole-screen comment's pin (`onScreenPin`). */
export interface CommentPin {
  key?: string | undefined;
  requests: number[];
}

export interface ReviewState {
  view: PrototypeViewState;
  bubble: CommentBubble;
}

export type ReviewEvent =
  | PrototypeViewEvent
  /** Comment on the whole screen showing (switches to Annotate); at the spot `at` when given (a draft's hollow pin reopening it). */
  | { type: "COMMENT_ON_SCREEN"; at?: FramePoint | undefined }
  /**
   * A click on empty space in Annotate, at `at` (`PrototypeFrame.onScreenClick`):
   * a click away from an open bubble closes it; else it opens a whole-screen
   * comment there.
   */
  | { type: "SCREEN_CLICK"; at: FramePoint }
  /** Go to where a queued comment was made and open it. */
  | { type: "OPEN_COMMENT"; index: number; request: FeedbackRequest }
  /** A queued comment's pin was clicked (either mode): open it where it is, letting the selection go. */
  | { type: "OPEN_PIN"; index: number; pin: CommentPin }
  /** Close the bubble; the selection stays (Escape clears it next). */
  | { type: "CLOSE_BUBBLE" };

export function initialReview(manifest: PrototypeManifest): ReviewState {
  return { view: initialPrototypeView(manifest), bubble: null };
}

/** Whether two views show the same thing: screen, flow, role, display state and mode. */
function samePlace(a: PrototypeViewState, b: PrototypeViewState): boolean {
  return a.mode === b.mode && a.screenId === b.screenId && a.flowId === b.flowId && a.roleId === b.roleId && a.stateId === b.stateId;
}

export function reduceReview(manifest: PrototypeManifest, s: ReviewState, e: ReviewEvent): ReviewState {
  switch (e.type) {
    case "COMMENT_ON_SCREEN": {
      const view = reducePrototypeView(manifest, s.view, s.view.mode === "annotate" ? { type: "CLEAR_SELECTION" } : { type: "ENTER_ANNOTATE" });
      return { view, bubble: e.at ? { on: "screen", at: e.at } : { on: "screen" } };
    }
    case "SCREEN_CLICK":
      // The frame is untrusted: only Annotate comments on a click.
      if (s.view.mode !== "annotate") return s;
      if (s.bubble) return { ...s, bubble: null };
      return { view: reducePrototypeView(manifest, s.view, { type: "CLEAR_SELECTION" }), bubble: { on: "screen", at: e.at } };
    case "OPEN_COMMENT": {
      const { request } = e;
      const view = initialPrototypeView(manifest, {
        screen: request.screenId,
        flow: request.flowId,
        role: request.roleId,
        state: request.stateId,
        mode: "annotate",
      });
      return { view, bubble: { on: "comment", index: e.index } };
    }
    case "OPEN_PIN":
      return { view: reducePrototypeView(manifest, s.view, { type: "CLEAR_SELECTION" }), bubble: { on: "comment", index: e.index, pin: e.pin } };
    case "CLOSE_BUBBLE":
      return { ...s, bubble: null };
    default: {
      const view = reducePrototypeView(manifest, s.view, e);
      return { view, bubble: bubbleAfter(s, view, e) };
    }
  }
}

/**
 * The bubble once the view changed: a click that selects opens it on the
 * selection; clearing the selection closes it; otherwise it stays open only
 * on what it was on, if that is still there (a selection still held; a
 * screen or comment still showing).
 */
function bubbleAfter(s: ReviewState, view: PrototypeViewState, e: PrototypeViewEvent): CommentBubble {
  // Letting the selection go ends the comment (it was added, or the reviewer moved on).
  if (e.type === "CLEAR_SELECTION") return null;
  // A click the reducer refused (an element it does not know, Preview) changes nothing.
  if (view === s.view) return s.bubble;
  const selected = view.mode === "annotate" && view.selectedKeys.length > 0;
  if (e.type === "SELECT_ONLY" || e.type === "TOGGLE_SELECTION" || e.type === "SELECT_ELEMENTS") return selected ? { on: "selection" } : null;
  if (s.bubble?.on === "selection") return selected ? s.bubble : null;
  return s.bubble && !selected && samePlace(s.view, view) ? s.bubble : null;
}
