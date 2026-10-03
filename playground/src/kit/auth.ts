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
 * The playground's dev verifier: the second `Authenticate` adapter of the
 * design agent's `/v1` edge (07 §9; the pod's is `idpAuthenticate`). There is
 * no IdP in a local run, so a session mints one random bearer secret, hands
 * the client its headers, and the verifier admits that secret alone as the
 * local user. Nothing is signed and nothing outlives the session; the app
 * listens on loopback only (`agents-app.ts`).
 */

import { randomBytes, timingSafeEqual } from "node:crypto";
import { AuthError, type Authenticate, type AuthenticatedUser } from "@aep/ae-design-agent/edge/authenticate";

/** Who every playground turn is credited to. */
export const PLAYGROUND_USER = { sub: "playground", name: "Playground", email: "" } as const;

export interface DevAuth {
  /** The `/v1` gate: the session's bearer secret, or 401. */
  authenticate: Authenticate;
  /** What every `/v1` request carries. */
  headers: Record<string, string>;
}

const BEARER = /^Bearer ([^\s]+)$/i;

function unauthenticated(): AuthError {
  return new AuthError(401, "unauthenticated", "the playground session's bearer token is required", {
    "www-authenticate": "Bearer",
  });
}

/** A fresh dev credential and its verifier, for one session. */
export function devAuth(): DevAuth {
  const secret = randomBytes(32).toString("base64url");
  const expected = Buffer.from(secret);
  const user: AuthenticatedUser = {
    ...PLAYGROUND_USER,
    claims: { sub: PLAYGROUND_USER.sub, name: PLAYGROUND_USER.name, exp: Number.MAX_SAFE_INTEGER },
  };
  return {
    authenticate: async (req) => {
      const presented = Buffer.from(BEARER.exec(req.headers.authorization ?? "")?.[1] ?? "");
      if (presented.length !== expected.length || !timingSafeEqual(presented, expected)) throw unauthenticated();
      return user;
    },
    headers: { Authorization: `Bearer ${secret}` },
  };
}
