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
import type { CommentAnchor, DesignComment } from "../api/designModel";
import {
  addressComment,
  canReply,
  canResolve,
  openComments,
  pinComment,
  replyToComment,
  resolveComment,
  unresolvedOn,
} from "./comments";

const anchor: CommentAnchor = {
  label: "Go to Approved",
  view: "ClaimDetail",
  element: { attr: "aria-label", value: "Go to Approved", nth: 0 },
  x: 0.2,
  y: 0.7,
};

function pinned(): DesignComment[] {
  let comments = pinComment([], { id: "c1", artifactId: "prototype", anchor, text: " Show the receipts " });
  comments = pinComment(comments, { id: "c2", artifactId: "flow-approve", anchor: { ...anchor, view: null }, text: "Name Dana" });
  return comments;
}

describe("pinning", () => {
  it("numbers pins in the order they are made, open and unanswered", () => {
    const [first, second] = pinned();
    expect(first).toMatchObject({ n: 1, text: "Show the receipts", status: "open", reply: null, earlier: [] });
    expect(second).toMatchObject({ n: 2, status: "open" });
    expect(openComments(pinned()).map((c) => c.id)).toEqual(["c1", "c2"]);
  });
});

describe("the feedback loop", () => {
  it("goes open → addressed (with the reply and any spec line) → resolved", () => {
    const [c1, c2] = pinned();
    const addressed = addressComment(c1!, "Sam now sees the receipts.", "F2.2");
    expect(addressed).toMatchObject({ status: "addressed", reply: "Sam now sees the receipts.", specLine: "F2.2" });
    expect(canResolve(addressed) && canReply(addressed)).toBe(true);
    const resolved = resolveComment([addressed, c2!], "c1")!;
    expect(resolved[0]!.status).toBe("resolved");
    expect(unresolvedOn(resolved, "prototype")).toEqual([]);
  });

  it("takes a reply on an addressed comment back to open, keeping what was said before", () => {
    const addressed = addressComment(pinned()[0]!, "Receipts are shown.", null);
    const replied = replyToComment([addressed], "c1", "Show them larger")!;
    expect(replied[0]).toMatchObject({ status: "open", text: "Show them larger", earlier: ["Show the receipts"], reply: null });
  });

  it("refuses moves the state does not allow", () => {
    const comments = pinned();
    expect(resolveComment(comments, "c1")).toBeNull();
    expect(replyToComment(comments, "c1", "More")).toBeNull();
    expect(resolveComment(comments, "missing")).toBeNull();
    const resolved = resolveComment([addressComment(comments[0]!, "Done.", null)], "c1")!;
    expect(addressComment(resolved[0]!, "Again.", null)).toBe(resolved[0]);
    expect(replyToComment([addressComment(comments[0]!, "Done.", null)], "c1", "  ")).toBeNull();
  });
});
