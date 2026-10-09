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
/** The open new comment's text following the review: kept as a draft where it was written, never lost. */

import { describe, expect, it } from "vitest";
import { EMPTY_FEEDBACK_QUEUE, draftAt, draftPinsOnScreen, keepDraft, type FeedbackRequest } from "../src/feedback/index.js";
import { followComment, keepOpenComment, newComment } from "../src/host/comment-draft.js";
import type { ReviewState } from "../src/host/review-state.js";

const on = (elementIds: string[], text: string, screenId = "queue"): FeedbackRequest => ({ screenId, roleId: "approver", stateId: "default", elementIds, text });

const view = (selectedKeys: string[], screenId = "queue") => ({
  mode: "annotate" as const,
  roleId: "approver",
  screenId,
  flowId: null,
  stateId: "default",
  selectedKeys,
});
/** A review with its bubble open on the selection (or closed, with nothing selected). */
const selecting = (keys: string[], screenId = "queue"): ReviewState => ({ view: view(keys, screenId), bubble: keys.length > 0 ? { on: "selection" } : null });
/** A review with its bubble open on the whole screen (at a spot, when a click on the screen opened it). */
const onScreen = (screenId = "queue", at?: { x: number; y: number }): ReviewState => ({ view: view([], screenId), bubble: at ? { on: "screen", at } : { on: "screen" } });
const closed = (screenId = "queue"): ReviewState => ({ view: view([], screenId), bubble: null });

describe("the open comment following the review", () => {
  it("keeps typed text as a draft when the comment closes, and opens empty next time", () => {
    expect(followComment(EMPTY_FEEDBACK_QUEUE, selecting(["a"]), "Half a thought", closed(), false)).toEqual({
      queue: { ...EMPTY_FEEDBACK_QUEUE, drafts: [on(["a"], "Half a thought")] },
      text: "",
    });
  });

  it("restores the draft when its elements are selected again", () => {
    const q = keepDraft(EMPTY_FEEDBACK_QUEUE, on(["a"], "Half a thought"));
    expect(followComment(q, closed(), "", selecting(["a"]), false)).toEqual({ queue: q, text: "Half a thought" });
  });

  it("keeps the text of a comment moved to other elements as a draft there, and opens theirs", () => {
    const q = keepDraft(EMPTY_FEEDBACK_QUEUE, on(["b"], "About b"));
    const moved = followComment(q, selecting(["a"]), "About a", selecting(["b"]), false);
    expect(moved.text).toBe("About b");
    expect(draftAt(moved.queue, "queue", ["a"])?.text).toBe("About a");
  });

  it("carries the text when Shift adds or takes out an element", () => {
    expect(followComment(EMPTY_FEEDBACK_QUEUE, selecting(["a"]), "Swap", selecting(["a", "b"]), true)).toEqual({ queue: EMPTY_FEEDBACK_QUEUE, text: "Swap" });
  });

  it("keeps the text as a draft when Shift takes out the last element", () => {
    expect(followComment(EMPTY_FEEDBACK_QUEUE, selecting(["a"]), "Swap", closed(), true).queue.drafts).toEqual([on(["a"], "Swap")]);
  });

  it("changes nothing while the comment stays where it is", () => {
    const q = keepDraft(EMPTY_FEEDBACK_QUEUE, on(["a"], "old"));
    expect(followComment(q, selecting(["a"]), "typing", selecting(["a"]), false)).toEqual({ queue: q, text: "typing" });
    expect(followComment(q, onScreen(), "typing", onScreen(), false)).toEqual({ queue: q, text: "typing" });
  });

  it("keeps nothing for an empty comment, and drops the draft whose text was cleared", () => {
    const q = keepDraft(EMPTY_FEEDBACK_QUEUE, on(["a"], "old"));
    expect(followComment(q, selecting(["a"]), "", closed(), false).queue.drafts).toEqual([]);
  });

  it("keeps the draft on the screen it was written on", () => {
    const moved = followComment(EMPTY_FEEDBACK_QUEUE, selecting(["a"]), "About a", closed("detail"), false);
    expect(draftPinsOnScreen(moved.queue, "queue")).toEqual(["a"]);
  });

  it("keeps a whole-screen comment's text as the screen's draft, which the screen's bubble opens with again", () => {
    const left = followComment(EMPTY_FEEDBACK_QUEUE, onScreen(), "About the page", closed(), false);
    expect(left).toEqual({ queue: { ...EMPTY_FEEDBACK_QUEUE, drafts: [on([], "About the page")] }, text: "" });
    expect(followComment(left.queue, closed(), "", onScreen(), false).text).toBe("About the page");
  });

  it("keeps the whole-screen text apart from the elements' when a click moves the comment to an element", () => {
    const moved = followComment(EMPTY_FEEDBACK_QUEUE, onScreen(), "About the page", selecting(["a"]), false);
    expect(moved.text).toBe("");
    expect(draftAt(moved.queue, "queue", [])?.text).toBe("About the page");
  });

  it("keeps a whole-screen comment's text as the screen's draft at the spot it was written at, and opens it at another spot", () => {
    const here = { x: 40, y: 900 };
    const left = followComment(EMPTY_FEEDBACK_QUEUE, onScreen("queue", here), "About the page", closed(), false);
    expect(left.queue.drafts).toEqual([{ ...on([], "About the page"), at: here }]);
    expect(followComment(left.queue, closed(), "", onScreen("queue", { x: 1, y: 2 }), false).text).toBe("About the page");
    expect(keepOpenComment(EMPTY_FEEDBACK_QUEUE, onScreen("queue", here), "Unsaved").drafts).toEqual([{ ...on([], "Unsaved"), at: here }]);
  });

  it("follows no comment while a queued one is open", () => {
    const reading: ReviewState = { view: view([]), bubble: { on: "comment", index: 0 } };
    expect(followComment(EMPTY_FEEDBACK_QUEUE, reading, "", closed(), false)).toEqual({ queue: EMPTY_FEEDBACK_QUEUE, text: "" });
  });
});

describe("the review going away with a comment open", () => {
  it("keeps the open comment's text as a draft where it was written", () => {
    expect(keepOpenComment(EMPTY_FEEDBACK_QUEUE, selecting(["a"]), "Unsaved").drafts).toEqual([on(["a"], "Unsaved")]);
    expect(keepOpenComment(EMPTY_FEEDBACK_QUEUE, onScreen(), "Unsaved").drafts).toEqual([on([], "Unsaved")]);
  });

  it("keeps nothing when no new comment is open or it is empty", () => {
    expect(keepOpenComment(EMPTY_FEEDBACK_QUEUE, closed(), "stale")).toBe(EMPTY_FEEDBACK_QUEUE);
    expect(keepOpenComment(EMPTY_FEEDBACK_QUEUE, selecting(["a"]), "  ")).toBe(EMPTY_FEEDBACK_QUEUE);
  });
});

describe("the comment the open bubble adds", () => {
  it("is on the selected elements, or on the whole screen at the spot clicked, or none when no new comment is open", () => {
    expect(newComment(selecting(["a", "b"]), "Swap")).toEqual(on(["a", "b"], "Swap"));
    expect(newComment(onScreen("queue", { x: 4, y: 800 }), "Busy")).toEqual({ ...on([], "Busy"), at: { x: 4, y: 800 } });
    expect(newComment(onScreen(), "Busy")).toEqual(on([], "Busy"));
    expect(newComment(closed(), "stale")).toBeNull();
  });
});
