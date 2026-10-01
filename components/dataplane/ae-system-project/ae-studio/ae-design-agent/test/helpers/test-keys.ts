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
 * A local stand-in for the Platform IdP: one RS256 key in a local JWKS and
 * signers for the token shapes the pod sees (a console user, the per-org
 * publisher client, the AE-only internal client).
 */

import { SignJWT, createLocalJWKSet, exportJWK, generateKeyPair, type JWTVerifyGetKey } from "jose";

const ISSUER = "http://thunder.test";
const USER_AUDIENCE = "aep-console-client";

export interface TestKeys {
  issuer: string;
  jwks: JWTVerifyGetKey;
  /** A console user's access token in the OU `ouId` / `ouHandle`. */
  user(ouHandle: string, ouId: string): Promise<string>;
  /** A client_credentials token: the org's publisher client (default) or the AE-only client. */
  m2m(client?: "publisher" | "ae-internal"): Promise<string>;
  /** A client_credentials token that names the user audience, with org claims. */
  m2mOnUserAudience(ouHandle: string, ouId: string): Promise<string>;
}

export async function testKeys(): Promise<TestKeys> {
  const { publicKey, privateKey } = await generateKeyPair("RS256");
  const jwks = createLocalJWKSet({ keys: [{ ...(await exportJWK(publicKey)), kid: "k1", alg: "RS256" }] });
  const sign = (claims: Record<string, unknown>) =>
    new SignJWT(claims)
      .setProtectedHeader({ alg: "RS256", kid: "k1" })
      .setIssuer(ISSUER)
      .setExpirationTime("1h")
      .sign(privateKey);
  const cc = { grant_type: "client_credentials" };
  return {
    issuer: ISSUER,
    jwks,
    user: (ouHandle, ouId) => sign({ aud: USER_AUDIENCE, sub: `user-${ouHandle}`, ouId, ouHandle }),
    m2m: (client = "publisher") =>
      client === "publisher"
        ? sign({ ...cc, aud: "aep-publisher-default", client_id: "aep-publisher-default", ouId: "ou-1", ouHandle: "default" })
        : sign({ ...cc, aud: "ae-studio-internal-client", client_id: "ae-studio-internal-client" }),
    m2mOnUserAudience: (ouHandle, ouId) => sign({ ...cc, aud: USER_AUDIENCE, client_id: USER_AUDIENCE, ouId, ouHandle }),
  };
}
