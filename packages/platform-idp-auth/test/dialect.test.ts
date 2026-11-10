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
import { dialectFor } from "../src/dialect.js";
import { parseDialectKind } from "../src/index.js";

test("parseDialectKind: empty is thunder; a kind this build does not know throws", () => {
  assert.equal(parseDialectKind(undefined), "thunder");
  assert.equal(parseDialectKind(""), "thunder");
  assert.equal(parseDialectKind(" Thunder "), "thunder");
  assert.throws(() => parseDialectKind("asgardeo"), /unknown platform IdP kind/);
  assert.throws(() => parseDialectKind("okta"), /unknown platform IdP kind/);
});

// The Thunder org precedence (ouHandle, else ouName, else ouId), and whether
// the handle was the token's own.
test("thunder: the org and whether its handle was declared", () => {
  const d = dialectFor("thunder");
  assert.deepEqual(d.org({ ouHandle: "acme", ouName: "Acme Inc", ouId: "ou-1" }), {
    handle: "acme",
    id: "ou-1",
    name: "Acme Inc",
    handleDeclared: true,
  });
  assert.deepEqual(d.org({ ouName: "Acme Inc", ouId: "ou-1" }), { handle: "Acme Inc", id: "ou-1", name: "Acme Inc", handleDeclared: false });
  assert.deepEqual(d.org({ ouId: "ou-1" }), { handle: "ou-1", id: "ou-1", name: "", handleDeclared: false });
  assert.deepEqual(d.org({ grant_type: "authorization_code" }), { handle: "", id: "", name: "", handleDeclared: false });
  // A non-string claim is no claim.
  assert.equal(d.org({ ouHandle: 42 }).handleDeclared, false);
});

// client_credentials is a machine's; any other grant a user's; no grant
// answers neither, so a verifier can refuse it rather than guess.
test("thunder: a machine's token by grant_type", () => {
  const d = dialectFor("thunder");
  assert.equal(d.machine({ grant_type: "client_credentials" }), true);
  assert.equal(d.machine({ grant_type: "authorization_code" }), false);
  assert.equal(d.machine({ grant_type: "refresh_token" }), false);
  assert.equal(d.machine({ ouHandle: "acme" }), undefined);
});
