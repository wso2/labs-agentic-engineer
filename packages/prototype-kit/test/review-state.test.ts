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
/** The review's comment bubble alongside the view, as both hosts open and close it. */

import { describe, expect, it } from "vitest";
import type { FeedbackRequest } from "../src/feedback/index.js";
import type { PrototypeManifest } from "../src/manifest/types.js";
import { initialReview, reduceReview, type ReviewEvent, type ReviewState } from "../src/host/review-state.js";

const manifest: PrototypeManifest = {
  schemaVersion: 3,
  name: "Expenses",
  entryScreen: "queue",
  roles: [{ id: "approver", name: "Approver" }],
  states: [
    { id: "default", name: "Default" },
    { id: "empty", name: "Empty" },
  ],
  screens: [
    { id: "queue", name: "Queue", roleIds: ["approver"] },
    { id: "detail", name: "Detail", roleIds: ["approver"] },
  ],
  flows: [],
};

const run = (...events: ReviewEvent[]): ReviewState => events.reduce((s, e) => reduceReview(manifest, s, e), initialReview(manifest));

const comment: FeedbackRequest = { screenId: "detail", roleId: "approver", stateId: "empty", elementIds: ["btn.approve"], text: "Bigger" };

describe("the comment bubble", () => {
  it("opens on the element a click in Annotate selects, and nowhere in Preview", () => {
    expect(run({ type: "SELECT_ONLY", elementKey: "a" }).bubble).toBeNull();
    const s = run({ type: "ENTER_ANNOTATE" }, { type: "SELECT_ONLY", elementKey: "a" });
    expect(s.bubble).toEqual({ on: "selection" });
    expect(s.view.selectedKeys).toEqual(["a"]);
  });

  it("stays open on the selection as Shift adds to it, and closes when the selection empties", () => {
    const s = run({ type: "ENTER_ANNOTATE" }, { type: "SELECT_ONLY", elementKey: "a" }, { type: "TOGGLE_SELECTION", elementKey: "b" });
    expect(s.bubble).toEqual({ on: "selection" });
    expect(s.view.selectedKeys).toEqual(["a", "b"]);
    expect(reduceReview(manifest, reduceReview(manifest, s, { type: "TOGGLE_SELECTION", elementKey: "a" }), { type: "TOGGLE_SELECTION", elementKey: "b" }).bubble).toBeNull();
  });

  it("closes and keeps the selection, then the selection goes", () => {
    const closed = run({ type: "ENTER_ANNOTATE" }, { type: "SELECT_ONLY", elementKey: "a" }, { type: "CLOSE_BUBBLE" });
    expect(closed.bubble).toBeNull();
    expect(closed.view.selectedKeys).toEqual(["a"]);
    expect(reduceReview(manifest, closed, { type: "CLEAR_SELECTION" }).view.selectedKeys).toEqual([]);
  });

  it("opens on the whole screen from Preview, entering Annotate, and closes when the screen changes", () => {
    const s = run({ type: "COMMENT_ON_SCREEN" });
    expect(s.bubble).toEqual({ on: "screen" });
    expect(s.view.mode).toBe("annotate");
    expect(reduceReview(manifest, s, { type: "NAVIGATE", screenId: "detail" }).bubble).toBeNull();
  });

  it("opens a queued comment where it was made", () => {
    const s = run({ type: "OPEN_COMMENT", index: 2, request: comment });
    expect(s.bubble).toEqual({ on: "comment", index: 2 });
    expect(s.view).toMatchObject({ screenId: "detail", stateId: "empty", mode: "annotate", selectedKeys: [] });
  });

  it("opens a pin's comment where it is, in either mode, letting the selection go", () => {
    const pin = { key: "a", requests: [1] };
    expect(run({ type: "OPEN_PIN", index: 0, pin }).bubble).toEqual({ on: "comment", index: 0, pin });
    const s = run({ type: "ENTER_ANNOTATE" }, { type: "SELECT_ONLY", elementKey: "b" }, { type: "OPEN_PIN", index: 0, pin });
    expect(s.view.selectedKeys).toEqual([]);
    expect(s.view.mode).toBe("annotate");
  });

  it("keeps an open comment through a click the reducer refuses, and closes it on another selection", () => {
    const opened = run({ type: "OPEN_PIN", index: 0, pin: { key: "a", requests: [1] } });
    expect(reduceReview(manifest, opened, { type: "SELECT_ONLY", elementKey: "b" }).bubble).toEqual(opened.bubble);
    const annotating = run({ type: "ENTER_ANNOTATE" }, { type: "OPEN_PIN", index: 0, pin: { key: "a", requests: [1] } });
    expect(reduceReview(manifest, annotating, { type: "SELECT_ONLY", elementKey: "b" }).bubble).toEqual({ on: "selection" });
  });

  it("opens on the whole screen at the spot a click on empty space in Annotate hit, letting the selection go", () => {
    const at = { x: 40, y: 900 };
    const s = run({ type: "ENTER_ANNOTATE" }, { type: "SELECT_ONLY", elementKey: "a" }, { type: "CLOSE_BUBBLE" }, { type: "SCREEN_CLICK", at });
    expect(s.bubble).toEqual({ on: "screen", at });
    expect(s.view.selectedKeys).toEqual([]);
  });

  it("closes an open bubble on a click on empty space, as a click away does, keeping the selection", () => {
    const open = run({ type: "ENTER_ANNOTATE" }, { type: "SELECT_ONLY", elementKey: "a" });
    const s = reduceReview(manifest, open, { type: "SCREEN_CLICK", at: { x: 1, y: 1 } });
    expect(s.bubble).toBeNull();
    expect(s.view.selectedKeys).toEqual(["a"]);
  });

  it("opens nothing on a click on empty space in Preview, where the frame should not report one", () => {
    const s = run({ type: "SCREEN_CLICK", at: { x: 1, y: 1 } });
    expect(s).toEqual(initialReview(manifest));
  });

  it("opens a whole-screen comment's pin where it is, naming no element", () => {
    const pin = { requests: [2] };
    expect(run({ type: "ENTER_ANNOTATE" }, { type: "OPEN_PIN", index: 1, pin }).bubble).toEqual({ on: "comment", index: 1, pin });
  });
});
