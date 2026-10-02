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

/**
 * `isCollabConfig` — the agents server's pre-stream 400 check on a room-scoped
 * turn's `collab` block. TEMPORARY (phase 3 deletes): old agents joins the pod
 * Room, so the block names the Room's ws URL beside the room and the token.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { isCollabConfig } from "../src/contracts/sse-events.js";

const ok = { roomId: "spec-acme-shop", token: "t", url: "ws://ae-collab-x.example:19080/v1/rooms" };

test("accepts a room, a token and a ws or wss url", () => {
  assert.equal(isCollabConfig(ok), true);
  assert.equal(isCollabConfig({ ...ok, url: "wss://ae-collab-x.example/v1/rooms" }), true);
});

test("refuses a block without a usable ws url", () => {
  for (const bad of [
    { roomId: ok.roomId, token: ok.token },
    { ...ok, url: "" },
    { ...ok, url: 1 },
    { ...ok, url: "not a url" },
    { ...ok, url: "http://ae-collab-x.example/v1/rooms" },
  ]) {
    assert.equal(isCollabConfig(bad), false, JSON.stringify(bad));
  }
});

test("still refuses a missing room or token", () => {
  assert.equal(isCollabConfig({ ...ok, roomId: "" }), false);
  assert.equal(isCollabConfig({ ...ok, token: "" }), false);
  assert.equal(isCollabConfig(null), false);
});
