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
import { SignJWT, generateKeyPair, exportJWK, createLocalJWKSet } from "jose";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { createVerifier, IdpUnavailableError, UnauthenticatedError } from "../src/index.js";

const ISS = "http://thunder.openchoreo.localhost:8080";
const USER = { name: "user" as const, audiences: ["aep-console-client"] };
const AGENT = { name: "ae-studio" as const, audiences: ["ae-studio-default"] };

async function setup() {
  const rs = await generateKeyPair("RS256");
  const es = await generateKeyPair("ES256");
  const jwks = createLocalJWKSet({
    keys: [
      { ...(await exportJWK(rs.publicKey)), kid: "rs", alg: "RS256" },
      { ...(await exportJWK(es.publicKey)), kid: "es", alg: "ES256" },
    ],
  });
  const builder = (claims: Record<string, unknown>, alg = "RS256") =>
    new SignJWT(claims).setProtectedHeader({ alg, kid: alg === "RS256" ? "rs" : "es" });
  const key = (alg: string) => (alg === "RS256" ? rs.privateKey : es.privateKey);
  const sign = (claims: Record<string, unknown>, alg = "RS256") =>
    builder(claims, alg).setIssuer(ISS).setExpirationTime("1h").sign(key(alg));
  return {
    verify: createVerifier({ issuer: ISS, jwksUrl: "http://unused", jwks }),
    sign,
    builder,
    rsKey: rs.privateKey,
  };
}

test("user token resolves to kind user (RS256 and ES256)", async () => {
  const { verify, sign } = await setup();
  for (const alg of ["RS256", "ES256"]) {
    const t = await sign({ aud: "aep-console-client", sub: "u1", ouId: "ou-1", ouHandle: "default" }, alg);
    const v = await verify(t, [USER]);
    assert.equal(v.kind, "user");
    assert.equal(v.claims.sub, "u1");
    assert.equal(v.claims.ouId, "ou-1");
  }
});

test("an aud array matches when any entry is on the kind's list", async () => {
  const { verify, sign } = await setup();
  const t = await sign({ aud: ["other", "aep-console-client"], sub: "u1" });
  assert.equal((await verify(t, [USER])).kind, "user");
});

test("a token matching no kind is refused", async () => {
  const { verify, sign } = await setup();
  const m2m = await sign({ aud: "ae-studio-internal-client", client_id: "ae-studio-internal-client", grant_type: "client_credentials" });
  await assert.rejects(verify(m2m, [USER]), UnauthenticatedError);
  const pub = await sign({ aud: "aep-publisher-default", ouId: "ou-1", ouHandle: "default", grant_type: "client_credentials" });
  await assert.rejects(verify(pub, [USER]), UnauthenticatedError);
  const noAud = await sign({ sub: "u1", ouId: "ou-1", ouHandle: "default" });
  await assert.rejects(verify(noAud, [USER]), UnauthenticatedError);
});

test("client_credentials never passes as a user", async () => {
  const { verify, sign } = await setup();
  const t = await sign({ aud: "aep-console-client", grant_type: "client_credentials", ouId: "ou-1", ouHandle: "default" });
  await assert.rejects(verify(t, [USER]), UnauthenticatedError);
  // Offering both kinds does not let an M2M token on the user audience in.
  await assert.rejects(verify(t, [AGENT, USER]), UnauthenticatedError);
});

test("user token with no sub is rejected", async () => {
  const { verify, sign } = await setup();
  const none = await sign({ aud: "aep-console-client", ouId: "ou-1", ouHandle: "default" });
  await assert.rejects(verify(none, [USER]), UnauthenticatedError);
  const empty = await sign({ aud: "aep-console-client", sub: "", ouId: "ou-1", ouHandle: "default" });
  await assert.rejects(verify(empty, [USER]), UnauthenticatedError);
});

