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

import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";
import {
  MAX_FEEDBACK_ID,
  MAX_FEEDBACK_REQUESTS,
  MAX_FEEDBACK_TEXT,
  parseFeedbackSubmission,
  pinsOnScreen,
  prototypeHash,
  requestFor,
  type FeedbackRequest,
} from "../src/feedback/index.js";
import type { PrototypeViewState } from "../src/host/view-state.js";
import { feedbackBatch, feedbackTable } from "./feedback-cases.js";

describe("feedback cases shared with agent-stream and the Go BFF", () => {
  it("states the kit's limits", () => {
    expect(feedbackTable.limits).toEqual({ requests: MAX_FEEDBACK_REQUESTS, text: MAX_FEEDBACK_TEXT, id: MAX_FEEDBACK_ID });
  });
  it.each(feedbackTable.cases.filter((c) => !c.component).map((c) => [c.name, c] as const))("%s", (_name, row) => {
    expect("submission" in parseFeedbackSubmission(feedbackBatch(row))).toBe(row.valid);
  });
});

describe("prototypeHash", () => {
  const node = (manifest: string, source: string) => createHash("sha256").update(manifest).update("\u0000").update(source).digest("hex");
  it.each([
    ["empty files", "", ""],
    ["a small revision", '{"schemaVersion":3}', "export default defineApp({});"],
    // 55, 56 and 64 bytes are where SHA-256's padding spills into another block.
    ["a message one byte short of a padding spill", "m".repeat(27), "s".repeat(27)],
    ["a message that spills its padding", "m".repeat(28), "s".repeat(27)],
    ["a message of exactly one block", "m".repeat(31), "s".repeat(32)],
    ["non-ASCII and astral characters", "{\"name\":\"Café ✓\"}", "const face = \"😀\"; // 𐍈"],
    ["a lone surrogate, encoded as U+FFFD by both", "\ud800", "x"],
    ["a large source", "{}", "x".repeat(262_144)],
  ])("matches node:crypto for %s", (_name, manifest, source) => {
    expect(prototypeHash(manifest, source)).toBe(node(manifest, source));
  });
  it("is synchronous and needs no Web Crypto", () => {
    expect(prototypeHash("a", "b")).toMatch(/^[0-9a-f]{64}$/);
  });
});

describe("the Annotate queue", () => {
  const view = (over: Partial<PrototypeViewState>): PrototypeViewState => ({
    mode: "annotate",
    roleId: "approver",
    screenId: "queue",
    flowId: null,
    stateId: "default",
    selectedKeys: [],
    ...over,
  });
  it("makes a request on what is showing, in selection order", () => {
    expect(requestFor(view({ flowId: "approve", selectedKeys: ["b", "a"] }), "Swap these")).toEqual({
      screenId: "queue",
      flowId: "approve",
      roleId: "approver",
      stateId: "default",
      elementIds: ["b", "a"],
      text: "Swap these",
    });
    expect(requestFor(view({}), "Whole screen")).not.toHaveProperty("flowId");
  });
  it("numbers the pins of this screen's requests", () => {
    const queue: FeedbackRequest[] = [
      { screenId: "queue", roleId: "r", stateId: "s", elementIds: ["a"], text: "1" },
      { screenId: "detail", roleId: "r", stateId: "s", elementIds: ["a"], text: "2" },
      { screenId: "queue", roleId: "r", stateId: "s", elementIds: ["a", "b"], text: "3" },
    ];
    expect(pinsOnScreen(queue, "queue")).toEqual({ a: [1, 3], b: [3] });
  });
});
