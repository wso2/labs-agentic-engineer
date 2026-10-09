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

import { describe, expect, it } from "vitest";
import { EMPTY_FEEDBACK_QUEUE, enqueue, keepDraft, requestFor, type FeedbackRequest } from "@wso2/prototype-kit/feedback";
import { initialPrototypeView, reducePrototypeView, type PrototypeManifest } from "@wso2/prototype-kit/host";
import { feedbackBatch, sent } from "./feedback";

const manifest: PrototypeManifest = {
  schemaVersion: 3,
  name: "Expenses",
  entryScreen: "screen.claims",
  roles: [{ id: "manager", name: "Manager" }],
  states: [{ id: "state.default", name: "Default" }],
  screens: [
    { id: "screen.claims", name: "Claims", roleIds: ["manager"] },
    { id: "screen.claim", name: "Claim", roleIds: ["manager"] },
  ],
  flows: [{ id: "flow.approve", name: "Approve", roleId: "manager", screenIds: ["screen.claims", "screen.claim"] }],
};

const request = (screenId: string, elementIds: string[], text = "Change it"): FeedbackRequest => ({
  screenId,
  roleId: "manager",
  stateId: "state.default",
  elementIds,
  text,
});

describe("a request on the review", () => {
  it("is the kit's: made on what the reviewer looks at, for the selection in the order it was made", () => {
    let view = reducePrototypeView(manifest, initialPrototypeView(manifest), { type: "SET_FLOW", flowId: "flow.approve" });
    view = reducePrototypeView(manifest, view, { type: "ENTER_ANNOTATE" });
    view = reducePrototypeView(manifest, view, { type: "TOGGLE_SELECTION", elementKey: "btn.reject" });
    view = reducePrototypeView(manifest, view, { type: "TOGGLE_SELECTION", elementKey: "btn.approve" });
    expect(requestFor(view, "Swap these")).toEqual({
      screenId: "screen.claims",
      flowId: "flow.approve",
      roleId: "manager",
      stateId: "state.default",
      elementIds: ["btn.reject", "btn.approve"],
      text: "Swap these",
    });
  });
});

describe("the batch Send makes", () => {
  const hash = "c".repeat(64);

  it("is the queued comments, on their revision, as the component's feedback", () => {
    const queue = enqueue(EMPTY_FEEDBACK_QUEUE, hash, request("screen.claim", ["btn.approve"]));
    expect(feedbackBatch("expense-web", queue)).toEqual({ prototypeHash: hash, component: "expense-web", requests: [request("screen.claim", ["btn.approve"])] });
  });

  it("leaves the drafts out, and is nothing while only drafts are kept", () => {
    const drafted = keepDraft(EMPTY_FEEDBACK_QUEUE, request("screen.claims", ["btn.reject"], "Half a thought"));
    expect(feedbackBatch("expense-web", drafted)).toBeNull();
    expect(feedbackBatch("expense-web", enqueue(drafted, hash, request("screen.claim", [])))?.requests).toEqual([request("screen.claim", [])]);
  });

  it("empties the queue once sent, keeping the drafts", () => {
    const draft = request("screen.claims", ["btn.reject"], "Half a thought");
    const queue = enqueue(keepDraft(EMPTY_FEEDBACK_QUEUE, draft), hash, request("screen.claim", []));
    expect(sent(queue)).toEqual({ ...EMPTY_FEEDBACK_QUEUE, drafts: [draft] });
  });
});
