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
import { HAND_OFF_TO_ISSUES, HAND_OFF_TOOLS, handOffView, isHandOffTool, VIEWS } from "../src/contracts/sse-events.js";

test("the hand-off tool is named per view and recognised by name", () => {
  assert.equal(HAND_OFF_TO_ISSUES, "hand_off_to_issues");
  assert.equal(isHandOffTool("hand_off_to_issues"), true);
  assert.equal(isHandOffTool("ask_question"), false);
  assert.equal(isHandOffTool(undefined), false);
});

test("handOffView maps the tool name to the view it opens", () => {
  assert.equal(handOffView("hand_off_to_issues"), "issues");
  assert.equal(handOffView("ask_question"), undefined);
});

test("every view has exactly one hand-off tool", () => {
  assert.deepEqual(Object.keys(HAND_OFF_TOOLS).sort(), [...VIEWS].sort());
});
