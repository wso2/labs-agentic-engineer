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
import { MAX_FEEDBACK_REQUESTS, requestFor, type FeedbackRequest } from "@wso2/prototype-kit/feedback";
import { initialPrototypeView, reducePrototypeView, type PrototypeManifest } from "@wso2/prototype-kit/host";
import { dequeue, enqueue, feedbackBatch } from "./feedback";

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

describe("the queue", () => {
  it("keeps the revision its first request was made on", () => {
    let queue = enqueue(null, "a".repeat(64), request("screen.claims", []));
    queue = enqueue(queue, "b".repeat(64), request("screen.claim", []));
    expect(queue.hash).toBe("a".repeat(64));
    expect(queue.requests).toHaveLength(2);
  });

  it("takes no more than the kit's limit", () => {
    let queue = enqueue(null, "a".repeat(64), request("screen.claims", []));
    for (let i = 1; i < MAX_FEEDBACK_REQUESTS + 5; i++) queue = enqueue(queue, "a".repeat(64), request("screen.claims", []));
    expect(queue.requests).toHaveLength(MAX_FEEDBACK_REQUESTS);
  });

  it("drops a request, and is gone with its last one", () => {
    const queue = enqueue(enqueue(null, "a".repeat(64), request("screen.claims", [], "1")), "a".repeat(64), request("screen.claim", [], "2"));
    expect(dequeue(queue, 0)?.requests.map((r) => r.text)).toEqual(["2"]);
    expect(dequeue(dequeue(queue, 0)!, 0)).toBeNull();
  });

  it("is sent whole as the component's feedback batch", () => {
    const queue = enqueue(null, "c".repeat(64), request("screen.claim", ["btn.approve"]));
    expect(feedbackBatch("expense-web", queue)).toEqual({ prototypeHash: "c".repeat(64), component: "expense-web", requests: queue.requests });
  });
});
