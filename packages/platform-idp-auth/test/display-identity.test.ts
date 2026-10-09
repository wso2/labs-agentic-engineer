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

// The cases of aep-api's TestParseDisplayIdentity (A/spec/collab_identity_test.go),
// over verified claims instead of a raw header: the header-parsing rows
// (empty, non-bearer, malformed) have no counterpart, because a verified
// token always parsed.

import { test } from "node:test";
import assert from "node:assert/strict";
import { displayIdentity } from "../src/index.js";

const base = { sub: "user-123", exp: 1 };

test("displayIdentity: an explicit name claim wins", () => {
  assert.deepEqual(displayIdentity({ ...base, name: "Ada Lovelace", email: "ada@x.io", given_name: "A" }), {
    name: "Ada Lovelace",
    email: "ada@x.io",
  });
});

test("displayIdentity: given + family assembled when there is no name", () => {
  assert.deepEqual(displayIdentity({ ...base, given_name: "Ada", family_name: "Lovelace" }), {
    name: "Ada Lovelace",
    email: "",
  });
});

test("displayIdentity: the placeholder 'User' surname is scrubbed, any case", () => {
  assert.equal(displayIdentity({ ...base, given_name: "Ada", family_name: "User" }).name, "Ada");
  assert.equal(displayIdentity({ ...base, given_name: " Ada ", family_name: "user" }).name, "Ada");
});

test("displayIdentity: falls back to the subject when no display field is set", () => {
  assert.deepEqual(displayIdentity(base), { name: "user-123", email: "" });
  assert.deepEqual(displayIdentity({ ...base, given_name: " ", family_name: "User" }), { name: "user-123", email: "" });
});
