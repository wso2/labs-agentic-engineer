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
import { createDialect } from "../src/dialect.js";
import { readIdpClaims } from "../src/index.js";
import { ASGARDEO, claimsOf, loadFixture, THUNDER } from "./helpers/idp-claims.js";

// Every sample token of both IdPs reads as the shared fixtures say. The same
// files pin aep-api's dialect, the pod's Go twin and the console, so the four
// cannot drift apart.
for (const idp of ["thunder", "asgardeo"] as const) {
  const fixture = loadFixture(idp);
  const dialect = createDialect(claimsOf(fixture));
  for (const token of fixture.tokens) {
    test(`${idp}: ${token.name}`, () => {
      assert.deepEqual(dialect.org(token.payload), token.org);
      const machine = dialect.machine(token.payload);
      assert.equal(machine === undefined ? "refused" : machine ? "machine" : "user", token.caller);
    });
  }
}

test("readIdpClaims: the pod's AE_IDP_CLAIMS_* settings, trimmed", () => {
  const { claims, problems } = readIdpClaims({
    AE_IDP_CLAIMS_ORG_HANDLE: " org_handle ",
    AE_IDP_CLAIMS_ORG_ID: "org_id",
    AE_IDP_CLAIMS_ORG_NAME: "org_name",
    AE_IDP_CLAIMS_CALLER_TYPE_CLAIM: "aut",
    AE_IDP_CLAIMS_CALLER_TYPE_MACHINE: "APPLICATION",
    AE_IDP_CLAIMS_CALLER_TYPE_USER: "APPLICATION_USER",
  });
  assert.deepEqual(problems, []);
  assert.deepEqual(claims, ASGARDEO);
});

test("readIdpClaims: the pod knows no IdP, so each mandatory key that is missing is named", () => {
  const { claims, problems } = readIdpClaims({ AE_IDP_CLAIMS_ORG_ID: "ouId", AE_IDP_CLAIMS_CALLER_TYPE_CLAIM: "  " });
  assert.deepEqual(problems, [
    "missing AE_IDP_CLAIMS_ORG_HANDLE",
    "missing AE_IDP_CLAIMS_CALLER_TYPE_CLAIM",
    "missing AE_IDP_CLAIMS_CALLER_TYPE_MACHINE",
  ]);
  // The org's display name claim and the user value are optional.
  assert.equal(claims.orgName, "");
  assert.equal(claims.callerTypeUser, "");
  assert.deepEqual(readIdpClaims({ ...envOf(THUNDER) }).problems, []);
});

test("createDialect: an incomplete set throws, naming what is missing", () => {
  assert.throws(() => createDialect({ ...ASGARDEO, orgHandle: "" }), /orgHandle/);
  assert.throws(() => createDialect({ ...ASGARDEO, orgId: " " }), /orgId/);
  assert.throws(() => createDialect({ ...ASGARDEO, callerTypeClaim: "" }), /callerTypeClaim/);
  assert.throws(() => createDialect({ ...ASGARDEO, callerTypeMachine: "" }), /callerTypeMachine/);
  assert.doesNotThrow(() => createDialect({ ...ASGARDEO, orgName: "", callerTypeUser: "" }));
});

test("with no user value configured, any value but the machine's is a user's", () => {
  const strict = createDialect(ASGARDEO);
  const loose = createDialect({ ...ASGARDEO, callerTypeUser: "" });
  assert.equal(strict.machine({ aut: "DEVICE" }), undefined);
  assert.equal(loose.machine({ aut: "DEVICE" }), false);
  assert.equal(loose.machine({ aut: "APPLICATION" }), true);
  // A caller type claim that is not text is no claim.
  assert.equal(loose.machine({ aut: true }), undefined);
});

/** The env a pod would get for `claims`. */
function envOf(claims: typeof THUNDER): Record<string, string> {
  return {
    AE_IDP_CLAIMS_ORG_HANDLE: claims.orgHandle,
    AE_IDP_CLAIMS_ORG_ID: claims.orgId,
    AE_IDP_CLAIMS_ORG_NAME: claims.orgName,
    AE_IDP_CLAIMS_CALLER_TYPE_CLAIM: claims.callerTypeClaim,
    AE_IDP_CLAIMS_CALLER_TYPE_MACHINE: claims.callerTypeMachine,
    AE_IDP_CLAIMS_CALLER_TYPE_USER: claims.callerTypeUser,
  };
}
