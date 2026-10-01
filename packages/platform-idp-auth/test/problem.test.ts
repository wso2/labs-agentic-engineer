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
import { problem } from "../src/index.js";

test("problem builds an application/problem+json body", () => {
  assert.deepEqual(problem(401, "unauthenticated", "a valid bearer token is required"), {
    status: 401,
    body: { type: "about:blank", title: "Unauthorized", status: 401, detail: "a valid bearer token is required", code: "unauthenticated" },
  });
  assert.deepEqual(problem(403, "org_mismatch").body, { type: "about:blank", title: "Forbidden", status: 403, detail: "", code: "org_mismatch" });
});
