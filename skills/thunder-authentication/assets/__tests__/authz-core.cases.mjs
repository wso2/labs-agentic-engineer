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

// The cases for ../authz-core.ts. NOT named *.test.mjs on purpose: importing a
// .ts module needs `--experimental-strip-types` on the Node this repo pins, and
// the repo-wide runner (`node --test $(find skills -name '*.test.mjs')`) passes
// no flags. ./authz-core.test.mjs is the wrapper that runs this file with the
// flag; run it directly with
//
//   node --experimental-strip-types --test authz-core.cases.mjs
//
// when you want the per-case output.

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  canReach,
  classifyApiFailure,
  createUnauthorizedHandler,
  granted,
  heldRoles,
  parseScopes,
  rolesGranting,
  tokenIsValid,
} from "../authz-core.ts";

// The Expense Tracker catalog, as security.json declares it.
const ROLE_GRANTS = {
  Approver: ["claims:approve", "claims:read", "claims:read-all", "claims:reject", "reports:read"],
  Employee: ["claims:read", "claims:submit"],
};

const held = (...scopes) => new Set(scopes);

// --- parseScopes ------------------------------------------------------------

test("parseScopes splits the space-separated claim", () => {
  assert.deepEqual(
    [...parseScopes("openid profile email group ou claims:read claims:submit")],
    ["openid", "profile", "email", "group", "ou", "claims:read", "claims:submit"],
  );
});

test("parseScopes treats a missing or empty claim as no scopes", () => {
  assert.equal(parseScopes(undefined).size, 0);
  assert.equal(parseScopes("").size, 0);
  assert.equal(parseScopes("   ").size, 0);
});

test("the OIDC scopes being present is NOT being authorized", () => {
  // The one confusion the header table warns about: a token always carries
  // openid/profile/email/group/ou, so "the token has scopes" is never "the
  // caller may do this".
  const scopes = parseScopes("openid profile email group ou");
  assert.equal(granted(scopes, "claims:read"), false);
  assert.equal(granted(scopes, "openid"), true);
  assert.deepEqual(heldRoles(scopes, ROLE_GRANTS), []);
});

test("granted compares the WHOLE handle — read-all is not read", () => {
  const scopes = parseScopes("claims:read-all claims:approve");
  assert.equal(granted(scopes, "claims:read-all"), true);
  assert.equal(granted(scopes, "claims:read"), false);
  assert.equal(granted(scopes, "claims"), false);
  assert.equal(granted(scopes, "claims:rea"), false);
});

// --- canReach ---------------------------------------------------------------

test("canReach: null is any signed-in caller", () => {
  assert.equal(canReach(null, held()), true);
  assert.equal(canReach(null, held("claims:read")), true);
});

test('canReach: "public" is reachable with nothing at all', () => {
  assert.equal(canReach("public", held()), true);
});

test("canReach: a handle is matched exactly", () => {
  assert.equal(canReach("claims:read", held("claims:read")), true);
  assert.equal(canReach("claims:read", held("claims:read-all")), false);
  assert.equal(canReach("claims:read", held()), false);
});

// --- heldRoles / rolesGranting ---------------------------------------------

test("heldRoles needs EVERY grant of a role", () => {
  assert.deepEqual(heldRoles(held(...ROLE_GRANTS.Employee), ROLE_GRANTS), ["Employee"]);
  assert.deepEqual(heldRoles(held(...ROLE_GRANTS.Approver), ROLE_GRANTS), ["Approver"]);
  assert.deepEqual(
    heldRoles(held(...ROLE_GRANTS.Employee, ...ROLE_GRANTS.Approver), ROLE_GRANTS),
    ["Approver", "Employee"],
  );
});

test("heldRoles yields no role for a PARTIAL grant set", () => {
  // Three of the Approver's five handles, and one of the Employee's two.
  assert.deepEqual(
    heldRoles(held("claims:read-all", "claims:approve", "claims:reject"), ROLE_GRANTS),
    [],
  );
  assert.deepEqual(heldRoles(held("claims:read"), ROLE_GRANTS), []);
  assert.deepEqual(heldRoles(held(), ROLE_GRANTS), []);
});

