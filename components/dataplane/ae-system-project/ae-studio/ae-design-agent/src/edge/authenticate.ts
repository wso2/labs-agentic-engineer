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
 * Who is calling `/v1` (07 §8). `authenticate(req)` answers the verified
 * user, or throws `AuthError` with the status the gate answers. Two adapters:
 * the Platform IdP's here (`idpAuthenticate`: a user token of the pod's org,
 * never an M2M token) and the playground's dev verifier (Task 3.14).
 */

import type { IncomingMessage } from "node:http";
import type { JWTVerifyGetKey } from "jose";
import {
  createVerifier,
  displayIdentity,
  IdpUnavailableError,
  UnauthenticatedError,
  userRule,
  type UserClaims,
} from "@aep/platform-idp-auth";

/** A verified user: the credit a turn carries (07 §1 "Credit"). */
export interface AuthenticatedUser {
  sub: string;
  /** `displayIdentity`'s rule: never empty. */
  name: string;
  /** `""` when the token carries none. */
  email: string;
  claims: UserClaims;
}

export type Authenticate = (req: IncomingMessage) => Promise<AuthenticatedUser>;

/** The caller is not admitted. The fields are fixed sentences: no token or claim value. */
export class AuthError extends Error {
  constructor(
    readonly status: 401 | 403 | 503,
    readonly code: "unauthenticated" | "org_mismatch" | "idp_unavailable",
    readonly detail: string,
    readonly headers: Record<string, string> = {},
  ) {
    super(detail);
    this.name = "AuthError";
  }
}

export interface IdpAuthConfig {
  issuer: string;
  jwksUrl: string;
  userAudiences: string[];
  orgId: string;
  orgHandle: string;
  /** Replaces the IdP's remote JWKS (tests). */
  jwks?: JWTVerifyGetKey;
}

const BEARER = /^Bearer ([^\s]+)$/i;

/** The pod's adapter: a Platform IdP user token whose org claims are the pod's. */
export function idpAuthenticate(cfg: IdpAuthConfig): Authenticate {
  const verify = createVerifier({ issuer: cfg.issuer, jwksUrl: cfg.jwksUrl, ...(cfg.jwks ? { jwks: cfg.jwks } : {}) });
  const kinds = [{ name: "user" as const, audiences: cfg.userAudiences }];
  const pod = { orgId: cfg.orgId, orgHandle: cfg.orgHandle };
  return async (req) => {
    const token = BEARER.exec(req.headers.authorization ?? "")?.[1];
    if (!token) throw new AuthError(401, "unauthenticated", "a bearer token is required", { "www-authenticate": "Bearer" });
    let verified;
    try {
      verified = await verify(token, kinds);
    } catch (err) {
      if (err instanceof IdpUnavailableError) {
        // No verdict on the token: the IdP's keys could not be fetched.
        throw new AuthError(503, "idp_unavailable", "the identity provider cannot be reached", { "retry-after": "5" });
      }
      if (err instanceof UnauthenticatedError) {
        throw new AuthError(401, "unauthenticated", "the bearer token is not valid here", {
          "www-authenticate": 'Bearer error="invalid_token"',
        });
      }
      throw err;
    }
    // Only the user kind is offered, so anything else is a wiring fault.
    if (verified.kind !== "user") throw new Error("idpAuthenticate: unexpected token kind");
    if (!userRule(verified.claims, pod)) {
      throw new AuthError(403, "org_mismatch", "the token is not for this organization");
    }
    const { name, email } = displayIdentity(verified.claims);
    return { sub: verified.claims.sub, name, email, claims: verified.claims };
  };
}