test("the ae-studio kind is only its own client_credentials client", async () => {
  const { verify, sign } = await setup();
  const notCC = await sign({ aud: "ae-studio-default", client_id: "ae-studio-default", sub: "u1", ouId: "ou-1", ouHandle: "default" });
  await assert.rejects(verify(notCC, [AGENT]), UnauthenticatedError);
  const otherClient = await sign({ aud: "ae-studio-default", client_id: "someone-else", grant_type: "client_credentials" });
  await assert.rejects(verify(otherClient, [AGENT]), UnauthenticatedError);
  const noClient = await sign({ aud: "ae-studio-default", grant_type: "client_credentials" });
  await assert.rejects(verify(noClient, [AGENT]), UnauthenticatedError);
});

test("a token without kid is refused", async () => {
  const rs = await generateKeyPair("RS256");
  const jwks = createLocalJWKSet({ keys: [{ ...(await exportJWK(rs.publicKey)), alg: "RS256" }] });
  const verify = createVerifier({ issuer: ISS, jwksUrl: "http://unused", jwks });
  const t = await new SignJWT({ aud: "aep-console-client", sub: "u1" })
    .setProtectedHeader({ alg: "RS256" })
    .setIssuer(ISS)
    .setExpirationTime("1h")
    .sign(rs.privateKey);
  await assert.rejects(verify(t, [USER]), UnauthenticatedError);
});

test("the ae-studio kind needs its own audience", async () => {
  const { verify, sign } = await setup();
  const t = await sign({ aud: "ae-studio-default", client_id: "ae-studio-default", grant_type: "client_credentials", ouId: "ou-1", ouHandle: "default" });
  assert.equal((await verify(t, [AGENT])).kind, "ae-studio");
  await assert.rejects(verify(t, [USER]), UnauthenticatedError);
});

test("wrong issuer and expired are refused", async () => {
  const { verify, builder, rsKey } = await setup();
  await assert.rejects(verify("not-a-jwt", [USER]), UnauthenticatedError);
  const claims = { aud: "aep-console-client", sub: "u1", ouId: "ou-1", ouHandle: "default" };
  const foreign = await builder(claims).setIssuer("http://evil.example").setExpirationTime("1h").sign(rsKey);
  await assert.rejects(verify(foreign, [USER]), UnauthenticatedError);
  const prefixed = await builder(claims).setIssuer(`${ISS}/`).setExpirationTime("1h").sign(rsKey);
  await assert.rejects(verify(prefixed, [USER]), UnauthenticatedError);
  const expired = await builder(claims).setIssuer(ISS).setExpirationTime(Math.floor(Date.now() / 1000) - 60).sign(rsKey);
  await assert.rejects(verify(expired, [USER]), UnauthenticatedError);
});

test("a token without exp is refused", async () => {
  const { verify, builder, rsKey } = await setup();
  const t = await builder({ aud: "aep-console-client", sub: "u1" }).setIssuer(ISS).sign(rsKey);
  await assert.rejects(verify(t, [USER]), UnauthenticatedError);
});

test("an unknown kid or an algorithm off the list is refused", async () => {
  const { verify, sign } = await setup();
  const other = await generateKeyPair("RS256");
  const unknownKid = await new SignJWT({ aud: "aep-console-client", sub: "u1" })
    .setProtectedHeader({ alg: "RS256", kid: "nope" })
    .setIssuer(ISS)
    .setExpirationTime("1h")
    .sign(other.privateKey);
  await assert.rejects(verify(unknownKid, [USER]), UnauthenticatedError);
  const hs = await new SignJWT({ aud: "aep-console-client", sub: "u1" })
    .setProtectedHeader({ alg: "HS256", kid: "rs" })
    .setIssuer(ISS)
    .setExpirationTime("1h")
    .sign(new TextEncoder().encode("a-shared-secret-of-sufficient-length!"));
  await assert.rejects(verify(hs, [USER]), UnauthenticatedError);
  // A valid token re-signed by a key the IdP does not publish fails too.
  const good = await sign({ aud: "aep-console-client", sub: "u1" });
  const [h, p] = good.split(".");
  const forged = await new SignJWT({ aud: "aep-console-client", sub: "u1" })
    .setProtectedHeader({ alg: "RS256", kid: "rs" })
    .setIssuer(ISS)
    .setExpirationTime("1h")
    .sign(other.privateKey);
  await assert.rejects(verify(`${h}.${p}.${forged.split(".")[2]}`, [USER]), UnauthenticatedError);
});

