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
import { prototypeNavReducer } from "../src/prototypeState.js";

test("navigate pushes the current screen onto the back stack", () => {
  const s = prototypeNavReducer({ current: "Login", stack: [] }, { type: "navigate", to: "Dashboard" });
  assert.deepEqual(s, { current: "Dashboard", stack: ["Login"] });
});

test("navigate to the current screen is a no-op", () => {
  const before = { current: "Login", stack: ["Home"] };
  assert.equal(prototypeNavReducer(before, { type: "navigate", to: "Login" }), before);
});

test("back pops the stack", () => {
  const s = prototypeNavReducer({ current: "Detail", stack: ["Login", "List"] }, { type: "back" });
  assert.deepEqual(s, { current: "List", stack: ["Login"] });
});

test("back on an empty stack is a no-op", () => {
  const before = { current: "Login", stack: [] };
  assert.equal(prototypeNavReducer(before, { type: "back" }), before);
});

test("reset jumps to a screen and clears the back stack — a flow switch starts a fresh walkthrough", () => {
  const s = prototypeNavReducer({ current: "Detail", stack: ["Login", "List"] }, { type: "reset", to: "AdminQueue" });
  assert.deepEqual(s, { current: "AdminQueue", stack: [] });
});

test("reset to the screen already shown still clears the stack", () => {
  const s = prototypeNavReducer({ current: "Login", stack: ["Home"] }, { type: "reset", to: "Login" });
  assert.deepEqual(s, { current: "Login", stack: [] });
});
