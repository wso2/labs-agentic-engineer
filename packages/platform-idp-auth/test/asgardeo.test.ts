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

/**
 * The verifier under the Asgardeo dialect. The tokens carry a real Asgardeo
 * access token's claim set (names recorded from a test organization, values
 * made up): the org in org_handle / org_id / org_name, aut for who the token
 * was issued to, aud as one string, and on a machine token sub, aud, azp and
 * client_id all holding the client id.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { SignJWT, generateKeyPair, exportJWK, createLocalJWKSet } from "jose";
import { createVerifier, orgRule, UnauthenticatedError, userRule } from "../src/index.js";

const ISS = "https://api.asgardeo.io/t/acme/oauth2/token";
const CONSOLE = "aep-console-client";
const STUDIO = "ae-studio-acme";
const USER = { name: "user" as const, audiences: [CONSOLE] };
const AGENT = { name: "ae-studio" as const, audiences: [STUDIO] };
const POD = { orgId: "org-1", orgHandle: "acme" };
const ORG = { org_id: "org-1", org_name: "Acme Inc", org_handle: "acme" };

const userClaims = {
  sub: "u1",
  aut: "APPLICATION_USER",
  aud: CONSOLE,
  azp: CONSOLE,
  client_id: CONSOLE,
  ...ORG,
  scope: "email openid profile",
  binding_type: "sso-session",
  binding_ref: "b1",
  email: "ada@acme.example",
  given_name: "Ada",
  family_name: "Lovelace",
  username: "ada@acme.example",
  groups: ["ae-users"],
};

const machineClaims = (clientId: string) => ({ sub: clientId, aud: clientId, azp: clientId, client_id: clientId, aut: "APPLICATION", ...ORG });

async function setup() {
  const rs = await generateKeyPair("RS256");
  const jwks = createLocalJWKSet({ keys: [{ ...(await exportJWK(rs.publicKey)), kid: "rs", alg: "RS256" }] });
  const sign = (claims: Record<string, unknown>) =>
    new SignJWT(claims)
      .setProtectedHeader({ alg: "RS256", kid: "rs", typ: "at+jwt" })
      .setIssuer(ISS)
      .setExpirationTime("1h")
      .sign(rs.privateKey);
  return { verify: createVerifier({ issuer: ISS, jwksUrl: "http://unused", kind: "asgardeo", jwks }), sign };
}

/** `claims` without the named claim. */
function without(claims: Record<string, unknown>, name: string): Record<string, unknown> {
  const rest = { ...claims };
  delete rest[name];
  return rest;
}

test("asgardeo: a user's token is kind user, its org and display claims read, and it passes the user rule", async () => {
  const { verify, sign } = await setup();
  const v = await verify(await sign(userClaims), [AGENT, USER]);
  assert.equal(v.kind, "user");
  assert.equal(v.claims.sub, "u1");
  assert.deepEqual(v.claims.org, { handle: "acme", id: "org-1", name: "Acme Inc", handleDeclared: true });
  // Asgardeo sends no name claim; the given and family names carry the display name.
  assert.equal(v.claims.name, undefined);
  assert.equal(v.claims.given_name, "Ada");
  assert.equal(v.claims.family_name, "Lovelace");
  assert.equal(v.claims.email, "ada@acme.example");
  assert.equal(v.kind === "user" && userRule(v.claims, POD), true);
});

test("asgardeo: a user of another org verifies but fails the user rule", async () => {
  const { verify, sign } = await setup();
  for (const other of [{ org_id: "org-2", org_handle: "globex" }, { org_id: "org-2" }, { org_handle: "globex" }]) {
    const v = await verify(await sign({ ...userClaims, ...other }), [USER]);
    assert.equal(v.kind === "user" && userRule(v.claims, POD), false);
  }
  const noHandle = await verify(await sign(without(userClaims, "org_handle")), [USER]);
  assert.equal(noHandle.kind === "user" && userRule(noHandle.claims, POD), false);
});

test("asgardeo: the org's ae-studio client is kind ae-studio and passes the org rule", async () => {
  const { verify, sign } = await setup();
  const v = await verify(await sign(machineClaims(STUDIO)), [AGENT, USER]);
  assert.equal(v.kind, "ae-studio");
  assert.equal(v.claims.client_id, STUDIO);
  assert.equal(orgRule(v.claims, POD), true);
});

test("asgardeo: tokens the dialect places elsewhere, or cannot place, are refused", async () => {
  const { verify, sign } = await setup();
  const refused: Record<string, Record<string, unknown>> = {
    "a machine token on the user audience": machineClaims(CONSOLE),
    "a user token on the ae-studio audience": { ...userClaims, aud: STUDIO, client_id: STUDIO },
    "no aut": without(userClaims, "aut"),
    "an aut that is neither value": { ...userClaims, aut: "DEVICE" },
    "a Thunder user's claims": { ...without(userClaims, "aut"), grant_type: "authorization_code" },
    "a user token without a sub": without(userClaims, "sub"),
    "another client on the ae-studio audience": { ...machineClaims(STUDIO), client_id: "someone-else" },
    "the install-wide client": machineClaims("ae-studio-internal-client"),
  };
  for (const [name, claims] of Object.entries(refused)) {
    await assert.rejects(verify(await sign(claims), [AGENT, USER]), UnauthenticatedError, name);
  }
});
