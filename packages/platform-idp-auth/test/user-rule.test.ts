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
import { orgRule, userRule } from "../src/index.js";

const pod = { orgId: "ou-1", orgHandle: "default" };

test("userRule: both claims must equal the pod's", () => {
  assert.equal(userRule({ sub: "u", exp: 1, ouId: "ou-1", ouHandle: "default" }, pod), true);
  assert.equal(userRule({ sub: "u", exp: 1, ouId: "ou-2", ouHandle: "default" }, pod), false);
  assert.equal(userRule({ sub: "u", exp: 1, ouId: "ou-1", ouHandle: "other" }, pod), false);
  assert.equal(userRule({ sub: "u", exp: 1, ouId: "ou-1" }, pod), false);
  assert.equal(userRule({ sub: "u", exp: 1, ouHandle: "default" }, pod), false);
  assert.equal(userRule({ sub: "u", exp: 1 }, pod), false);
});

test("userRule: an empty pod org fails closed", () => {
  const claims = { sub: "u", exp: 1, ouId: "", ouHandle: "" };
  assert.throws(() => userRule(claims, { orgId: "", orgHandle: "default" }), /orgId/);
  assert.throws(() => userRule(claims, { orgId: "ou-1", orgHandle: "" }), /orgHandle/);
});

test("orgRule: the org check alone, for any verified token kind", () => {
  // An ae-studio client token carries no sub; the org claims still decide.
  assert.equal(orgRule({ exp: 1, ouId: "ou-1", ouHandle: "default" }, pod), true);
  assert.equal(orgRule({ exp: 1, ouId: "ou-2", ouHandle: "default" }, pod), false);
  assert.equal(orgRule({ exp: 1, ouId: "ou-1", ouHandle: "other" }, pod), false);
  assert.equal(orgRule({ exp: 1 }, pod), false);
  assert.throws(() => orgRule({ exp: 1, ouId: "", ouHandle: "" }, { orgId: "", orgHandle: "default" }), /orgId/);
  assert.throws(() => orgRule({ exp: 1, ouId: "", ouHandle: "" }, { orgId: "ou-1", orgHandle: "" }), /orgHandle/);
});

test("userRule: the org rule plus a subject", () => {
  assert.equal(userRule({ sub: "", exp: 1, ouId: "ou-1", ouHandle: "default" }, pod), false);
});