test("construction fails closed on an empty issuer or JWKS URL", () => {
  assert.throws(() => createVerifier({ issuer: "", jwksUrl: "http://idp/jwks" }), /issuer/);
  assert.throws(() => createVerifier({ issuer: ISS, jwksUrl: "" }), /jwksUrl/);
});

test("no kinds, a kind without audiences or an empty audience is a wiring error, not a 401", async () => {
  const { verify, sign } = await setup();
  const t = await sign({ aud: "aep-console-client", sub: "u1" });
  for (const kinds of [[], [{ name: "user" as const, audiences: [] }], [{ name: "user" as const, audiences: [""] }]]) {
    await assert.rejects(verify(t, kinds), (err: unknown) => err instanceof Error && !(err instanceof UnauthenticatedError));
  }
});

/** A JWKS endpoint answering with `answer`; the URL and a close. */
async function jwksServer(answer: (res: import("node:http").ServerResponse) => void): Promise<{ url: string; close(): Promise<void> }> {
  const server: Server = createServer((_req, res) => answer(res));
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return {
    url: `http://127.0.0.1:${(server.address() as AddressInfo).port}/oauth2/jwks`,
    close: () =>
      new Promise((resolve) => {
        server.closeAllConnections();
        server.close(() => resolve());
      }),
  };
}

test("an IdP whose keys cannot be fetched is unavailable, not a verdict on the token", async () => {
  const { sign } = await setup();
  const t = await sign({ aud: "aep-console-client", sub: "u1" });
  // Nothing listens: the fetch is refused.
  const down = await jwksServer(() => {});
  await down.close();
  await assert.rejects(createVerifier({ issuer: ISS, jwksUrl: down.url })(t, [USER]), IdpUnavailableError);
  // A 5xx and a body that is not a key set.
  const broken = await jwksServer((res) => res.writeHead(503).end("busy"));
  await assert.rejects(createVerifier({ issuer: ISS, jwksUrl: broken.url })(t, [USER]), IdpUnavailableError);
  await broken.close();
  const junk = await jwksServer((res) => res.writeHead(200, { "content-type": "application/json" }).end("{}"));
  await assert.rejects(createVerifier({ issuer: ISS, jwksUrl: junk.url })(t, [USER]), IdpUnavailableError);
  await junk.close();
});

test("a reachable IdP still refuses a token its keys do not verify", async () => {
  const { sign } = await setup();
  const other = await generateKeyPair("RS256");
  const keys = await jwksServer((res) =>
    void exportJWK(other.publicKey).then((jwk) =>
      res.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify({ keys: [{ ...jwk, kid: "rs", alg: "RS256" }] })),
    ),
  );
  try {
    const verify = createVerifier({ issuer: ISS, jwksUrl: keys.url });
    // Signed by a key the IdP does not publish under that kid: bad signature.
    await assert.rejects(verify(await sign({ aud: "aep-console-client", sub: "u1" }), [USER]), UnauthenticatedError);
    // A kid the IdP does not publish at all.
    const unknownKid = await new SignJWT({ aud: "aep-console-client", sub: "u1" })
      .setProtectedHeader({ alg: "RS256", kid: "nope" })
      .setIssuer(ISS)
      .setExpirationTime("1h")
      .sign(other.privateKey);
    await assert.rejects(verify(unknownKid, [USER]), UnauthenticatedError);
  } finally {
    await keys.close();
  }
});
