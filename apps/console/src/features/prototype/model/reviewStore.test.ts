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

import { describe, expect, it, vi } from "vitest";
import { prototypeHash, type FeedbackRequest } from "@wso2/prototype-kit/feedback";
import type { PrototypeManifest } from "@wso2/prototype-kit/host";
import type { TurnOutcome } from "../../agent-chat/chatStore";
import type { PrototypeFeedback } from "../../agent-chat/turnScope";
import type { AppPrototype, PrototypeFiles } from "./prototypes";
import { sendStarted } from "./revision";
import { createReviewStore } from "./reviewStore";

const manifest: PrototypeManifest = {
  schemaVersion: 3,
  name: "Expenses",
  entryScreen: "screen.claims",
  roles: [{ id: "manager", name: "Manager" }],
  states: [{ id: "state.default", name: "Default" }],
  screens: [{ id: "screen.claims", name: "Claims", roleIds: ["manager"] }],
  flows: [],
};
const V1: PrototypeFiles = { manifestText: JSON.stringify(manifest), manifest, source: "// one" };
const C = "expense-web";
const app = (status: AppPrototype["status"]): AppPrototype => ({ component: C, status, problem: null, files: V1, exists: true });
const request: FeedbackRequest = { screenId: "screen.claims", roleId: "manager", stateId: "state.default", elementIds: ["btn.reject"], text: "Ask for a reason" };
const batch: PrototypeFeedback = { prototypeHash: prototypeHash(V1.manifestText, V1.source), component: C, requests: [request] };

/** A store on a stub chat whose turns end when the test says. */
function store() {
  const ends = new Set<(projectName: string, outcome: TurnOutcome) => void>();
  const reviews = createReviewStore({
    onTurnEnd: (fn) => {
      ends.add(fn);
      return () => ends.delete(fn);
    },
  });
  return { reviews, end: (projectName: string, outcome: TurnOutcome) => ends.forEach((fn) => fn(projectName, outcome)) };
}

describe("the review store", () => {
  it("keeps each project's sessions apart, and tells only that project's subscribers", () => {
    const { reviews } = store();
    const told = vi.fn();
    reviews.subscribe("acme", told);
    reviews.change("other", C, (s) => ({ ...s, revising: true }));
    expect(told).not.toHaveBeenCalled();
    reviews.change("acme", C, (s) => ({ ...s, revising: true }));
    expect(told).toHaveBeenCalledTimes(1);
    expect(reviews.get("acme")[C]?.revising).toBe(true);
    expect(reviews.get("other")[C]?.revising).toBe(true);
  });

  it("hands out the same sessions while nothing changed", () => {
    const { reviews } = store();
    reviews.follow("acme", [app("ready")]);
    const before = reviews.get("acme");
    reviews.follow("acme", [app("ready")]);
    expect(reviews.get("acme")).toBe(before);
  });

  it("records how a turn ended with no review open, and gives a failed batch back once the prototype is followed again", () => {
    const { reviews, end } = store();
    reviews.follow("acme", [app("ready")]);
    reviews.change("acme", C, (s) => sendStarted(s, batch));
    reviews.follow("acme", [app("revising")]);
    // The Prototype tab is left: nothing follows the prototype while the turn ends.
    end("acme", "failed");
    end("other", "completed");
    reviews.follow("acme", [app("ready")]);
    const s = reviews.get("acme")[C]!;
    expect(s.sent).toBeNull();
    expect(s.queue.requests.map((r) => r.text)).toEqual(["Ask for a reason"]);
    expect(s.notice).toMatchObject({ kind: "failed" });
  });

  it("settles a turn whose revising it never saw, once it knows how the turn ended", () => {
    const { reviews, end } = store();
    reviews.follow("acme", [app("ready")]);
    reviews.change("acme", C, (s) => sendStarted(s, batch));
    end("acme", "failed");
    reviews.follow("acme", [app("ready")]);
    expect(reviews.get("acme")[C]).toMatchObject({ sent: null, notice: { kind: "failed" } });
  });
});
