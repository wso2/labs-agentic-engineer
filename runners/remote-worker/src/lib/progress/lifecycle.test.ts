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
import { PROVISIONING, WORKSPACE_READY } from "./lifecycle.js";

// The regression these pin: the two pre-session lines briefly carried their
// English here ("[workspace] provisioning"), which both changed the copy a user
// reads and moved it out of `@aep/progress-view` — the one place the console and
// the playground both read their wording from. A producer that ships a sentence
// is how one fact starts reading two ways on two surfaces.
test("lifecycle: the pre-session lines name a CONDITION, not a sentence", () => {
  assert.deepEqual(PROVISIONING, { kind: "notice", level: "info", code: "workspace_provisioning" });
  assert.deepEqual(WORKSPACE_READY, { kind: "notice", level: "info", code: "workspace_ready" });
});

test("lifecycle: neither line carries prose of its own", () => {
  for (const ev of [PROVISIONING, WORKSPACE_READY]) {
    assert.equal(ev.detail, undefined, "the wording belongs to @aep/progress-view");
    assert.equal(ev.summary, undefined);
  }
});
