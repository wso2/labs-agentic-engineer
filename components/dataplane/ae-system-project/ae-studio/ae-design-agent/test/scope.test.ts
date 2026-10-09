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

import { test } from "node:test";
import assert from "node:assert/strict";
import { isTurnScope } from "@aep/agent-stream";
import { projectDisplayHistory } from "../src/conversation/display-history.js";
import type { Conversation } from "../src/store/conversation-store.js";

// A turn's scope (S6): what the user was looking at when they sent it. Its
// wording is pinned in turn-compose.test.ts; here, the shape it must have on
// the wire, and that a reload still says what each message was about.

test("a scope is a feature ID or the design review, and nothing else", () => {
  assert.ok(isTurnScope({ kind: "feature", feature: "F2" }));
  assert.ok(isTurnScope({ kind: "feature", feature: "F12" }));
  assert.ok(isTurnScope({ kind: "design-review" }));
  assert.ok(!isTurnScope({ kind: "feature", feature: "F2.3" }), "a story is not a feature");
  assert.ok(!isTurnScope({ kind: "feature", feature: "approvals" }));
  assert.ok(!isTurnScope({ kind: "feature" }));
  assert.ok(!isTurnScope({ kind: "design-review", feature: "F2" }));
  assert.ok(!isTurnScope({ kind: "product" }), "the whole product is the absence of a scope");
  assert.ok(!isTurnScope({ kind: "feature", feature: "F2", extra: true }));
  assert.ok(!isTurnScope(null));
});

test("the journalled scope is served back on the display read, and an unscoped message grows none", () => {
  const conv = {
    messages: [
      { role: "user", content: "composed prompt" },
      { role: "assistant", content: "done" },
      { role: "user", content: "composed prompt" },
    ],
    turns: [
      { turnId: "t-1", text: "deputies can approve up to 500", scope: { kind: "feature", feature: "F2" }, messageIndex: 0, createdAt: new Date() },
      { turnId: "t-2", text: "what next?", messageIndex: 2, createdAt: new Date() },
    ],
  } as unknown as Conversation;
  const [scoped, , unscoped] = projectDisplayHistory(conv);
  assert.deepEqual((scoped as { scope?: unknown }).scope, { kind: "feature", feature: "F2" });
  assert.ok(!("scope" in (unscoped as object)));
});
