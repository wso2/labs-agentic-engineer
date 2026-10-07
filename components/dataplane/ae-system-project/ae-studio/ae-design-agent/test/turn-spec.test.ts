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
 * The turn's web-search and MCP-catalog gates (port of aep-api's
 * `designOrCollabTurn` / `catalogTurn`, `A/spec/turn_runner.go:134-162`).
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { catalogTurn, designOrRoomTurn } from "../src/turns/turn-spec.js";

test("web search: the design flow, or any turn writing through the Room", () => {
  assert.equal(designOrRoomTurn({ flow: "design", roomScoped: false }), true);
  assert.equal(designOrRoomTurn({ flow: "", roomScoped: true }), true);
  assert.equal(designOrRoomTurn({ flow: "start", roomScoped: false }), false);
  assert.equal(designOrRoomTurn({ flow: "", roomScoped: false }), false);
});

test("MCP catalog: every web-search turn plus the requirements flows anywhere", () => {
  // Main's catalogTurn (#878): the requirements loop is /start, /interview F<n> and refine's branches.
  for (const flow of ["start", "interview", "refine", "feature", "actor", "amend", "settle"]) {
    assert.equal(catalogTurn({ flow, roomScoped: false }), true, flow);
  }
  assert.equal(catalogTurn({ flow: "design", roomScoped: false }), true);
  assert.equal(catalogTurn({ flow: "", roomScoped: true }), true);
  assert.equal(catalogTurn({ flow: "", roomScoped: false }), false);
  assert.equal(catalogTurn({ flow: "prototype", roomScoped: false }), false);
});