test("heldRoles ignores a handle no role grants", () => {
  assert.deepEqual(heldRoles(held("reports:export"), ROLE_GRANTS), []);
  assert.deepEqual(
    heldRoles(held(...ROLE_GRANTS.Employee, "reports:export"), ROLE_GRANTS),
    ["Employee"],
  );
});

test("rolesGranting names what unlocks a handle — the Forbidden copy", () => {
  assert.deepEqual(rolesGranting("claims:approve", ROLE_GRANTS), ["Approver"]);
  assert.deepEqual(rolesGranting("claims:submit", ROLE_GRANTS), ["Employee"]);
  assert.deepEqual(rolesGranting("claims:read", ROLE_GRANTS), ["Approver", "Employee"]);
  assert.deepEqual(rolesGranting("reports:export", ROLE_GRANTS), []);
});

// --- tokenIsValid -----------------------------------------------------------

const NOW = 1_800_000_000_000; // ms
const nowSeconds = NOW / 1000;

test("tokenIsValid: no token at all is not valid", () => {
  assert.equal(tokenIsValid(undefined, NOW, 300), false);
});

test("tokenIsValid: a future expiry is valid", () => {
  assert.equal(tokenIsValid(nowSeconds + 60, NOW, 300), true);
});

test("tokenIsValid: the skew is a GRACE window, not a shortening", () => {
  // 100 s past expiry, 300 s of skew: still treated as alive, because calling a
  // live session dead is what starts the sign-in loop.
  assert.equal(tokenIsValid(nowSeconds - 100, NOW, 300), true);
  assert.equal(tokenIsValid(nowSeconds - 100, NOW, 0), false);
  assert.equal(tokenIsValid(nowSeconds - 400, NOW, 300), false);
});

// --- classifyApiFailure + the handler: DECISION A1 --------------------------

test("classifyApiFailure covers the three cases and nothing else", () => {
  assert.equal(classifyApiFailure(401, true), "forbidden");
  assert.equal(classifyApiFailure(401, false), "signin");
  assert.equal(classifyApiFailure(403, true), "forbidden");
  assert.equal(classifyApiFailure(403, false), "forbidden");
  assert.equal(classifyApiFailure(200, true), "ok");
  assert.equal(classifyApiFailure(404, true), "ok");
  assert.equal(classifyApiFailure(500, false), "ok");
});

function spies() {
  const calls = { signIn: 0, forbidden: 0 };
  const handle = createUnauthorizedHandler({
    signIn: () => {
      calls.signIn += 1;
    },
    onForbidden: () => {
      calls.forbidden += 1;
    },
  });
  return { calls, handle };
}

test("A1 · 401 with a LIVE token is Forbidden, and never signs in again", () => {
  // The regression this exists to catch: the gateway answers 401 for a missing
  // scope, and `if (401) signIn()` turned that into an endless sign-in loop for
  // a correctly provisioned user.
  const { calls, handle } = spies();
  assert.equal(handle(401, true), "forbidden");
  assert.equal(calls.signIn, 0);
  assert.equal(calls.forbidden, 1);
});

test("A1 · 401 with an expired or absent token signs in EXACTLY once", () => {
  const { calls, handle } = spies();
  assert.equal(handle(401, false), "signin");
  assert.equal(calls.signIn, 1);
  assert.equal(calls.forbidden, 0);
  // A screen fires several requests at once; each 401 must not start its own
  // redirect.
  handle(401, false);
  handle(401, false);
  assert.equal(calls.signIn, 1);
  assert.equal(calls.forbidden, 0);
});

test("A1 · 403 with a live token is Forbidden and never signs in", () => {
  const { calls, handle } = spies();
  assert.equal(handle(403, true), "forbidden");
  assert.equal(calls.signIn, 0);
  assert.equal(calls.forbidden, 1);
});

test("A1 · a 403 never signs in even when the token is gone", () => {
  const { calls, handle } = spies();
  assert.equal(handle(403, false), "forbidden");
  assert.equal(calls.signIn, 0);
  assert.equal(calls.forbidden, 1);
});

test("anything that is not 401 or 403 touches neither branch", () => {
  const { calls, handle } = spies();
  assert.equal(handle(500, true), "ok");
  assert.equal(calls.signIn, 0);
  assert.equal(calls.forbidden, 0);
});
