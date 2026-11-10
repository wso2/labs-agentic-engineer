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

/** An org as the dialect reads it; `declared` false means the handle came from a fallback claim. */
const org = (id: string, handle: string, declared = true) => ({ id, handle, name: "", handleDeclared: declared });

test("userRule: the org's id and its declared handle must both equal the pod's", () => {
  assert.equal(userRule({ sub: "u", exp: 1, org: org("ou-1", "default") }, pod), true);
  assert.equal(userRule({ sub: "u", exp: 1, org: org("ou-2", "default") }, pod), false);
  assert.equal(userRule({ sub: "u", exp: 1, org: org("ou-1", "other") }, pod), false);
  assert.equal(userRule({ sub: "u", exp: 1, org: org("ou-1", "") }, pod), false);
  assert.equal(userRule({ sub: "u", exp: 1, org: org("", "default") }, pod), false);
  // The handle filled in from a name or id is not a declared handle: the
  // fallback serves aep-api's user edge for legacy tokens, never the pod.
  assert.equal(userRule({ sub: "u", exp: 1, org: org("ou-1", "default", false) }, pod), false);
});

test("userRule: an empty pod org fails closed", () => {
  const claims = { sub: "u", exp: 1, org: org("", "", false) };
  assert.throws(() => userRule(claims, { orgId: "", orgHandle: "default" }), /orgId/);
  assert.throws(() => userRule(claims, { orgId: "ou-1", orgHandle: "" }), /orgHandle/);
});

test("orgRule: the org check alone, for any verified token kind", () => {
  // No sub, as an ae-studio client token may be.
  assert.equal(orgRule({ exp: 1, org: org("ou-1", "default") }, pod), true);
  assert.equal(orgRule({ exp: 1, org: org("ou-2", "default") }, pod), false);
  assert.equal(orgRule({ exp: 1, org: org("ou-1", "other") }, pod), false);
  assert.equal(orgRule({ exp: 1, org: org("ou-1", "default", false) }, pod), false);
  assert.throws(() => orgRule({ exp: 1, org: org("", "", false) }, { orgId: "", orgHandle: "default" }), /orgId/);
  assert.throws(() => orgRule({ exp: 1, org: org("", "", false) }, { orgId: "ou-1", orgHandle: "" }), /orgHandle/);
});

test("userRule: the org rule plus a subject", () => {
  assert.equal(userRule({ sub: "", exp: 1, org: org("ou-1", "default") }, pod), false);
});
