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
import { EMPTY_FEEDBACK_QUEUE, earlierComments, enqueue, prototypeHash, type FeedbackRequest } from "@wso2/prototype-kit/feedback";
import type { PrototypeManifest } from "@wso2/prototype-kit/host";
import { feedbackBatch } from "./feedback";
import type { AppPrototype, PrototypeFiles } from "./prototypes";
import { NEW_SESSION, follow, sendStarted, turnEnded, type ReviewSession } from "./revision";

const manifest: PrototypeManifest = {
  schemaVersion: 3,
  name: "Expenses",
  entryScreen: "screen.claims",
  roles: [{ id: "manager", name: "Manager" }],
  states: [{ id: "state.default", name: "Default" }],
  screens: [{ id: "screen.claims", name: "Claims", roleIds: ["manager"] }],
  flows: [],
};

const filesOf = (source: string): PrototypeFiles => ({ manifestText: JSON.stringify(manifest), manifest, source });
const V1 = filesOf("// one");
const V2 = filesOf("// two");
const hashOf = (f: PrototypeFiles) => prototypeHash(f.manifestText, f.source);

const app = (status: AppPrototype["status"], files: PrototypeFiles | null): AppPrototype => ({ component: "expense-web", status, problem: null, files, exists: true });

const request = (elementIds: string[], text: string): FeedbackRequest => ({ screenId: "screen.claims", roleId: "manager", stateId: "state.default", elementIds, text });

/** A session showing V1 that sent one comment, with one more held while the agent revises. */
function heldWhileRevising(): ReviewSession {
  let s = follow(NEW_SESSION, app("ready", V1));
  s = { ...s, queue: enqueue(s.queue, hashOf(V1), request(["btn.reject"], "Ask for a reason")) };
  s = sendStarted(s, feedbackBatch("expense-web", s.queue)!);
  s = follow(s, app("revising", V1));
  return { ...s, queue: enqueue(s.queue, hashOf(V1), request(["btn.approve"], "Make it green")) };
}

describe("a revision landing", () => {
  it("moves the held comments onto it, so the next batch names it, each still marked as written on the one before", () => {
    const landed = follow(turnEnded(heldWhileRevising(), "completed"), app("ready", V2));
    expect(landed.notice).toEqual({ kind: "updated", addressed: 1 });
    expect(feedbackBatch("expense-web", landed.queue)?.prototypeHash).toBe(hashOf(V2));
    expect(earlierComments(landed.queue, hashOf(V2))).toEqual([0]);
  });

  it("gives a failed batch back in front of the held comments, on the revision still showing", () => {
    const failed = follow(turnEnded(heldWhileRevising(), "failed"), app("ready", V1));
    expect(failed.queue.requests.map((r) => r.text)).toEqual(["Ask for a reason", "Make it green"]);
    expect(feedbackBatch("expense-web", failed.queue)?.prototypeHash).toBe(hashOf(V1));
    expect(earlierComments(failed.queue, hashOf(V1))).toEqual([]);
  });

  it("moves a failed batch onto what the turn wrote before it stopped, which is showing, and says so", () => {
    const failed = follow(turnEnded(heldWhileRevising(), "failed"), app("ready", V2));
    expect(failed.shown).toBe(V2);
    expect(failed.queue.requests.map((r) => r.text)).toEqual(["Ask for a reason", "Make it green"]);
    expect(feedbackBatch("expense-web", failed.queue)?.prototypeHash).toBe(hashOf(V2));
    expect(earlierComments(failed.queue, hashOf(V2))).toEqual([0, 1]);
    expect(failed.notice).toEqual({
      kind: "failed",
      reason: "The agent stopped partway, so you're seeing the changes it made before it stopped. Your comments are back in the queue: Retry sends them again.",
    });
  });

  it("says the previous version still shows when a failed turn changed nothing", () => {
    const failed = follow(turnEnded(heldWhileRevising(), "failed"), app("ready", V1));
    expect(failed.notice).toEqual({
      kind: "failed",
      reason: "The prototype wasn't updated, so you're still seeing the previous version. Your comments are back in the queue: Retry sends them again.",
    });
  });

  it("keeps an empty queue empty", () => {
    let s = sendStarted(follow(NEW_SESSION, app("ready", V1)), { prototypeHash: hashOf(V1), component: "expense-web", requests: [request([], "x")] });
    s = follow(follow(s, app("revising", V1)), app("ready", V2));
    expect(s.queue).toEqual(EMPTY_FEEDBACK_QUEUE);
  });
});
