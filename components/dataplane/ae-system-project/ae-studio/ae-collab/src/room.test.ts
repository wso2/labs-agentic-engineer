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
import { isSpecRoom } from "./room.js";

test("accepts spec-<org>-<project> rooms", () => {
  assert.equal(isSpecRoom("spec-acme-demo-shop"), true);
  assert.equal(isSpecRoom("spec-a-b"), true);
  assert.equal(isSpecRoom("spec-my-org-my-project"), true);
});

test("rejects rooms without both org and project segments", () => {
  assert.equal(isSpecRoom("spec-acme"), false);
  assert.equal(isSpecRoom("spec-"), false);
  assert.equal(isSpecRoom("spec"), false);
});

test("rejects non-spec prefixes and malformed slugs", () => {
  assert.equal(isSpecRoom("design-acme-shop"), false);
  assert.equal(isSpecRoom("spec-Acme-Shop"), false);
  assert.equal(isSpecRoom("spec-acme-shop-"), false);
  assert.equal(isSpecRoom("spec-acme/../etc"), false);
  assert.equal(isSpecRoom(""), false);
});

test("rejects oversized room ids", () => {
  assert.equal(isSpecRoom(`spec-a-${"b".repeat(200)}`), false);
});
