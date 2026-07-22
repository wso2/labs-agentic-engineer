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

/**
 * The M2M gate over its shared-secret (HS256) path — the path the evals and
 * playground use. The JWKS (RS256) path is the same jose call with a remote key
 * set and is exercised only against a live IDP; it has no unit coverage here.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import express from "express";
import { SignJWT } from "jose";
import { createAuthMiddleware, type AgentsAuthConfig } from "../src/shared/auth.js";
import { listen0 } from "../src/shared/listen.js";

const AUD = "agents-service";
const SECRET = "unit-secret";

async function bootGuarded(cfg: AgentsAuthConfig = { audience: AUD, secret: SECRET }) {
  const app = express();
  app.use(createAuthMiddleware(cfg));
  app.get("/secure", (_req, res) => {
    res.status(200).json({ ok: true });
  });
  return listen0(app.listen(0));
}

async function hs256(opts: { audience?: string; secret?: string; issuer?: string; expired?: boolean } = {}): Promise<string> {
  const jwt = new SignJWT({})
    .setProtectedHeader({ alg: "HS256" })
    .setAudience(opts.audience ?? AUD)
    .setIssuedAt()
    .setExpirationTime(opts.expired ? Math.floor(Date.now() / 1000) - 60 : "1h");
  if (opts.issuer) jwt.setIssuer(opts.issuer);
  return jwt.sign(new TextEncoder().encode(opts.secret ?? SECRET));
}

const bearer = (t: string) => ({ headers: { Authorization: `Bearer ${t}` } });

test("createAuthMiddleware throws when neither JWKS nor secret is configured", () => {
  assert.throws(() => createAuthMiddleware({ audience: AUD }), /always on/);
});

test("accepts a valid shared-secret token", async () => {
  const { baseUrl, close } = await bootGuarded();
  try {
    const res = await fetch(`${baseUrl}/secure`, bearer(await hs256()));
    assert.equal(res.status, 200);
    assert.deepEqual(await res.json(), { ok: true });
  } finally {
    await close();
  }
});

test("rejects a missing Authorization header with a Bearer challenge", async () => {
  const { baseUrl, close } = await bootGuarded();
  try {
    const res = await fetch(`${baseUrl}/secure`);
    assert.equal(res.status, 401);
    assert.match(res.headers.get("www-authenticate") ?? "", /^Bearer realm="agents-service"/);
  } finally {
    await close();
  }
});

test("rejects a malformed Authorization header", async () => {
  const { baseUrl, close } = await bootGuarded();
  try {
    const res = await fetch(`${baseUrl}/secure`, { headers: { Authorization: "NotBearer xyz" } });
    assert.equal(res.status, 401);
    assert.match(res.headers.get("www-authenticate") ?? "", /error="invalid_token"/);
  } finally {
    await close();
  }
});

test("rejects wrong audience, wrong secret, and expired tokens", async () => {
  const { baseUrl, close } = await bootGuarded();
  try {
    assert.equal((await fetch(`${baseUrl}/secure`, bearer(await hs256({ audience: "other" })))).status, 401);
    assert.equal((await fetch(`${baseUrl}/secure`, bearer(await hs256({ secret: "wrong" })))).status, 401);
    assert.equal((await fetch(`${baseUrl}/secure`, bearer(await hs256({ expired: true })))).status, 401);
  } finally {
    await close();
  }
});

test("accepts a token whose nbf is a few seconds ahead of this pod's clock (skew tolerance)", async () => {
  const { baseUrl, close } = await bootGuarded();
  try {
    // nbf ~2s in the future — inside the 5s clockTolerance, so it must still verify.
    const token = await new SignJWT({})
      .setProtectedHeader({ alg: "HS256" })
      .setAudience(AUD)
      .setIssuedAt()
      .setNotBefore(Math.floor(Date.now() / 1000) + 2)
      .setExpirationTime("1h")
      .sign(new TextEncoder().encode(SECRET));
    assert.equal((await fetch(`${baseUrl}/secure`, bearer(token))).status, 200);
  } finally {
    await close();
  }
});

test("enforces issuer when configured", async () => {
  const { baseUrl, close } = await bootGuarded({ audience: AUD, secret: SECRET, issuer: "aep-bff" });
  try {
    assert.equal((await fetch(`${baseUrl}/secure`, bearer(await hs256({ issuer: "aep-bff" })))).status, 200);
    assert.equal((await fetch(`${baseUrl}/secure`, bearer(await hs256({ issuer: "someone-else" })))).status, 401);
    assert.equal((await fetch(`${baseUrl}/secure`, bearer(await hs256()))).status, 401); // no iss claim
  } finally {
    await close();
  }
});
